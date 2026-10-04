package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

// ===== events =====

func (a *API) registerEvents(r chi.Router) {
	r.Route("/api/v1/events", func(r chi.Router) {
		r.Get("/", a.eventList)
		r.Get("/{id}", a.eventGet)
		r.Post("/", a.eventCreate)
		r.Put("/{id}", a.eventUpdate)
		r.Delete("/{id}", a.eventDelete)
	})
}

func (a *API) eventList(w http.ResponseWriter, r *http.Request) {
	// person_id 按参与人过滤，q 才是标题/摘要/地点关键字，两者互不混淆。
	list, err := a.Store.EventList(r.Context(),
		r.URL.Query().Get("person_id"),
		r.URL.Query().Get("q"),
		parseIntQuery(r, "limit", 50), parseIntQuery(r, "offset", 0))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) eventGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	e, err := a.Store.EventGet(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeErr(w, 404, "not found")
			return
		}
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, e)
}

func (a *API) eventCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		store.Event
		ParticipantIDs []string `json:"participant_ids"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if body.Event.EventDate == "" {
		writeErr(w, 400, "event_date required")
		return
	}
	d, err := normDate("event_date", body.Event.EventDate)
	if err != nil {
		badRequestErr(w, err)
		return
	}
	body.Event.EventDate = d
	// 地点：locations 为准，location 存拼接串供搜索/兼容
	body.Event.Locations = cleanStrings(body.Event.Locations)
	if len(body.Event.Locations) > 0 {
		body.Event.Location = strings.Join(body.Event.Locations, " · ")
	}
	expense, err := eventExpense(&body.Event, body.ParticipantIDs)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.EventCreate(r.Context(), &body.Event, body.ParticipantIDs, expense); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, body.Event)
}

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// eventExpense 把事件表单上的开销金额翻译成要落账的那笔往来。
// 金额为空/0 时返回 nil，交给 store 在事务里清掉旧账。
func eventExpense(e *store.Event, participantIDs []string) (*store.EventExpense, error) {
	if e.ExpenseFen <= 0 {
		return nil, nil
	}
	person := e.ExpensePersonID
	if person == "" && len(participantIDs) > 0 {
		person = participantIDs[0]
	}
	if person == "" {
		return nil, errors.New("记录开销需要至少一位参与人（或指定开销归属人）")
	}
	return &store.EventExpense{
		PersonID:   person,
		AmountFen:  e.ExpenseFen,
		Title:      e.Title,
		OccurredAt: e.EventDate,
	}, nil
}

func (a *API) eventUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		store.Event
		ParticipantIDs []string `json:"participant_ids"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	body.Event.ID = id
	if body.Event.EventDate == "" {
		writeErr(w, 400, "event_date required")
		return
	}
	d, err := normDate("event_date", body.Event.EventDate)
	if err != nil {
		badRequestErr(w, err)
		return
	}
	body.Event.EventDate = d
	body.Event.Locations = cleanStrings(body.Event.Locations)
	if len(body.Event.Locations) > 0 {
		body.Event.Location = strings.Join(body.Event.Locations, " · ")
	}
	expense, err := eventExpense(&body.Event, body.ParticipantIDs)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.EventUpdate(r.Context(), &body.Event, body.ParticipantIDs, expense); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, body.Event)
}

func (a *API) eventDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.EventDelete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// ===== memos =====

func (a *API) registerMemos(r chi.Router) {
	r.Route("/api/v1/memos", func(r chi.Router) {
		r.Get("/", a.memoList)
		r.Post("/", a.memoCreate)
		r.Put("/{id}", a.memoUpdate)
		r.Delete("/{id}", a.memoDelete)
	})
}

func (a *API) memoList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.MemoList(r.Context(),
		r.URL.Query().Get("person_id"),
		r.URL.Query().Get("promises_only") == "1",
		parseIntQuery(r, "limit", 50), parseIntQuery(r, "offset", 0))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) memoCreate(w http.ResponseWriter, r *http.Request) {
	var m store.Memo
	if err := decode(r, &m); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if m.SaidAt == "" {
		writeErr(w, 400, "said_at required")
		return
	}
	if err := validateMemo(&m); err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.MemoCreate(r.Context(), &m); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, m)
}

func (a *API) memoUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var m store.Memo
	if err := decode(r, &m); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	m.ID = id
	if m.SaidAt == "" {
		writeErr(w, 400, "said_at required")
		return
	}
	if err := validateMemo(&m); err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.MemoUpdate(r.Context(), &m); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, m)
}

func (a *API) memoDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.MemoDelete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// ===== transactions / repayments =====

func (a *API) registerTransactions(r chi.Router) {
	r.Route("/api/v1/transactions", func(r chi.Router) {
		r.Get("/", a.txList)
		r.Get("/{id}", a.txGet)
		r.Post("/", a.txCreate)
		r.Put("/{id}", a.txUpdate)
		r.Delete("/{id}", a.txDelete)
		r.Post("/{id}/repayments", a.repaymentCreate)
		r.Get("/{id}/repayments", a.repaymentsList)
		r.Delete("/{id}/repayments/{rid}", a.repaymentDelete)
	})
}

func (a *API) txList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.TransactionList(r.Context(),
		r.URL.Query().Get("person_id"),
		parseIntQuery(r, "limit", 50), parseIntQuery(r, "offset", 0))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) txGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := a.Store.TransactionGet(r.Context(), id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, t)
}

// normalizeTx 收口「欠账」的语义：只有借还（loan）才有未结清这回事。
//
// 礼物/花销/其它是一笔了结的支出，前端不再给出「已结清」勾选，这里直接置位，
// 免得一条勾漏的礼物被算进首页的「我借出（未还）」并生成「某人待还 ¥N」的建议。
// kind 为空按「其它」处理，否则会留下既不是借还、又没有结清语义的死角数据。
func normalizeTx(t *store.Transaction) {
	if t.Kind == "" {
		t.Kind = "other"
	}
	if t.Kind != "loan" {
		t.Settled = true
	}
}

func (a *API) txCreate(w http.ResponseWriter, r *http.Request) {
	var t store.Transaction
	if err := decode(r, &t); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if t.OccurredAt == "" {
		writeErr(w, 400, "occurred_at required")
		return
	}
	normalizeTx(&t)
	if err := validateTx(&t); err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.TransactionCreate(r.Context(), &t); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

func (a *API) txUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var t store.Transaction
	if err := decode(r, &t); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	t.ID = id
	if t.OccurredAt == "" {
		writeErr(w, 400, "occurred_at required")
		return
	}
	normalizeTx(&t)
	if err := validateTx(&t); err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.TransactionUpdate(r.Context(), &t); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

func (a *API) txDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.TransactionDelete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) repaymentCreate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var rp store.Repayment
	if err := decode(r, &rp); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rp.TransactionID = id
	if rp.OccurredAt == "" {
		rp.OccurredAt = nowLocalStamp()[:10]
	}
	if err := positiveFen("amount_fen", rp.AmountFen); err != nil {
		badRequestErr(w, err)
		return
	}
	d, err := normDate("occurred_at", rp.OccurredAt)
	if err != nil {
		badRequestErr(w, err)
		return
	}
	rp.OccurredAt = d
	if err := a.Store.RepaymentCreate(r.Context(), &rp); err != nil {
		writeStoreErr(w, err)
		return
	}
	// 还款后自动校正结清状态，还完即结清、删掉还款则回到未结清
	if err := a.resyncSettled(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, rp)
}

// nowLocalStamp 返回本地时间戳（不带时区后缀），与 due_at/occurred_at 的口径一致。
func nowLocalStamp() string {
	return time.Now().Format("2006-01-02T15:04:05")
}

func (a *API) repaymentsList(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	list, err := a.Store.RepaymentsOf(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) repaymentDelete(w http.ResponseWriter, r *http.Request) {
	rid := chi.URLParam(r, "rid")
	if err := a.Store.RepaymentDelete(r.Context(), rid); err != nil {
		writeStoreErr(w, err)
		return
	}
	// 删掉一笔还款后重新计算结清状态，避免出现「已结清但仍有欠款」
	if err := a.resyncSettled(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// resyncSettled 按「本金 - 已还」自动校正交易的结清标记。
func (a *API) resyncSettled(ctx context.Context, txID string) error {
	if txID == "" {
		return nil
	}
	t, err := a.Store.TransactionGet(ctx, txID)
	if err != nil || t == nil {
		return nil
	}
	remaining := t.AmountFen - t.RepaidFen
	settled := remaining <= 0
	if settled == t.Settled {
		return nil
	}
	t.Settled = settled
	if settled {
		t.SettledAt = nowLocalStamp()
	} else {
		t.SettledAt = ""
	}
	return a.Store.TransactionUpdate(ctx, t)
}

// ===== anniversaries =====

func (a *API) registerAnniversaries(r chi.Router) {
	r.Route("/api/v1/anniversaries", func(r chi.Router) {
		r.Get("/", a.annivList)
		r.Post("/", a.annivCreate)
		r.Put("/{id}", a.annivUpdate)
		r.Delete("/{id}", a.annivDelete)
	})
}

func (a *API) annivList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.AnniversaryList(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) annivCreate(w http.ResponseWriter, r *http.Request) {
	var a2 store.Anniversary
	if err := decode(r, &a2); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if a2.Date == "" || a2.Title == "" {
		writeErr(w, 400, "title and date required")
		return
	}
	if err := validateAnniv(&a2); err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.AnniversaryCreate(r.Context(), &a2); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, a2)
}

func (a *API) annivUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var a2 store.Anniversary
	if err := decode(r, &a2); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	a2.ID = id
	if a2.Date == "" || a2.Title == "" {
		writeErr(w, 400, "title and date required")
		return
	}
	if err := validateAnniv(&a2); err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.AnniversaryUpdate(r.Context(), &a2); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, a2)
}

func (a *API) annivDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.AnniversaryDelete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// ===== reminders =====

func (a *API) registerReminders(r chi.Router) {
	r.Route("/api/v1/reminders", func(r chi.Router) {
		r.Get("/", a.reminderList)
		r.Get("/upcoming", a.upcoming)
		r.Post("/", a.reminderCreate)
		r.Put("/{id}", a.reminderUpdate)
		r.Delete("/{id}", a.reminderDelete)
		r.Post("/{id}/done", a.reminderDone)
	})
}

// reminderList 待办页列表。
//
// 除了 reminders 表里的行，还把纪念日与承诺派生出的待办一并列出：页面标题一直写着
// 「自定义 & 自动生成」，但过去只查表，派生项只在今日页露面，用户在待办页看到的
// 偏偏是空表。派生项不在表里，分页只能在合并排序之后切，所以表里的行先取全。
func (a *API) reminderList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := r.URL.Query().Get("status")
	limit := parseIntQuery(r, "limit", 50)
	offset := parseIntQuery(r, "offset", 0)
	list, err := a.Store.ReminderList(ctx, status, 0, 0)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if status == "" || status == "pending" {
		if annivs, err := a.Store.AnniversaryUpcoming(ctx, 0); err == nil {
			list = append(list, annivs...)
		}
		if promises, err := a.Store.PromiseUpcoming(ctx, 0); err == nil {
			list = append(list, promises...)
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].DueAt < list[j].DueAt })
	}
	if limit > 0 {
		if offset >= len(list) {
			list = []*store.Reminder{}
		} else {
			end := offset + limit
			if end > len(list) {
				end = len(list)
			}
			list = list[offset:end]
		}
	}
	writeJSON(w, 200, list)
}

func (a *API) upcoming(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ReminderUpcoming(r.Context(), parseIntQuery(r, "days", 30))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) reminderCreate(w http.ResponseWriter, r *http.Request) {
	var rm store.Reminder
	if err := decode(r, &rm); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(rm.Title) == "" {
		writeErr(w, 400, "title required")
		return
	}
	if strings.TrimSpace(rm.DueAt) == "" {
		writeErr(w, 400, "due_at required")
		return
	}
	if err := validateReminder(&rm); err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.ReminderCreate(r.Context(), &rm); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, rm)
}

// derivedReminder 判断 id 是不是派生待办的合成串（纪念日 / 承诺）。
//
// 这类行不在 reminders 表里：UPDATE/DELETE 会静默影响 0 行并回成功，
// 用户以为改掉了，刷新后又原样出现，所以除了「完成」都要挡回去。
func derivedReminder(id string) bool {
	return strings.HasPrefix(id, "anniv:") || strings.HasPrefix(id, "promise:")
}

func (a *API) reminderUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if derivedReminder(id) {
		writeErr(w, 400, "这条是自动生成的待办，请到纪念日或对话里修改")
		return
	}
	var rm store.Reminder
	if err := decode(r, &rm); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rm.ID = id
	if strings.TrimSpace(rm.Title) == "" {
		writeErr(w, 400, "title required")
		return
	}
	if strings.TrimSpace(rm.DueAt) == "" {
		writeErr(w, 400, "due_at required")
		return
	}
	if err := validateReminder(&rm); err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.ReminderUpdate(r.Context(), &rm); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, rm)
}

func (a *API) reminderDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if derivedReminder(id) {
		writeErr(w, 400, "这条是自动生成的待办，请到纪念日或对话里删除")
		return
	}
	if err := a.Store.ReminderDelete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// reminderDone 完成一条待办。
//
// 纪念日衍生待办的 id 是合成串 "anniv:<anniversary_id>:<date>:<offset>"，
// 它们并不存在于 reminders 表，走 UPDATE 会静默影响 0 行并返回 204，
// 用户以为已完成，刷新后又原样出现。这里单独落一条 dismiss 记录。
// 承诺衍生待办（"promise:<memo_id>"）同理，但语义更实：完成 = 承诺兑现。
func (a *API) reminderDone(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.HasPrefix(id, "anniv:") {
		rest := strings.TrimPrefix(id, "anniv:")
		annivID, occurrence, ok := strings.Cut(rest, ":")
		if !ok || annivID == "" || occurrence == "" {
			writeErr(w, 400, "bad anniversary reminder id")
			return
		}
		if err := a.Store.AnniversaryDismiss(r.Context(), annivID, occurrence); err != nil {
			writeStoreErr(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	if strings.HasPrefix(id, "promise:") {
		memoID := strings.TrimPrefix(id, "promise:")
		if memoID == "" {
			writeErr(w, 400, "bad promise reminder id")
			return
		}
		if err := a.Store.MemoFulfill(r.Context(), memoID); err != nil {
			writeStoreErr(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	if err := a.Store.ReminderDone(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}
