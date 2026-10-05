package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/qiansi/app/internal/lunar"

	"github.com/google/uuid"
	qiansiLunar "github.com/qiansi/app/internal/lunar"
)

// ===== Events =====

type Event struct {
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	TypeID          *int           `json:"type_id,omitempty"`
	TypeName        *string        `json:"type_name,omitempty"`
	TypeColor       *string        `json:"type_color,omitempty"`
	EventDate       string         `json:"event_date"`
	Location        string         `json:"location,omitempty"` // 兼容字段：地点拼接串
	Locations       []string       `json:"locations,omitempty"`
	HasGift         bool           `json:"has_gift"`
	Gift            string         `json:"gift,omitempty"`
	Summary         string         `json:"summary,omitempty"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
	Participants    []*Person      `json:"participants,omitempty"`
	ExpenseFen      int            `json:"expense_fen,omitempty"` // 关联开销合计（分）
	ExpensePersonID string         `json:"expense_person_id,omitempty"`
	Expenses        []*Transaction `json:"expenses,omitempty"`
	// 事件自带的那笔礼金（N2）。金额/方向/归属都是 transactions 的投影，
	// 写入时它们是表单上填的意图，读出时是账本上的现状；
	// GiftTransactionID 指向被事件认领的那一条，手工登记的礼单不在其列。
	GiftTransactionID string `json:"gift_transaction_id,omitempty"`
	GiftAmountFen     int    `json:"gift_amount_fen,omitempty"`
	GiftDirection     string `json:"gift_direction,omitempty"`
	GiftPersonID      string `json:"gift_person_id,omitempty"`
}

func encodeLocations(locs []string) string {
	clean := make([]string, 0, len(locs))
	for _, l := range locs {
		l = strings.TrimSpace(l)
		if l != "" {
			clean = append(clean, l)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return ""
	}
	return string(b)
}

func decodeLocations(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// EventExpense 是事件自带的那笔开销（kind='expense' 且 event_id 指向该事件）。
// 金额的真相在 transactions 里，事件上的 expense_fen 只是它的投影。
type EventExpense struct {
	PersonID   string
	AmountFen  int
	Title      string
	OccurredAt string
}

// applyEventExpense 在事件所在的事务里把开销对齐到 ex。
//
// 这一步不能挪到 API 层事后补写：事件提交成功、开销写入失败（归属人被并发删掉、
// 磁盘满、进程被杀）就会留下一张金额与账本对不上的事件，而且下次保存会再补一条。
func applyEventExpense(ctx context.Context, tx *sql.Tx, eventID string, ex *EventExpense) error {
	if ex == nil || ex.AmountFen <= 0 {
		_, err := tx.ExecContext(ctx, "DELETE FROM transactions WHERE event_id=? AND kind='expense'", eventID)
		return err
	}
	// 只保留一条：历史上重复保存可能挂出多笔
	var keep string
	err := tx.QueryRowContext(ctx,
		"SELECT id FROM transactions WHERE event_id=? AND kind='expense' ORDER BY created_at, id LIMIT 1", eventID).Scan(&keep)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if keep == "" {
		_, err := tx.ExecContext(ctx, `INSERT INTO transactions(id,person_id,kind,direction,amount_fen,title,occurred_at,due_date,settled,settled_at,created_at,event_id)
VALUES(?,?,'expense','out',?,?,?,NULL,1,?,?,?)`,
			uuid.NewString(), ex.PersonID, ex.AmountFen, nullableString(ex.Title), ex.OccurredAt, ex.OccurredAt, nowUTC(), eventID)
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM transactions WHERE event_id=? AND kind='expense' AND id<>?", eventID, keep); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE transactions SET person_id=?,direction='out',amount_fen=?,title=?,occurred_at=?,settled=1,settled_at=COALESCE(settled_at,?) WHERE id=?",
		ex.PersonID, ex.AmountFen, nullableString(ex.Title), ex.OccurredAt, ex.OccurredAt, keep)
	return err
}

// applyEventGift 在同一事务里对齐事件自带的那笔礼金（N2）。
//
// 与开销不同，这里只认 events.gift_transaction_id 指的那一条，绝不按 event_id 批量删：
// 一场宴席的礼单往往是几十笔 kind='gift'（每位来客一条，从金钱页手工挂上来），
// 事件表单再保存一次就把别人的账清光了。
//
// 指针失效的两种情况都当作「没有认领」：账目被删（外键 ON DELETE SET NULL 已经把列清空，
// 但同一事务里读到的可能还是旧值），或被人改挂到了别的事件上——那种情况下它属于另一个事件。
func applyEventGift(ctx context.Context, tx *sql.Tx, e *Event) error {
	owned := e.GiftTransactionID
	if owned != "" {
		var evID sql.NullString
		// 回收站里的账目不算认领对象：事件表单再保存既不该把它从垃圾堆里捞回来，
		// 也不该顺手把它删成永远找不回的孤儿。
		err := tx.QueryRowContext(ctx, "SELECT event_id FROM transactions WHERE id=? AND deleted_at IS NULL", owned).Scan(&evID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err != nil || !evID.Valid || evID.String != e.ID {
			owned = ""
		}
	}
	if e.GiftAmountFen <= 0 {
		had := e.GiftTransactionID != ""
		e.GiftTransactionID = ""
		if owned != "" {
			if _, err := tx.ExecContext(ctx, "DELETE FROM transactions WHERE id=?", owned); err != nil {
				return err
			}
		}
		if !had {
			return nil // 从来没有过礼金，不必回写指针
		}
		// 指针失效（指向别的事件）时也要落一个 NULL，别留一个永远认领不到的引用
		_, err := tx.ExecContext(ctx, "UPDATE events SET gift_transaction_id=NULL WHERE id=?", e.ID)
		return err
	}
	title := nullableString(e.Title)
	if owned == "" {
		owned = uuid.NewString()
		_, err := tx.ExecContext(ctx, `INSERT INTO transactions(id,person_id,kind,direction,amount_fen,title,occurred_at,due_date,settled,settled_at,created_at,event_id)
VALUES(?,?,'gift',?,?,?,?,NULL,1,?,?,?)`,
			owned, e.GiftPersonID, e.GiftDirection, e.GiftAmountFen, title, e.EventDate, e.EventDate, nowUTC(), e.ID)
		if err != nil {
			return err
		}
	} else {
		_, err := tx.ExecContext(ctx, "UPDATE transactions SET person_id=?,kind='gift',direction=?,amount_fen=?,title=?,occurred_at=?,event_id=?,settled=1,settled_at=COALESCE(settled_at,?) WHERE id=?",
			e.GiftPersonID, e.GiftDirection, e.GiftAmountFen, title, e.EventDate, e.ID, e.EventDate, owned)
		if err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE events SET gift_transaction_id=? WHERE id=?", owned, e.ID)
	if err == nil {
		e.GiftTransactionID = owned // 让新建事件的响应也带得上这个 id
	}
	return err
}

// EventCreate 写入事件、参与人与它自带的那笔开销和礼金。
// expense 为 nil 表示「这个事件没有开销」，会顺带清掉该事件下 kind='expense' 的往来。
func (s *Store) EventCreate(ctx context.Context, e *Event, participantIDs []string, expense *EventExpense) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	e.CreatedAt = nowUTC()
	e.UpdatedAt = e.CreatedAt
	e.GiftTransactionID = "" // 新建没有可认领的账，客户端传什么都不认
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO events(id,title,type_id,event_date,location,locations,has_gift,gift,summary,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
		e.ID, e.Title, e.TypeID, e.EventDate, e.Location, encodeLocations(e.Locations), e.HasGift, e.Gift, e.Summary, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return err
	}
	for _, pid := range participantIDs {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO event_participants(event_id,person_id) VALUES(?,?)", e.ID, pid); err != nil {
			return err
		}
	}
	if err := applyEventExpense(ctx, tx, e.ID, expense); err != nil {
		return err
	}
	if err := applyEventGift(ctx, tx, e); err != nil {
		return err
	}
	return tx.Commit()
}

// EventUpdate 语义同 EventCreate：事件字段、参与人、开销与礼金在一次事务里落定，
// 任何一步失败都整体回滚，不会出现「事件改了、账没改」。
func (s *Store) EventUpdate(ctx context.Context, e *Event, participantIDs []string, expense *EventExpense) error {
	e.UpdatedAt = nowUTC()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := execOne(ctx, tx, "UPDATE events SET title=?,type_id=?,event_date=?,location=?,locations=?,has_gift=?,gift=?,summary=?,updated_at=? WHERE id=? AND deleted_at IS NULL",
		e.Title, e.TypeID, e.EventDate, e.Location, encodeLocations(e.Locations), e.HasGift, e.Gift, e.Summary, e.UpdatedAt, e.ID); err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, "DELETE FROM event_participants WHERE event_id=?", e.ID)
	for _, pid := range participantIDs {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO event_participants(event_id,person_id) VALUES(?,?)", e.ID, pid); err != nil {
			return err
		}
	}
	if err := applyEventExpense(ctx, tx, e.ID, expense); err != nil {
		return err
	}
	// 礼金指针由服务端说了算：表单回不回传、传了什么，都不能让它去认领别的事件的账，
	// 也不能因为漏传就把原有那笔丢成永远认领不到的孤儿。
	// 只认回收站外的那一条：账目在垃圾堆里时指针读回来是空的，本次保存因此
	// 不会把指针写成 NULL——账目恢复之后，这层认领关系还在。
	var stored string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT t.id FROM live_transactions t
WHERE t.id=(SELECT gift_transaction_id FROM events WHERE id=?)),'')`, e.ID).Scan(&stored); err != nil {
		return err
	}
	e.GiftTransactionID = stored
	if err := applyEventGift(ctx, tx, e); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EventDelete(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "UPDATE events SET deleted_at=? WHERE id=? AND deleted_at IS NULL", nowUTC(), id)
}

// EventPurge 真删除一场往来。往来本身留在账上（钱确实花过），只是不再挂在事件上，
// 与旧版删除的口径一致。
func (s *Store) EventPurge(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 往来留着（钱确实花过），只是不再挂在事件上。外键本身带 ON DELETE SET NULL，
	// 这里显式置空是为了让「删事件不会删账」这个意图留在代码里。
	if _, err := tx.ExecContext(ctx, "UPDATE transactions SET event_id=NULL WHERE event_id=?", id); err != nil {
		return err
	}
	if err := execOne(ctx, tx, "DELETE FROM events WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// expense_fen 只统计支出方向（收到的礼金/回礼属入账，不算开销），
// 并且排掉事件自带那笔礼金：它已经由 gift_amount_fen 单独显示，
// 再算进「花费」会让同一笔 800 在一张卡片上出现两次。
// 后面三列是事件自带那笔礼金的投影：金额/方向/归属都从 transactions 读，
// 事件表里只留一个认领用的 id。
const eventColumns = `e.id,e.title,e.type_id,e.event_date,e.location,e.locations,e.has_gift,e.gift,e.summary,e.created_at,e.updated_at,et.name,et.color,
(SELECT COALESCE(SUM(t.amount_fen),0) FROM live_transactions t WHERE t.event_id=e.id AND t.direction='out' AND t.id IS NOT e.gift_transaction_id),
(SELECT COALESCE((SELECT t.id FROM live_transactions t WHERE t.id=e.gift_transaction_id),'')),
(SELECT t.amount_fen FROM live_transactions t WHERE t.id=e.gift_transaction_id),
(SELECT t.direction FROM live_transactions t WHERE t.id=e.gift_transaction_id),
(SELECT t.person_id FROM live_transactions t WHERE t.id=e.gift_transaction_id)`

func scanEvent(rows *sql.Rows) (*Event, error) {
	e := &Event{}
	var tn, tc, locs sql.NullString
	var giftTx, giftDir, giftPerson sql.NullString
	var giftFen sql.NullInt64
	if err := rows.Scan(&e.ID, &e.Title, &e.TypeID, &e.EventDate, &e.Location, &locs, &e.HasGift, &e.Gift, &e.Summary,
		&e.CreatedAt, &e.UpdatedAt, &tn, &tc, &e.ExpenseFen,
		&giftTx, &giftFen, &giftDir, &giftPerson); err != nil {
		return nil, err
	}
	if tn.Valid {
		e.TypeName = &tn.String
	}
	if tc.Valid {
		e.TypeColor = &tc.String
	}
	if giftTx.Valid {
		e.GiftTransactionID = giftTx.String
	}
	if giftFen.Valid {
		e.GiftAmountFen = int(giftFen.Int64)
	}
	if giftDir.Valid {
		e.GiftDirection = giftDir.String
	}
	if giftPerson.Valid {
		e.GiftPersonID = giftPerson.String
	}
	e.Locations = decodeLocations(locs.String)
	if len(e.Locations) == 0 && e.Location != "" {
		e.Locations = []string{e.Location}
	}
	return e, nil
}

// EventExpenses 返回挂在该事件下的开销/往来记录
func (s *Store) EventExpenses(ctx context.Context, eventID string) ([]*Transaction, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id,t.person_id,t.kind,t.direction,t.amount_fen,t.title,t.occurred_at,t.due_date,t.settled,t.settled_at,t.created_at,p.name
FROM live_transactions t LEFT JOIN live_people p ON p.id=t.person_id WHERE t.event_id=? ORDER BY t.occurred_at DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Transaction{}
	for rows.Next() {
		t := &Transaction{}
		var title, due, settledAt, personName sql.NullString
		if err := rows.Scan(&t.ID, &t.PersonID, &t.Kind, &t.Direction, &t.AmountFen, &title, &t.OccurredAt,
			&due, &t.Settled, &settledAt, &t.CreatedAt, &personName); err != nil {
			return nil, err
		}
		if title.Valid {
			t.Title = title.String
		}
		if due.Valid {
			t.DueDate = due.String
		}
		if settledAt.Valid {
			t.SettledAt = settledAt.String
		}
		if personName.Valid {
			t.PersonName = personName.String
		}
		t.EventID = eventID
		list = append(list, t)
	}
	return list, rows.Err()
}

func (s *Store) EventList(ctx context.Context, personID string, q string, limit, offset int) ([]*Event, error) {
	if limit <= 0 {
		limit = 100
	}
	qry := `SELECT ` + eventColumns + `
FROM live_events e LEFT JOIN event_types et ON e.type_id=et.id`
	var args []any
	var conds []string
	if personID != "" {
		conds = append(conds, "e.id IN (SELECT ep.event_id FROM event_participants ep WHERE ep.person_id=?)")
		args = append(args, personID)
	}
	if q != "" {
		conds = append(conds, "(e.title LIKE ? OR e.summary LIKE ? OR e.location LIKE ?)")
		q = "%" + q + "%"
		args = append(args, q, q, q)
	}
	if len(conds) > 0 {
		qry += " WHERE " + strings.Join(conds, " AND ")
	}
	qry += " ORDER BY e.event_date DESC, e.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := s.DB.QueryContext(ctx, qry, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	// 参与人一次补齐。连接池只有一条连接，逐场查等于让几十个 SELECT 排队。
	ids := make([]string, len(list))
	for i, e := range list {
		ids[i] = e.ID
	}
	parts, err := s.eventParticipantsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, e := range list {
		e.Participants = parts[e.ID]
	}
	return list, rows.Err()
}

// eventParticipantsFor 批量取一批往来的参与人，按登记顺序返回。
func (s *Store) eventParticipantsFor(ctx context.Context, ids []string) (map[string][]*Person, error) {
	out := map[string][]*Person{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT ep.event_id,p.id,p.name
FROM event_participants ep JOIN live_people p ON p.id=ep.person_id
WHERE ep.event_id IN (`+ph+`) ORDER BY ep.event_id,ep.rowid`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var eventID, pid, name string
		if err := rows.Scan(&eventID, &pid, &name); err != nil {
			rows.Close()
			return nil, err
		}
		out[eventID] = append(out[eventID], &Person{ID: pid, Name: name})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	defer rows.Close()
	return out, nil
}

// ===== Memos =====

type Memo struct {
	ID        string `json:"id"`
	PersonID  string `json:"person_id,omitempty"`
	// PersonName 只用于读取：对话列表要显示「谁说的」，
	// 但前端已经不再为了这几个名字拉全量名单。
	PersonName string `json:"person_name,omitempty"`
	Speaker   string `json:"speaker"`
	Content   string `json:"content"`
	SaidAt    string `json:"said_at"`
	IsPromise bool   `json:"is_promise"`
	DueDate   string `json:"due_date,omitempty"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) MemoCreate(ctx context.Context, m *Memo) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	if m.Speaker == "" {
		m.Speaker = "other"
	}
	if m.Status == "" {
		m.Status = "open"
	}
	m.CreatedAt = nowUTC()
	var personID any
	if m.PersonID != "" { personID = m.PersonID } else { personID = nil }
	var dueDate any
	if m.DueDate != "" { dueDate = m.DueDate } else { dueDate = nil }
	_, err := s.DB.ExecContext(ctx, "INSERT INTO memos(id,person_id,speaker,content,said_at,is_promise,due_date,status,created_at) VALUES(?,?,?,?,?,?,?,?,?)",
		m.ID, personID, m.Speaker, m.Content, m.SaidAt, m.IsPromise, dueDate, m.Status, m.CreatedAt)
	return err
}

func (s *Store) MemoUpdate(ctx context.Context, m *Memo) error {
	var personID any
	if m.PersonID != "" { personID = m.PersonID } else { personID = nil }
	var dueDate any
	if m.DueDate != "" { dueDate = m.DueDate } else { dueDate = nil }
	return execOne(ctx, s.DB, "UPDATE memos SET person_id=?,speaker=?,content=?,said_at=?,is_promise=?,due_date=?,status=? WHERE id=? AND deleted_at IS NULL", personID, m.Speaker, m.Content, m.SaidAt, m.IsPromise, dueDate, m.Status, m.ID)
}

// MemoDelete 收进回收站，MemoPurge 才是真删。
func (s *Store) MemoDelete(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "UPDATE memos SET deleted_at=? WHERE id=? AND deleted_at IS NULL", nowUTC(), id)
}

func (s *Store) MemoPurge(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "DELETE FROM memos WHERE id=?", id)
}

func (s *Store) MemoList(ctx context.Context, personID string, promisesOnly bool, limit, offset int) ([]*Memo, error) {
	if limit <= 0 { limit = 50 }
	q := `SELECT m.id,m.person_id,p.name,m.speaker,m.content,m.said_at,m.is_promise,m.due_date,m.status,m.created_at FROM live_memos m LEFT JOIN live_people p ON p.id=m.person_id WHERE 1=1`
	var args []any
	if personID != "" {
		q += " AND m.person_id=?"; args = append(args, personID)
	}
	if promisesOnly {
		q += " AND m.is_promise=1"
	}
	q += " ORDER BY m.said_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*Memo{}
	for rows.Next() {
		m := &Memo{}
		var personIDStr, name, due sql.NullString
		if err := rows.Scan(&m.ID, &personIDStr, &name, &m.Speaker, &m.Content, &m.SaidAt, &m.IsPromise, &due, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		if personIDStr.Valid { m.PersonID = personIDStr.String }
		if name.Valid { m.PersonName = name.String }
		if due.Valid { m.DueDate = due.String }
		list = append(list, m)
	}
	return list, rows.Err()
}

// ===== Relationships =====

type Relationship struct {
	ID         string `json:"id"`
	FromPerson string `json:"from_person_id"`
	ToPerson   string `json:"to_person_id"`
	Type       string `json:"type"`
	Remark     string `json:"remark,omitempty"`
	CreatedAt  string `json:"created_at"`
	FromName   string `json:"from_name,omitempty"`
	ToName     string `json:"to_name,omitempty"`
}

// ErrRelationshipExists 同一对人之间已经有同名的关系。
var ErrRelationshipExists = errors.New("这对人之间已经有同名的关系了")

// isUniqueRel 只认关系边唯一键撞了，别的外键/非空错误照常往上抛，
// 免得把真故障伪装成「已存在」。
func isUniqueRel(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: relationships")
}

func (s *Store) RelationshipCreate(ctx context.Context, r *Relationship) error {
	if r.ID == "" { r.ID = uuid.NewString() }
	r.CreatedAt = nowUTC()
	var remark any
	if r.Remark != "" { remark = r.Remark } else { remark = nil }
	// 用普通 INSERT：旧版是 INSERT OR REPLACE，同一对人再建一条会把前一条静默顶掉。
	_, err := s.DB.ExecContext(ctx, "INSERT INTO relationships(id,from_person_id,to_person_id,type,remark,created_at) VALUES(?,?,?,?,?,?)",
		r.ID, r.FromPerson, r.ToPerson, r.Type, remark, r.CreatedAt)
	if isUniqueRel(err) {
		return ErrRelationshipExists
	}
	return err
}

// RelationshipUpdate 改两端、类型与备注；ID 不变，created_at 保持原值
func (s *Store) RelationshipUpdate(ctx context.Context, r *Relationship) error {
	var remark any
	if r.Remark != "" {
		remark = r.Remark
	} else {
		remark = nil
	}
	err := execOne(ctx, s.DB, "UPDATE relationships SET from_person_id=?,to_person_id=?,type=?,remark=? WHERE id=?",
		r.FromPerson, r.ToPerson, r.Type, remark, r.ID)
	if isUniqueRel(err) {
		return ErrRelationshipExists
	}
	return err
}

func (s *Store) RelationshipDelete(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "DELETE FROM relationships WHERE id=?", id)
}

func (s *Store) RelationshipList(ctx context.Context) ([]*Relationship, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id,r.from_person_id,r.to_person_id,r.type,r.remark,r.created_at,
pf.name,pt.name
FROM relationships r
LEFT JOIN live_people pf ON pf.id=r.from_person_id
LEFT JOIN live_people pt ON pt.id=r.to_person_id
WHERE pf.id IS NOT NULL AND pt.id IS NOT NULL`)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*Relationship{}
	for rows.Next() {
		r := &Relationship{}
		var remark, fn, tn sql.NullString
		if err := rows.Scan(&r.ID, &r.FromPerson, &r.ToPerson, &r.Type, &remark, &r.CreatedAt, &fn, &tn); err != nil {
			return nil, err
		}
		if remark.Valid { r.Remark = remark.String }
		if fn.Valid { r.FromName = fn.String }
		if tn.Valid { r.ToName = tn.String }
		list = append(list, r)
	}
	return list, rows.Err()
}

// ===== Transactions =====

type Transaction struct {
	ID         string `json:"id"`
	PersonID   string `json:"person_id"`
	Kind       string `json:"kind"`
	Direction  string `json:"direction"`
	AmountFen  int    `json:"amount_fen"`
	Title      string `json:"title"`
	OccurredAt string `json:"occurred_at"`
	DueDate    string `json:"due_date,omitempty"`
	Settled    bool   `json:"settled"`
	SettledAt  string `json:"settled_at,omitempty"`
	CreatedAt  string `json:"created_at"`
	PersonName string `json:"person_name,omitempty"`
	RepaidFen  int    `json:"repaid_fen,omitempty"`
	EventID    string `json:"event_id,omitempty"`
	EventTitle string `json:"event_title,omitempty"`
}

type Repayment struct {
	ID            string `json:"id"`
	TransactionID string `json:"transaction_id"`
	AmountFen     int    `json:"amount_fen"`
	OccurredAt    string `json:"occurred_at"`
	Note          string `json:"note,omitempty"`
}

func (s *Store) TransactionCreate(ctx context.Context, t *Transaction) error {
	if t.ID == "" { t.ID = uuid.NewString() }
	if t.Kind == "" { t.Kind = "other" }
	t.CreatedAt = nowUTC()
	var dueDate any
	if t.DueDate != "" { dueDate = t.DueDate } else { dueDate = nil }
	var settledAt any
	if t.Settled && t.SettledAt == "" { t.SettledAt = t.OccurredAt }
	if t.SettledAt != "" { settledAt = t.SettledAt } else { settledAt = nil }
	var title any
	if t.Title != "" { title = t.Title } else { title = nil }
	var eventID any
	if t.EventID != "" { eventID = t.EventID } else { eventID = nil }
	_, err := s.DB.ExecContext(ctx, "INSERT INTO transactions(id,person_id,kind,direction,amount_fen,title,occurred_at,due_date,settled,settled_at,created_at,event_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
		t.ID, t.PersonID, t.Kind, t.Direction, t.AmountFen, title, t.OccurredAt, dueDate, t.Settled, settledAt, t.CreatedAt, eventID)
	if err == nil && t.EventID != "" && t.EventTitle == "" {
		_ = s.DB.QueryRowContext(ctx, "SELECT title FROM live_events WHERE id=?", t.EventID).Scan(&t.EventTitle)
	}
	return err
}

func (s *Store) TransactionUpdate(ctx context.Context, t *Transaction) error {
	var dueDate any
	if t.DueDate != "" { dueDate = t.DueDate } else { dueDate = nil }
	var settledAt any
	if t.SettledAt != "" { settledAt = t.SettledAt } else { settledAt = nil }
	var title any
	if t.Title != "" { title = t.Title } else { title = nil }
	var eventID any
	if t.EventID != "" { eventID = t.EventID } else { eventID = nil }
	return execOne(ctx, s.DB, "UPDATE transactions SET person_id=?,kind=?,direction=?,amount_fen=?,title=?,occurred_at=?,due_date=?,settled=?,settled_at=?,event_id=? WHERE id=? AND deleted_at IS NULL",
		t.PersonID, t.Kind, t.Direction, t.AmountFen, title, t.OccurredAt, dueDate, t.Settled, settledAt, eventID, t.ID)
}

// TransactionDelete 收进回收站。它名下的还款留在表里（外键没触发），
// 恢复时本金与还款的账仍然对得上；真删除才按级联清掉还款。
func (s *Store) TransactionDelete(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "UPDATE transactions SET deleted_at=? WHERE id=? AND deleted_at IS NULL", nowUTC(), id)
}

func (s *Store) TransactionPurge(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "DELETE FROM transactions WHERE id=?", id)
}

const txColumns = `t.id,t.person_id,t.kind,t.direction,t.amount_fen,t.title,t.occurred_at,t.due_date,t.settled,t.settled_at,t.created_at,p.name,
COALESCE((SELECT COALESCE(SUM(r.amount_fen),0) FROM repayments r WHERE r.transaction_id=t.id),0),
t.event_id,(SELECT ev.title FROM live_events ev WHERE ev.id=t.event_id)`

func scanTransaction(rows *sql.Rows) (*Transaction, error) {
	t := &Transaction{}
	var title, due, settledAt, personName, eventID, eventTitle sql.NullString
	if err := rows.Scan(&t.ID, &t.PersonID, &t.Kind, &t.Direction, &t.AmountFen, &title, &t.OccurredAt, &due, &t.Settled,
		&settledAt, &t.CreatedAt, &personName, &t.RepaidFen, &eventID, &eventTitle); err != nil {
		return nil, err
	}
	if title.Valid { t.Title = title.String }
	if due.Valid { t.DueDate = due.String }
	if settledAt.Valid { t.SettledAt = settledAt.String }
	if personName.Valid { t.PersonName = personName.String }
	if eventID.Valid { t.EventID = eventID.String }
	if eventTitle.Valid { t.EventTitle = eventTitle.String }
	return t, nil
}

func (s *Store) TransactionGet(ctx context.Context, id string) (*Transaction, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+txColumns+`
FROM live_transactions t LEFT JOIN live_people p ON p.id=t.person_id WHERE t.id=?`, id)
	if err != nil { return nil, err }
	defer rows.Close()
	if !rows.Next() { return nil, sql.ErrNoRows }
	return scanTransaction(rows)
}

func (s *Store) TransactionList(ctx context.Context, personID string, limit, offset int) ([]*Transaction, error) {
	if limit <= 0 { limit = 200 }
	q := `SELECT ` + txColumns + `
FROM live_transactions t LEFT JOIN live_people p ON p.id=t.person_id WHERE 1=1`
	var args []any
	if personID != "" {
		q += " AND t.person_id=?"; args = append(args, personID)
	}
	q += " ORDER BY t.occurred_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*Transaction{}
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// ===== Anniversaries =====

type Anniversary struct {
	ID           string `json:"id"`
	PersonID     string `json:"person_id,omitempty"`
	Title        string `json:"title"`
	Date         string `json:"date"`
	IsLunar      bool   `json:"is_lunar"`
	RepeatYearly bool   `json:"repeat_yearly"`
	RemindDays   string `json:"remind_days"`
	CreatedAt    string `json:"created_at"`
	PersonName   string `json:"person_name,omitempty"`
	// NextDate 下一次发生的公历日期（YYYY-MM-DD）；每年循环/农历项由后端计算。
	// DaysUntil 为 nil 表示无下一次（不循环且已过）。
	NextDate  string `json:"next_date,omitempty"`
	DaysUntil *int   `json:"days_until,omitempty"`
}

func (s *Store) AnniversaryCreate(ctx context.Context, a *Anniversary) error {
	if a.ID == "" { a.ID = uuid.NewString() }
	if a.RemindDays == "" { a.RemindDays = "7,3,1,0" }
	a.CreatedAt = nowUTC()
	var personID any
	if a.PersonID != "" { personID = a.PersonID } else { personID = nil }
	_, err := s.DB.ExecContext(ctx, "INSERT INTO anniversaries(id,person_id,title,date,is_lunar,repeat_yearly,remind_days,created_at) VALUES(?,?,?,?,?,?,?,?)",
		a.ID, personID, a.Title, a.Date, a.IsLunar, a.RepeatYearly, a.RemindDays, a.CreatedAt)
	return err
}

func (s *Store) AnniversaryUpdate(ctx context.Context, a *Anniversary) error {
	var personID any
	if a.PersonID != "" { personID = a.PersonID } else { personID = nil }
	return execOne(ctx, s.DB, "UPDATE anniversaries SET person_id=?,title=?,date=?,is_lunar=?,repeat_yearly=?,remind_days=? WHERE id=? AND deleted_at IS NULL", personID, a.Title, a.Date, a.IsLunar, a.RepeatYearly, a.RemindDays, a.ID)
}

// AnniversaryDelete 收进回收站。派生出的提醒随视图一起消失，
// 已经点过「完成」的那次发生（anniversary_dismiss）原样留着，恢复后不会凭空再响一次。
func (s *Store) AnniversaryDelete(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "UPDATE anniversaries SET deleted_at=? WHERE id=? AND deleted_at IS NULL", nowUTC(), id)
}

func (s *Store) AnniversaryPurge(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "DELETE FROM anniversaries WHERE id=?", id)
}

func (s *Store) AnniversaryList(ctx context.Context) ([]*Anniversary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id,a.person_id,a.title,a.date,a.is_lunar,a.repeat_yearly,a.remind_days,a.created_at,p.name
FROM live_anniversaries a LEFT JOIN live_people p ON p.id=a.person_id`)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*Anniversary{}
	for rows.Next() {
		a := &Anniversary{}
		var personID, personName sql.NullString
		if err := rows.Scan(&a.ID, &personID, &a.Title, &a.Date, &a.IsLunar, &a.RepeatYearly, &a.RemindDays, &a.CreatedAt, &personName); err != nil {
			return nil, err
		}
		if personID.Valid { a.PersonID = personID.String }
		if personName.Valid { a.PersonName = personName.String }
		list = append(list, a)
	}
	if err := rows.Err(); err != nil { return nil, err }

	// 计算下一次发生日期与倒计时，并按「即将到来在前、已过在后」排序。
	today := lunar.NowLocal()
	for _, a := range list {
		d := lunar.ParseDate(a.Date)
		if d.Year == 0 || d.Month < 1 || d.Day < 1 {
			continue // 日期异常，跳过
		}
		if a.RepeatYearly {
			next := lunar.NextOccurrence(d, a.IsLunar, today)
			a.NextDate = next.String()
			n := lunar.DaysBetween(today, next)
			a.DaysUntil = &n
		} else {
			next := lunar.YMD{Year: d.Year, Month: d.Month, Day: d.Day}
			if !next.Before(today) {
				a.NextDate = next.String()
				n := lunar.DaysBetween(today, next)
				a.DaysUntil = &n
			}
			// 不循环且已过：NextDate/DaysUntil 为空，排在最后
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		di, dj := list[i].DaysUntil, list[j].DaysUntil
		switch {
		case di != nil && dj != nil:
			if *di != *dj { return *di < *dj }
			return list[i].Date < list[j].Date
		case di != nil:
			return true
		case dj != nil:
			return false
		default:
			return list[i].Date < list[j].Date
		}
	})
	return list, nil
}

// ===== Reminders =====

type Reminder struct {
	ID          string `json:"id"`
	PersonID    string `json:"person_id,omitempty"`
	RefType     string `json:"ref_type"`
	RefID       string `json:"ref_id,omitempty"`
	Title       string `json:"title"`
	DueAt       string `json:"due_at"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	CompletedAt string `json:"completed_at,omitempty"`
	PersonName  string `json:"person_name,omitempty"`
}

func (s *Store) ReminderCreate(ctx context.Context, r *Reminder) error {
	if r.ID == "" { r.ID = uuid.NewString() }
	if r.RefType == "" { r.RefType = "custom" }
	if r.Status == "" { r.Status = "pending" }
	r.CreatedAt = nowUTC()
	var personID any
	if r.PersonID != "" { personID = r.PersonID } else { personID = nil }
	var refID any
	if r.RefID != "" { refID = r.RefID } else { refID = nil }
	_, err := s.DB.ExecContext(ctx, "INSERT INTO reminders(id,person_id,ref_type,ref_id,title,due_at,status,created_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?)",
		r.ID, personID, r.RefType, refID, r.Title, r.DueAt, r.Status, r.CreatedAt, nil)
	return err
}

func (s *Store) ReminderUpdate(ctx context.Context, r *Reminder) error {
	var personID any
	if r.PersonID != "" { personID = r.PersonID } else { personID = nil }
	var refID any
	if r.RefID != "" { refID = r.RefID } else { refID = nil }
	var completedAt any
	if r.CompletedAt != "" { completedAt = r.CompletedAt } else { completedAt = nil }
	return execOne(ctx, s.DB, "UPDATE reminders SET person_id=?,ref_type=?,ref_id=?,title=?,due_at=?,status=?,completed_at=? WHERE id=? AND deleted_at IS NULL", personID, r.RefType, refID, r.Title, r.DueAt, r.Status, completedAt, r.ID)
}

// ReminderDelete 收进回收站，只针对表里的自定义待办；
// 纪念日/承诺派生的待办不在表里，由各自的来源记录回收（见 API 层的 derivedReminder）。
func (s *Store) ReminderDelete(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "UPDATE reminders SET deleted_at=? WHERE id=? AND deleted_at IS NULL", nowUTC(), id)
}

func (s *Store) ReminderPurge(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "DELETE FROM reminders WHERE id=?", id)
}

func (s *Store) ReminderDone(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "UPDATE reminders SET status='done',completed_at=? WHERE id=? AND deleted_at IS NULL", nowUTC(), id)
}

// ReminderList 列出 reminders 表里的待办。
//
// limit<=0 表示不限：API 层要把纪念日/承诺的派生待办合并进来后再分页，
// 表里的行必须先取全，否则合并后的顺序与 offset 都对不上。
func (s *Store) ReminderList(ctx context.Context, status string, limit, offset int) ([]*Reminder, error) {
	q := `SELECT r.id,r.person_id,r.ref_type,r.ref_id,r.title,r.due_at,r.status,r.created_at,r.completed_at,p.name
FROM live_reminders r LEFT JOIN live_people p ON r.person_id=p.id WHERE 1=1`
	var args []any
	if status != "" {
		q += " AND r.status=?"; args = append(args, status)
	}
	q += " ORDER BY r.due_at ASC"
	if limit > 0 {
		q += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*Reminder{}
	for rows.Next() {
		r := &Reminder{}
		var personID, refID, completedAt, personName sql.NullString
		if err := rows.Scan(&r.ID, &personID, &r.RefType, &refID, &r.Title, &r.DueAt, &r.Status, &r.CreatedAt, &completedAt, &personName); err != nil {
			return nil, err
		}
		if personID.Valid { r.PersonID = personID.String }
		if refID.Valid { r.RefID = refID.String }
		if completedAt.Valid { r.CompletedAt = completedAt.String }
		if personName.Valid { r.PersonName = personName.String }
		list = append(list, r)
	}
	return list, rows.Err()
}

// ReminderUpcoming returns explicit reminders plus entries derived from
// anniversaries (next solar occurrence within `horizonDays`) and from open
// promises whose due date has arrived or passed.
//
// 时间基准统一为本地时区（见 store.nowLocal 注释）；horizonDays<=0 表示不限。
func (s *Store) ReminderUpcoming(ctx context.Context, horizonDays int) ([]*Reminder, error) {
	q := `SELECT r.id,r.person_id,r.ref_type,r.ref_id,r.title,r.due_at,r.status,r.created_at,r.completed_at,p.name
FROM live_reminders r LEFT JOIN live_people p ON r.person_id=p.id
WHERE r.status='pending'`
	var args []any
	if horizonDays > 0 {
		q += " AND r.due_at<=?"
		args = append(args, localCutoff(horizonDays))
	}
	q += " ORDER BY r.due_at ASC"
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil { return nil, err }
	list := []*Reminder{}
	for rows.Next() {
		r := &Reminder{}
		var pid, refid, completed, pn sql.NullString
		if err := rows.Scan(&r.ID, &pid, &r.RefType, &refid, &r.Title, &r.DueAt, &r.Status, &r.CreatedAt, &completed, &pn); err != nil {
			rows.Close()
			return nil, err
		}
		if pid.Valid { r.PersonID = pid.String }
		if refid.Valid { r.RefID = refid.String }
		if completed.Valid { r.CompletedAt = completed.String }
		if pn.Valid { r.PersonName = pn.String }
		list = append(list, r)
	}
	rows.Close() // 连接池只有 1 条，必须先释放再查纪念日
	// Merge anniversary-derived reminders (lunar→solar)
	annivs, err := s.AnniversaryUpcoming(ctx, horizonDays)
	if err == nil {
		list = append(list, annivs...)
	}
	// Merge open promises that have reached (or passed) their due date
	promises, err := s.PromiseUpcoming(ctx, horizonDays)
	if err == nil {
		list = append(list, promises...)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].DueAt < list[j].DueAt })
	return list, nil
}

// AnniversaryUpcoming returns Reminder-shaped entries for anniversaries whose
// next occurrence (lunar→solar if is_lunar=true) falls within `horizonDays`.
//
// 已经「完成」过的本次提醒（anniversary_dismiss）会被跳过，
// 否则用户在今日页勾掉纪念日后，刷新又会原样出现。
func (s *Store) AnniversaryUpcoming(ctx context.Context, horizonDays int) ([]*Reminder, error) {
	today := qiansiLunar.NowLocal()
	cutoff := today.AddDays(horizonDays)
	// 注意：连接池上限是 1（db.SetMaxOpenConns(1)），查询期间不能再发起其它查询，
	// 否则会互相等待造成死锁。这里先把结果全部读进内存并关闭 rows，再做后续计算。
	type annivRow struct {
		id, personID, title, date, remindDays, personName string
		isLunar                                           bool
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id,a.person_id,a.title,a.date,a.is_lunar,a.remind_days,p.name
FROM live_anniversaries a LEFT JOIN live_people p ON p.id=a.person_id`)
	if err != nil { return nil, err }
	raw := []annivRow{}
	for rows.Next() {
		var anID, personID, title, date, remindDays, personName sql.NullString
		var isLunar bool
		if err := rows.Scan(&anID, &personID, &title, &date, &isLunar, &remindDays, &personName); err != nil {
			rows.Close()
			return nil, err
		}
		raw = append(raw, annivRow{anID.String, personID.String, title.String, date.String, remindDays.String, personName.String, isLunar})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	dismissed := s.dismissedOccurrences(ctx)
	list := []*Reminder{}
	for _, a := range raw {
		if a.date == "" || a.title == "" { continue }
		anID, personID, title, date, remindDays, personName :=
			sql.NullString{String: a.id, Valid: a.id != ""},
			sql.NullString{String: a.personID, Valid: a.personID != ""},
			sql.NullString{String: a.title, Valid: true},
			sql.NullString{String: a.date, Valid: true},
			sql.NullString{String: a.remindDays, Valid: a.remindDays != ""},
			sql.NullString{String: a.personName, Valid: a.personName != ""}
		isLunar := a.isLunar
		anchor := qiansiLunar.ParseDate(date.String)
		next := qiansiLunar.NextOccurrence(anchor, isLunar, today)
		offsets := qiansiLunar.RemindDates(remindDays.String)
		if len(offsets) == 0 { offsets = []int{0} }
		for _, offset := range offsets {
			rd := next.AddDays(-offset)
			if rd.Before(today) { continue }
			if horizonDays > 0 && cutoff.Before(rd) { continue }
			// 本次发生 + 提前量构成唯一的一次提醒
			occurrence := next.String() + ":" + strconv.Itoa(offset)
			if dismissed[anID.String+"|"+occurrence] { continue }
			personIDStr := ""
			if personID.Valid { personIDStr = personID.String }
			pn := ""
			if personName.Valid { pn = personName.String }
			typeTag := ""
			if isLunar { typeTag = " 农历" }
			whenTag := ""
			if offset > 0 { whenTag = "，还有 " + strconv.Itoa(offset) + " 天" } else { whenTag = "，今天" }
			r := &Reminder{
				ID:         "anniv:" + anID.String + ":" + occurrence,
				PersonID:   personIDStr,
				RefType:    "anniversary",
				RefID:      anID.String,
				Title:      title.String + "（" + next.String() + typeTag + whenTag + "）",
				DueAt:      rd.String() + "T09:00:00",
				Status:     "pending",
				PersonName: pn,
			}
			list = append(list, r)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].DueAt < list[j].DueAt })
	return list, nil
}

// dismissedOccurrences 返回已被标记为完成的「纪念日 + 本次发生」集合，
// key 形如 "<anniversary_id>|<YYYY-MM-DD>:<offset>"。
func (s *Store) dismissedOccurrences(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	rows, err := s.DB.QueryContext(ctx, "SELECT anniversary_id,occurrence FROM anniversary_dismiss")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, occ string
		if err := rows.Scan(&id, &occ); err == nil {
			out[id+"|"+occ] = true
		}
	}
	return out
}

// AnniversaryDismiss 把某次纪念日提醒标记为已完成（occurrence 形如 "2026-10-01:0"）。
func (s *Store) AnniversaryDismiss(ctx context.Context, anniversaryID, occurrence string) error {
	if anniversaryID == "" || occurrence == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx,
		"INSERT OR IGNORE INTO anniversary_dismiss(anniversary_id,occurrence,created_at) VALUES(?,?,?)",
		anniversaryID, occurrence, nowUTC())
	return err
}

// PromiseUpcoming 把「答应帮 TA 办的事」派生成待办：只取承诺、仍挂着、且写了到期日的。
//
// 承诺是人情往来里最容易失信、后果最重的一类记录，但它过去只躺在 memos 表里，
// 到期那天既不出现在待办里，也不进每日摘要 —— 提醒链断在最需要它的一环。
//
// 与纪念日不同，承诺本身有状态（open/fulfilled/broken），勾掉即转 fulfilled，
// 不需要额外的 dismiss 表；逾期的照旧返回，因为「到期没办」才是最该被看到的一条。
// horizonDays<=0 表示不设上限。
func (s *Store) PromiseUpcoming(ctx context.Context, horizonDays int) ([]*Reminder, error) {
	q := `SELECT m.id,m.person_id,m.content,m.due_date,p.name FROM live_memos m
LEFT JOIN live_people p ON p.id=m.person_id
WHERE m.is_promise=1 AND m.status='open' AND m.due_date IS NOT NULL AND m.due_date <> ''`
	var args []any
	if horizonDays > 0 {
		// 到期日可能存成 YYYY-MM-DD 或带时间的串，取前 10 位按日期比较
		q += " AND substr(m.due_date,1,10)<=?"
		args = append(args, daysFromTodayLocal(horizonDays))
	}
	q += " ORDER BY substr(m.due_date,1,10) ASC"
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Reminder{}
	for rows.Next() {
		var id, content, dueDate, personID, personName sql.NullString
		if err := rows.Scan(&id, &personID, &content, &dueDate, &personName); err != nil {
			return nil, err
		}
		day := dueDate.String
		if len(day) > 10 {
			day = day[:10]
		}
		if day == "" {
			continue
		}
		text := strings.TrimSpace(content.String)
		if r := []rune(text); len(r) > 40 {
			text = string(r[:40]) + "…"
		}
		list = append(list, &Reminder{
			ID:         "promise:" + id.String,
			PersonID:   personID.String,
			RefType:    "promise",
			RefID:      id.String,
			Title:      "承诺：" + text,
			DueAt:      day + "T09:00:00",
			Status:     "pending",
			PersonName: personName.String,
		})
	}
	return list, rows.Err()
}

// MemoFulfill 把一条承诺记为已兑现。只动承诺项，避免误改普通对话的状态。
func (s *Store) MemoFulfill(ctx context.Context, memoID string) error {
	return execOne(ctx, s.DB,
		"UPDATE memos SET status='fulfilled' WHERE id=? AND is_promise=1 AND deleted_at IS NULL", memoID)
}

// ===== Global search =====

type SearchResult struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Date    string `json:"date,omitempty"`
	Path    string `json:"path"`
}

// Search 跨人物 / 往来 / 对话 / 金钱 / 纪念日做一次统一检索。
// 各分支独立 LIMIT，避免某类数据过多把其它类别挤掉。
func (s *Store) Search(ctx context.Context, q string, perType int) ([]*SearchResult, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []*SearchResult{}, nil
	}
	if perType <= 0 {
		perType = 5
	}
	like := "%" + q + "%"
	out := []*SearchResult{}

	// 人物
	people, err := s.PersonList(ctx, q, 0, 0, true, 0, perType, 0)
	if err == nil {
		for _, p := range people {
			sub := ""
			if p.Phone != "" { sub = p.Phone }
			if p.Wechat != "" {
				if sub != "" { sub += " · " }
				sub += "微信 " + p.Wechat
			}
			out = append(out, &SearchResult{Type: "person", ID: p.ID, Title: p.Name, Subtitle: sub, Path: "/people/" + p.ID})
		}
	}

	type row struct{ kind, sql string }
	queries := []row{
		{"event", `SELECT id,title,COALESCE(summary,''),event_date FROM live_events WHERE title LIKE ? OR summary LIKE ? OR location LIKE ? ORDER BY event_date DESC LIMIT ?`},
		{"memo", `SELECT id,content,COALESCE(due_date,''),said_at FROM live_memos WHERE content LIKE ? ORDER BY said_at DESC LIMIT ?`},
		{"transaction", `SELECT t.id,COALESCE(t.title,'')||' '||COALESCE(p.name,''),COALESCE(p.name,''),t.occurred_at FROM live_transactions t LEFT JOIN live_people p ON p.id=t.person_id WHERE t.title LIKE ? OR p.name LIKE ? ORDER BY t.occurred_at DESC LIMIT ?`},
		{"anniversary", `SELECT id,title,COALESCE(date,''),date FROM live_anniversaries WHERE title LIKE ? ORDER BY date DESC LIMIT ?`},
	}
	for _, qr := range queries {
		var args []any
		switch qr.kind {
		case "event":
			args = []any{like, like, like, perType}
		case "transaction":
			args = []any{like, like, perType}
		default:
			args = []any{like, perType}
		}
		rows, err := s.DB.QueryContext(ctx, qr.sql, args...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id, title, sub, date string
			if err := rows.Scan(&id, &title, &sub, &date); err != nil {
				continue
			}
			path := "/events"
			if qr.kind == "memo" { path = "/memos" }
			if qr.kind == "transaction" { path = "/money" }
			if qr.kind == "anniversary" { path = "/anniversaries" }
			out = append(out, &SearchResult{Type: qr.kind, ID: id, Title: title, Subtitle: sub, Date: date, Path: path})
		}
		rows.Close()
	}
	return out, nil
}

// ===== Timeline =====

type TimelineItem struct {
	Date       string `json:"date"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	PersonID   string `json:"person_id,omitempty"`
	PersonName string `json:"person_name,omitempty"`
	ID         string `json:"id"`
}

func (s *Store) PersonTimeline(ctx context.Context, personID string) ([]*TimelineItem, error) {
	q := `SELECT event_date AS date, 'event' AS type, title, ? AS person_id,
  (SELECT GROUP_CONCAT(p2.name, ',') FROM live_people p2 JOIN event_participants ep2 ON ep2.person_id=p2.id WHERE ep2.event_id=live_events.id) AS title2,
  id FROM live_events WHERE id IN (SELECT event_id FROM event_participants WHERE person_id=?)
UNION ALL
SELECT said_at,'memo',content,?, (SELECT name FROM people WHERE id=?), id FROM live_memos WHERE person_id=?
UNION ALL
SELECT occurred_at,'transaction',title,?, (SELECT name FROM people WHERE id=?), id FROM live_transactions WHERE person_id=?
UNION ALL
SELECT date,'anniversary',title,?, (SELECT name FROM people WHERE id=?), id FROM live_anniversaries WHERE person_id=?
ORDER BY date DESC`
	rows, err := s.DB.QueryContext(ctx, q, personID, personID, personID, personID, personID, personID, personID, personID, personID, personID, personID)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*TimelineItem{}
	for rows.Next() {
		ti := &TimelineItem{}
		var title2 sql.NullString
		if err := rows.Scan(&ti.Date, &ti.Type, &ti.Title, &ti.PersonID, &title2, &ti.ID); err != nil {
			return nil, err
		}
		if title2.Valid && ti.Type == "event" {
			ti.PersonName = title2.String
		}
		list = append(list, ti)
	}
	return list, rows.Err()
}

func (s *Store) GlobalTimeline(ctx context.Context, limit int) ([]*TimelineItem, error) {
	if limit <= 0 { limit = 100 }
	q := `SELECT event_date AS date, 'event' AS type, title,
  (SELECT GROUP_CONCAT(p2.name, ', ') FROM live_people p2 JOIN event_participants ep2 ON ep2.person_id=p2.id WHERE ep2.event_id=live_events.id) AS participants,
  id FROM live_events
UNION ALL
SELECT said_at,'memo',content, (SELECT name FROM live_people WHERE id=person_id), id FROM live_memos
UNION ALL
SELECT occurred_at,'transaction',title, (SELECT name FROM live_people WHERE id=person_id), id FROM live_transactions
UNION ALL
SELECT date,'anniversary',title, (SELECT name FROM live_people WHERE id=person_id), id FROM live_anniversaries
ORDER BY date DESC LIMIT ?`
	rows, err := s.DB.QueryContext(ctx, q, limit)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*TimelineItem{}
	for rows.Next() {
		ti := &TimelineItem{}
		var persons sql.NullString
		if err := rows.Scan(&ti.Date, &ti.Type, &ti.Title, &persons, &ti.ID); err != nil {
			return nil, err
		}
		if persons.Valid { ti.PersonName = persons.String }
		list = append(list, ti)
	}
	return list, rows.Err()
}

// ===== Dashboard Stats =====

type GradeDist struct {
	Grade int `json:"grade"`
	Count int `json:"count"`
}

// unsettledBalanceSQL 计算某个方向上未结清的净额（分）。
//
// 只统计借还（kind='loan'）：礼物、花销、随礼是一次性了结的支出，不存在「对方待还」，
// 把它们算进来会让首页的「我借出（未还）」凭空涨，建议里出现「某某待还 ¥600」这种
// 会让人去催一个根本没借钱的人的话。
//
// 注意：必须先在子查询里按每笔交易 t.id 扣除对应还款，再在外层求和。
// 原先写成 `SUM(t.amount_fen) - (SELECT … WHERE r.transaction_id=t.id)` 时，
// 聚合上下文中的 t.id 是裸列，SQLite 取哪一行未定义，多笔借款时结果错误。
const unsettledBalanceSQL = `SELECT COALESCE(SUM(remaining),0) FROM (
  SELECT t.amount_fen - COALESCE((SELECT SUM(r.amount_fen) FROM repayments r WHERE r.transaction_id=t.id),0) AS remaining
  FROM live_transactions t WHERE t.direction=? AND t.kind='loan' AND t.settled=0)`

func (s *Store) DashboardStats(ctx context.Context) (map[string]any, error) {
	var totalPeople, totalEvents int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_people WHERE archived=0`).Scan(&totalPeople)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_events`).Scan(&totalEvents)
	var pendingPromises int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_memos WHERE is_promise=1 AND status='open'`).Scan(&pendingPromises)

	// 待办口径与「近期待办」列表保持一致：自定义提醒 + 纪念日衍生，且排除已完成的纪念日提醒。
	upcoming7, dueToday := 0, 0
	if all, err := s.ReminderUpcoming(ctx, 7); err == nil {
		today := todayLocal()
		for _, r := range all {
			upcoming7++
			if len(r.DueAt) >= 10 && r.DueAt[:10] == today {
				dueToday++
			}
		}
	}
	var lendFen, borrowFen sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, unsettledBalanceSQL, "out").Scan(&lendFen)
	_ = s.DB.QueryRowContext(ctx, unsettledBalanceSQL, "in").Scan(&borrowFen)
	return map[string]any{
		"total_people":     totalPeople,
		"total_events":     totalEvents,
		"upcoming_days7":   upcoming7,
		"due_today":        dueToday,
		"lend_fen":         lendFen.Int64,
		"borrow_fen":       borrowFen.Int64,
		"pending_promises": pendingPromises,
	}, nil
}

// StatsByMonth 统计月度活动。
//
// 月份维度取三张表的并集：原先以 events 为主表分组，
// 某月只有备忘或金钱往来时整月会缺失，趋势图出现空洞。
func (s *Store) StatsByMonth(ctx context.Context, months int) ([]map[string]any, error) {
	if months <= 0 { months = 12 }
	rows, err := s.DB.QueryContext(ctx, `WITH months(month) AS (
  SELECT DISTINCT strftime('%Y-%m', event_date) FROM live_events WHERE event_date IS NOT NULL AND event_date <> ''
  UNION
  SELECT DISTINCT strftime('%Y-%m', said_at) FROM live_memos WHERE said_at IS NOT NULL AND said_at <> ''
  UNION
  SELECT DISTINCT strftime('%Y-%m', occurred_at) FROM live_transactions WHERE occurred_at IS NOT NULL AND occurred_at <> ''
)
SELECT m.month,
(SELECT COUNT(*) FROM live_events e WHERE strftime('%Y-%m', e.event_date)=m.month) AS event_count,
(SELECT COUNT(*) FROM live_memos x WHERE strftime('%Y-%m', x.said_at)=m.month) AS memo_count,
(SELECT COUNT(*) FROM live_transactions y WHERE strftime('%Y-%m', y.occurred_at)=m.month) AS tx_count
FROM months m WHERE m.month IS NOT NULL AND m.month <> ''
ORDER BY m.month DESC LIMIT ?`, months)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var month string
		var ec, mc, tc int
		if err := rows.Scan(&month, &ec, &mc, &tc); err != nil { return nil, err }
		list = append(list, map[string]any{
			"month": month, "event_count": ec, "memo_count": mc, "tx_count": tc,
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i]["month"].(string) > list[j]["month"].(string) })
	return list, rows.Err()
}

func (s *Store) GradeDistribution(ctx context.Context) ([]*GradeDist, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT grade, COUNT(*) FROM live_people WHERE archived=0 GROUP BY grade ORDER BY grade`)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*GradeDist{}
	for rows.Next() {
		g := &GradeDist{}
		if err := rows.Scan(&g.Grade, &g.Count); err != nil { return nil, err }
		list = append(list, g)
	}
	return list, rows.Err()
}

// ===== EventGet (single event with participants) =====

func (s *Store) EventGet(ctx context.Context, id string) (*Event, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+eventColumns+`
FROM live_events e LEFT JOIN event_types et ON e.type_id=et.id WHERE e.id=?`, id)
	if err != nil { return nil, err }
	defer rows.Close()
	if !rows.Next() { return nil, sql.ErrNoRows }
	e, err := scanEvent(rows)
	if err != nil { return nil, err }
	rows.Close()
	prows, _ := s.DB.QueryContext(ctx, `SELECT p.id,p.name FROM live_people p JOIN event_participants ep ON ep.person_id=p.id WHERE ep.event_id=? ORDER BY ep.rowid`, e.ID)
	defer prows.Close()
	for prows.Next() {
		var pid, pn string
		if prows.Scan(&pid, &pn) == nil { e.Participants = append(e.Participants, &Person{ID: pid, Name: pn}) }
	}
	if exps, err := s.EventExpenses(ctx, e.ID); err == nil {
		e.Expenses = exps
	}
	return e, nil
}

// ===== Repayments =====

func (s *Store) RepaymentCreate(ctx context.Context, r *Repayment) error {
	if r.ID == "" { r.ID = uuid.NewString() }
	_, err := s.DB.ExecContext(ctx, "INSERT INTO repayments(id,transaction_id,amount_fen,occurred_at,note) VALUES(?,?,?,?,?)",
		r.ID, r.TransactionID, r.AmountFen, r.OccurredAt, r.Note)
	return err
}

func (s *Store) RepaymentDelete(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "DELETE FROM repayments WHERE id=?", id)
}

func (s *Store) RepaymentsOf(ctx context.Context, transactionID string) ([]*Repayment, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,transaction_id,amount_fen,occurred_at,note FROM repayments WHERE transaction_id=? ORDER BY occurred_at DESC", transactionID)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*Repayment{}
	for rows.Next() {
		r := &Repayment{}
		var note sql.NullString
		if err := rows.Scan(&r.ID, &r.TransactionID, &r.AmountFen, &r.OccurredAt, &note); err != nil { return nil, err }
		if note.Valid { r.Note = note.String }
		list = append(list, r)
	}
	return list, rows.Err()
}

// ===== PersonDetail (person + fields) =====

func (s *Store) PersonDetail(ctx context.Context, id string) (*Person, []*PersonField, error) {
	p, err := s.PersonGet(ctx, id)
	if err != nil { return nil, nil, err }
	if p == nil { return nil, nil, nil }
	fields, err := s.PersonFieldList(ctx, id)
	if err != nil { return p, nil, err }
	return p, fields, nil
}

// ===== PersonArchive =====

func (s *Store) PersonArchive(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "UPDATE people SET archived=1 WHERE id=?", id)
}

func (s *Store) PersonUnarchive(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "UPDATE people SET archived=0 WHERE id=?", id)
}

// ===== PersonIntimacy / WordCloud =====

// TrendPoint 是某一天的亲密度快照。
type TrendPoint struct {
	Day   string `json:"day"`
	Score int    `json:"score"`
}

type PersonIntimacy struct {
	CurrentScore   int          `json:"current_score"`
	Grade          int          `json:"grade"`
	RecentEvents   int          `json:"recent_events"`
	LastInteraction string      `json:"last_interaction,omitempty"`
	Tags           []string     `json:"tags,omitempty"`
	// Trend 供详情页折线图使用；快照不足时至少回退为当天一点，避免前端拿到 undefined。
	Trend          []TrendPoint `json:"trend"`
}

func (s *Store) PersonIntimacy(ctx context.Context, personID string) (*PersonIntimacy, error) {
	p, err := s.PersonGet(ctx, personID)
	if err != nil { return nil, err }
	grade := 0
	if p != nil { grade = p.Grade }
	// Recent event count
	var recentCount int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_events e WHERE EXISTS (SELECT 1 FROM event_participants ep WHERE ep.event_id=e.id AND ep.person_id=?) AND e.event_date >= ?`, personID, daysFromTodayLocal(-60)).Scan(&recentCount)
	// Last interaction
	var last sql.NullString
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(event_date) FROM live_events e WHERE EXISTS (SELECT 1 FROM event_participants ep WHERE ep.event_id=e.id AND ep.person_id=?)`, personID).Scan(&last)
	// Score = grade*20 + recentEvents*3 + (archived ? 0 : 10)
	score := grade * 20 + recentCount*3 + 10
	if p != nil && p.Archived { score -= 15 }
	if score > 100 { score = 100 }
	if score < 0 { score = 0 }
	out := &PersonIntimacy{CurrentScore: score, Grade: grade, RecentEvents: recentCount}
	if last.Valid { out.LastInteraction = last.String }

	// 趋势：读取快照表，并把今天的最新分数补到末尾，保证至少有一个点。
	out.Trend = []TrendPoint{}
	if rows, err := s.DB.QueryContext(ctx,
		`SELECT day,score FROM intimacy_snapshots WHERE person_id=? ORDER BY day DESC LIMIT 30`, personID); err == nil {
		type row struct{ day string; score int }
		collected := []row{}
		for rows.Next() {
			var d string
			var sc int
			if err := rows.Scan(&d, &sc); err == nil {
				collected = append(collected, row{d, sc})
			}
		}
		rows.Close()
		for i := len(collected) - 1; i >= 0; i-- {
			out.Trend = append(out.Trend, TrendPoint{Day: collected[i].day, Score: collected[i].score})
		}
	}
	today := todayLocal()
	if len(out.Trend) == 0 || out.Trend[len(out.Trend)-1].Day != today {
		out.Trend = append(out.Trend, TrendPoint{Day: today, Score: score})
	}
	return out, nil
}

func (s *Store) PersonWordCloudText(ctx context.Context, personID string) ([]map[string]any, error) {
	// naive: collect memo content words
	rows, err := s.DB.QueryContext(ctx, "SELECT content FROM live_memos WHERE person_id=?", personID)
	if err != nil { return nil, err }
	defer rows.Close()
	words := make(map[string]int)
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			for _, w := range tokenizeSimple(c) {
				if len(w) >= 2 { words[w]++ }
			}
		}
	}
	list := []map[string]any{}
	for k, v := range words {
		list = append(list, map[string]any{"word": k, "count": v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i]["count"].(int) > list[j]["count"].(int) })
	if len(list) > 50 { list = list[:50] }
	return list, nil
}

func tokenizeSimple(s string) []string {
	// split on non-Chinese/alnum characters, keep tokens >= 2 chars
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		alnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c > 127
		if !alnum {
			tok := s[start:i]
			if len(tok) >= 2 { out = append(out, tok) }
			start = i + 1
		}
	}
	if len(s)-start >= 2 { out = append(out, s[start:]) }
	return out
}

// ===== RelationshipsOf / RelationshipGraph =====

func (s *Store) RelationshipsOf(ctx context.Context, personID string) ([]*Relationship, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id,r.from_person_id,r.to_person_id,r.type,r.remark,r.created_at,
pf.name,pt.name FROM relationships r
LEFT JOIN live_people pf ON pf.id=r.from_person_id LEFT JOIN live_people pt ON pt.id=r.to_person_id
WHERE (r.from_person_id=? OR r.to_person_id=?) AND pf.id IS NOT NULL AND pt.id IS NOT NULL`, personID, personID)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*Relationship{}
	for rows.Next() {
		r := &Relationship{}
		var remark, fn, tn sql.NullString
		if err := rows.Scan(&r.ID, &r.FromPerson, &r.ToPerson, &r.Type, &remark, &r.CreatedAt, &fn, &tn); err != nil {
			return nil, err
		}
		if remark.Valid { r.Remark = remark.String }
		if fn.Valid { r.FromName = fn.String }
		if tn.Valid { r.ToName = tn.String }
		list = append(list, r)
	}
	return list, rows.Err()
}

// RelationshipTypes 列出关系边里已经用过的全部类型，
// 给前端的类型 combobox 做历史候选（预置枚举之外的自定义类型也在里面）。
func (s *Store) RelationshipTypes(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT DISTINCT type FROM relationships WHERE type<>'' ORDER BY type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// graphNodeLimit 是整图一次能取回的节点上限。图谱要画全量关系，
// 只能整页取人；超过这个数就按等级/最近更新截断，并把截断如实报出去。
const graphNodeLimit = 500

// GraphData 关系图的一页数据。截断不是静默的：被丢掉的人与因此悬空的边
// 都计数返回，前端据此提示「图里只有前 N 人」。
type GraphData struct {
	People        []*Person          `json:"people"`
	Relationships []*Relationship    `json:"relationships"`
	Tags          map[string][]*Tag  `json:"tags"`
	TotalPeople   int                `json:"total_people"`
	Truncated     bool               `json:"truncated"`
	DroppedEdges  int                `json:"dropped_edges"`
}

func (s *Store) RelationshipGraph(ctx context.Context) (*GraphData, error) {
	out := &GraphData{People: []*Person{}, Relationships: []*Relationship{}, Tags: map[string][]*Tag{}}
	people, err := s.PersonList(ctx, "", 0, 0, false, 0, graphNodeLimit, 0)
	if err != nil { return nil, err }
	total, err := s.PersonCount(ctx)
	if err != nil { return nil, err }
	rels, err := s.RelationshipList(ctx)
	if err != nil { return nil, err }
	ids := make([]string, len(people))
	kept := make(map[string]bool, len(people))
	for i, p := range people {
		ids[i] = p.ID
		kept[p.ID] = true
	}
	// 端点没进这一页的边直接丢掉，否则图上会出现没有节点的悬空连线
	for _, r := range rels {
		if kept[r.FromPerson] && kept[r.ToPerson] {
			out.Relationships = append(out.Relationships, r)
		} else {
			out.DroppedEdges++
		}
	}
	tags, err := s.TagsFor(ctx, ids)
	if err != nil { return nil, err }
	out.People = people
	out.Tags = tags
	out.TotalPeople = total
	out.Truncated = total > len(people)
	return out, nil
}

// ===== Attachments =====

type Attachment struct {
	ID        string `json:"id"`
	EntityType string `json:"entity_type"`
	EntityID  string `json:"entity_id"`
	FileName  string `json:"file_name"`
	StoredName string `json:"stored_name"`
	Mime      string `json:"mime"`
	Size      int    `json:"size"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) AttachmentCreate(ctx context.Context, a *Attachment) error {
	if a.ID == "" { a.ID = uuid.NewString() }
	a.CreatedAt = nowUTC()
	_, err := s.DB.ExecContext(ctx, "INSERT INTO attachments(id,entity_type,entity_id,file_name,stored_name,mime,size,created_at) VALUES(?,?,?,?,?,?,?,?)",
		a.ID, a.EntityType, a.EntityID, a.FileName, a.StoredName, a.Mime, a.Size, a.CreatedAt)
	return err
}

func (s *Store) AttachmentDelete(ctx context.Context, id string) (*Attachment, error) {
	var a Attachment
	err := s.DB.QueryRowContext(ctx, "SELECT id,entity_type,entity_id,file_name,stored_name,mime,size,created_at FROM attachments WHERE id=?", id).Scan(
		&a.ID, &a.EntityType, &a.EntityID, &a.FileName, &a.StoredName, &a.Mime, &a.Size, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	_, err = s.DB.ExecContext(ctx, "DELETE FROM attachments WHERE id=?", id)
	return &a, err
}

func (s *Store) AttachmentsOf(ctx context.Context, entityType, entityID string) ([]*Attachment, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,entity_type,entity_id,file_name,stored_name,mime,size,created_at FROM attachments WHERE entity_type=? AND entity_id=? ORDER BY created_at DESC", entityType, entityID)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*Attachment{}
	for rows.Next() {
		a := &Attachment{}
		if err := rows.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.FileName, &a.StoredName, &a.Mime, &a.Size, &a.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// ===== PersonIntimacySnapshot (helper) =====

func (s *Store) PersonIntimacySnapshot(ctx context.Context, personID string, day string, score int) error {
	_, err := s.DB.ExecContext(ctx,
		"INSERT OR REPLACE INTO intimacy_snapshots(person_id,day,score) VALUES(?,?,?)",
		personID, day, score)
	return err
}
