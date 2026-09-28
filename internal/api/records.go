package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
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
		writeErr(w, 500, err.Error())
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
		writeErr(w, 500, err.Error())
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
	// 地点：locations 为准，location 存拼接串供搜索/兼容
	body.Event.Locations = cleanStrings(body.Event.Locations)
	if len(body.Event.Locations) > 0 {
		body.Event.Location = strings.Join(body.Event.Locations, " · ")
	}
	expensePerson := body.Event.ExpensePersonID
	if expensePerson == "" && len(body.ParticipantIDs) > 0 {
		expensePerson = body.ParticipantIDs[0]
	}
	if body.Event.ExpenseFen > 0 && expensePerson == "" {
		writeErr(w, 400, "记录开销需要至少一位参与人（或指定开销归属人）")
		return
	}
	if err := a.Store.EventCreate(r.Context(), &body.Event, body.ParticipantIDs); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if body.Event.ExpenseFen > 0 {
		tx := &store.Transaction{
			PersonID:   expensePerson,
			Kind:       "expense",
			Direction:  "out",
			AmountFen:  body.Event.ExpenseFen,
			Title:      body.Event.Title,
			OccurredAt: body.Event.EventDate,
			Settled:    true,
			EventID:    body.Event.ID,
		}
		if err := a.Store.TransactionCreate(r.Context(), tx); err != nil {
			writeErr(w, 500, "事件已保存，但开销记账失败: "+err.Error())
			return
		}
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

// syncEventExpense 把事件的开销金额同步到 Money：有金额则 upsert 一条关联交易，
// 金额为 0 则删除该事件下由事件自动创建的 expense 交易。
func (a *API) syncEventExpense(ctx context.Context, e *store.Event, participantIDs []string) error {
	existing, err := a.Store.EventExpenses(ctx, e.ID)
	if err != nil {
		return err
	}
	if e.ExpenseFen <= 0 {
		for _, t := range existing {
			if t.Kind == "expense" {
				if err := a.Store.TransactionDelete(ctx, t.ID); err != nil {
					return err
				}
			}
		}
		return nil
	}
	person := e.ExpensePersonID
	if person == "" && len(participantIDs) > 0 {
		person = participantIDs[0]
	}
	if person == "" {
		return errors.New("记录开销需要至少一位参与人（或指定开销归属人）")
	}
	for _, t := range existing {
		if t.Kind != "expense" {
			continue
		}
		t.PersonID = person
		t.AmountFen = e.ExpenseFen
		t.Title = e.Title
		t.OccurredAt = e.EventDate
		t.Settled = true
		return a.Store.TransactionUpdate(ctx, t)
	}
	return a.Store.TransactionCreate(ctx, &store.Transaction{
		PersonID:   person,
		Kind:       "expense",
		Direction:  "out",
		AmountFen:  e.ExpenseFen,
		Title:      e.Title,
		OccurredAt: e.EventDate,
		Settled:    true,
		EventID:    e.ID,
	})
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
	body.Event.Locations = cleanStrings(body.Event.Locations)
	if len(body.Event.Locations) > 0 {
		body.Event.Location = strings.Join(body.Event.Locations, " · ")
	}
	if err := a.Store.EventUpdate(r.Context(), &body.Event, body.ParticipantIDs); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := a.syncEventExpense(r.Context(), &body.Event, body.ParticipantIDs); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, body.Event)
}

func (a *API) eventDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.EventDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
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
		writeErr(w, 500, err.Error())
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
	if err := a.Store.MemoCreate(r.Context(), &m); err != nil {
		writeErr(w, 500, err.Error())
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
	if err := a.Store.MemoUpdate(r.Context(), &m); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, m)
}

func (a *API) memoDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.MemoDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
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
		writeErr(w, 500, err.Error())
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
	if err := a.Store.TransactionCreate(r.Context(), &t); err != nil {
		writeErr(w, 500, err.Error())
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
	if err := a.Store.TransactionUpdate(r.Context(), &t); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, t)
}

func (a *API) txDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.TransactionDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
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
	if rp.AmountFen <= 0 {
		writeErr(w, 400, "amount_fen 必须大于 0")
		return
	}
	if err := a.Store.RepaymentCreate(r.Context(), &rp); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 还款后自动校正结清状态，还完即结清、删掉还款则回到未结清
	if err := a.resyncSettled(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
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
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) repaymentDelete(w http.ResponseWriter, r *http.Request) {
	rid := chi.URLParam(r, "rid")
	if err := a.Store.RepaymentDelete(r.Context(), rid); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 删掉一笔还款后重新计算结清状态，避免出现「已结清但仍有欠款」
	if err := a.resyncSettled(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeErr(w, 500, err.Error())
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
		writeErr(w, 500, err.Error())
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
	if err := a.Store.AnniversaryCreate(r.Context(), &a2); err != nil {
		writeErr(w, 500, err.Error())
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
	if err := a.Store.AnniversaryUpdate(r.Context(), &a2); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, a2)
}

func (a *API) annivDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.AnniversaryDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
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

func (a *API) reminderList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ReminderList(r.Context(),
		r.URL.Query().Get("status"),
		parseIntQuery(r, "limit", 50), parseIntQuery(r, "offset", 0))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) upcoming(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ReminderUpcoming(r.Context(), parseIntQuery(r, "days", 30))
	if err != nil {
		writeErr(w, 500, err.Error())
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
	if err := a.Store.ReminderCreate(r.Context(), &rm); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rm)
}

func (a *API) reminderUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var rm store.Reminder
	if err := decode(r, &rm); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rm.ID = id
	if err := a.Store.ReminderUpdate(r.Context(), &rm); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rm)
}

func (a *API) reminderDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.ReminderDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

// reminderDone 完成一条待办。
//
// 纪念日衍生待办的 id 是合成串 "anniv:<anniversary_id>:<date>:<offset>"，
// 它们并不存在于 reminders 表，走 UPDATE 会静默影响 0 行并返回 204，
// 用户以为已完成，刷新后又原样出现。这里单独落一条 dismiss 记录。
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
			writeErr(w, 500, err.Error())
			return
		}
		w.WriteHeader(204)
		return
	}
	if err := a.Store.ReminderDone(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}
