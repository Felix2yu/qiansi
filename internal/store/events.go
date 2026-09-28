package store

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	qiansiLunar "github.com/qiansi/app/internal/lunar"
)

// ===== Events =====

type Event struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	TypeID       *int      `json:"type_id,omitempty"`
	TypeName     *string   `json:"type_name,omitempty"`
	TypeColor    *string   `json:"type_color,omitempty"`
	EventDate    string    `json:"event_date"`
	Location     string    `json:"location,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	CreatedAt    string    `json:"created_at"`
	UpdatedAt    string    `json:"updated_at"`
	Participants []*Person `json:"participants,omitempty"`
}

func (s *Store) EventCreate(ctx context.Context, e *Event, participantIDs []string) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	e.CreatedAt = nowUTC()
	e.UpdatedAt = e.CreatedAt
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO events(id,title,type_id,event_date,location,summary,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)",
		e.ID, e.Title, e.TypeID, e.EventDate, e.Location, e.Summary, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return err
	}
	for _, pid := range participantIDs {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO event_participants(event_id,person_id) VALUES(?,?)", e.ID, pid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) EventUpdate(ctx context.Context, e *Event, participantIDs []string) error {
	e.UpdatedAt = nowUTC()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "UPDATE events SET title=?,type_id=?,event_date=?,location=?,summary=?,updated_at=? WHERE id=?",
		e.Title, e.TypeID, e.EventDate, e.Location, e.Summary, e.UpdatedAt, e.ID)
	if err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, "DELETE FROM event_participants WHERE event_id=?", e.ID)
	for _, pid := range participantIDs {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO event_participants(event_id,person_id) VALUES(?,?)", e.ID, pid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) EventDelete(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM events WHERE id=?", id)
	return err
}

func (s *Store) EventList(ctx context.Context, q string, limit, offset int) ([]*Event, error) {
	if limit <= 0 {
		limit = 100
	}
	qry := `SELECT e.id,e.title,e.type_id,e.event_date,e.location,e.summary,e.created_at,e.updated_at,et.name,et.color
FROM events e LEFT JOIN event_types et ON e.type_id=et.id`
	var args []any
	if q != "" {
		qry += " WHERE e.title LIKE ? OR e.summary LIKE ? OR e.location LIKE ?"
		q = "%" + q + "%"
		args = append(args, q, q, q)
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
		e := &Event{}
		var tn, tc sql.NullString
		if err := rows.Scan(&e.ID, &e.Title, &e.TypeID, &e.EventDate, &e.Location, &e.Summary, &e.CreatedAt, &e.UpdatedAt, &tn, &tc); err != nil {
			return nil, err
		}
		if tn.Valid { e.TypeName = &tn.String }
		if tc.Valid { e.TypeColor = &tc.String }
		list = append(list, e)
	}
	// Hydrate participants for each event
	for _, e := range list {
		prows, err := s.DB.QueryContext(ctx,
			`SELECT p.id,p.name FROM people p JOIN event_participants ep ON ep.person_id=p.id WHERE ep.event_id=? ORDER BY ep.rowid`, e.ID)
		if err != nil {
			continue
		}
		for prows.Next() {
			var id, name string
			if err := prows.Scan(&id, &name); err == nil {
				e.Participants = append(e.Participants, &Person{ID: id, Name: name})
			}
		}
		prows.Close()
	}
	return list, rows.Err()
}

// ===== Memos =====

type Memo struct {
	ID        string `json:"id"`
	PersonID  string `json:"person_id,omitempty"`
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
	_, err := s.DB.ExecContext(ctx, "UPDATE memos SET person_id=?,speaker=?,content=?,said_at=?,is_promise=?,due_date=?,status=? WHERE id=?",
		personID, m.Speaker, m.Content, m.SaidAt, m.IsPromise, dueDate, m.Status, m.ID)
	return err
}

func (s *Store) MemoDelete(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM memos WHERE id=?", id)
	return err
}

func (s *Store) MemoList(ctx context.Context, personID string, promisesOnly bool, limit, offset int) ([]*Memo, error) {
	if limit <= 0 { limit = 50 }
	q := `SELECT m.id,m.person_id,m.speaker,m.content,m.said_at,m.is_promise,m.due_date,m.status,m.created_at FROM memos m WHERE 1=1`
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
		var personIDStr, due sql.NullString
		if err := rows.Scan(&m.ID, &personIDStr, &m.Speaker, &m.Content, &m.SaidAt, &m.IsPromise, &due, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		if personIDStr.Valid { m.PersonID = personIDStr.String }
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

func (s *Store) RelationshipCreate(ctx context.Context, r *Relationship) error {
	if r.ID == "" { r.ID = uuid.NewString() }
	r.CreatedAt = nowUTC()
	var remark any
	if r.Remark != "" { remark = r.Remark } else { remark = nil }
	_, err := s.DB.ExecContext(ctx, "INSERT OR REPLACE INTO relationships(id,from_person_id,to_person_id,type,remark,created_at) VALUES(?,?,?,?,?,?)",
		r.ID, r.FromPerson, r.ToPerson, r.Type, remark, r.CreatedAt)
	return err
}

func (s *Store) RelationshipDelete(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM relationships WHERE id=?", id)
	return err
}

func (s *Store) RelationshipList(ctx context.Context) ([]*Relationship, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id,r.from_person_id,r.to_person_id,r.type,r.remark,r.created_at,
pf.name,pt.name
FROM relationships r
LEFT JOIN people pf ON pf.id=r.from_person_id
LEFT JOIN people pt ON pt.id=r.to_person_id`)
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
	_, err := s.DB.ExecContext(ctx, "INSERT INTO transactions(id,person_id,kind,direction,amount_fen,title,occurred_at,due_date,settled,settled_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
		t.ID, t.PersonID, t.Kind, t.Direction, t.AmountFen, title, t.OccurredAt, dueDate, t.Settled, settledAt, t.CreatedAt)
	return err
}

func (s *Store) TransactionUpdate(ctx context.Context, t *Transaction) error {
	var dueDate any
	if t.DueDate != "" { dueDate = t.DueDate } else { dueDate = nil }
	var settledAt any
	if t.SettledAt != "" { settledAt = t.SettledAt } else { settledAt = nil }
	var title any
	if t.Title != "" { title = t.Title } else { title = nil }
	_, err := s.DB.ExecContext(ctx, "UPDATE transactions SET person_id=?,kind=?,direction=?,amount_fen=?,title=?,occurred_at=?,due_date=?,settled=?,settled_at=? WHERE id=?",
		t.PersonID, t.Kind, t.Direction, t.AmountFen, title, t.OccurredAt, dueDate, t.Settled, settledAt, t.ID)
	return err
}

func (s *Store) TransactionDelete(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM transactions WHERE id=?", id)
	return err
}

func (s *Store) TransactionGet(ctx context.Context, id string) (*Transaction, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id,t.person_id,t.kind,t.direction,t.amount_fen,t.title,t.occurred_at,t.due_date,t.settled,t.settled_at,t.created_at,p.name,
COALESCE((SELECT COALESCE(SUM(r.amount_fen),0) FROM repayments r WHERE r.transaction_id=t.id),0)
FROM transactions t LEFT JOIN people p ON p.id=t.person_id WHERE t.id=?`, id)
	if err != nil { return nil, err }
	defer rows.Close()
	if !rows.Next() { return nil, nil }
	t := &Transaction{}
	var title, due, settledAt, personName sql.NullString
	if err := rows.Scan(&t.ID, &t.PersonID, &t.Kind, &t.Direction, &t.AmountFen, &title, &t.OccurredAt, &due, &t.Settled, &settledAt, &t.CreatedAt, &personName, &t.RepaidFen); err != nil {
		return nil, err
	}
	if title.Valid { t.Title = title.String }
	if due.Valid { t.DueDate = due.String }
	if settledAt.Valid { t.SettledAt = settledAt.String }
	if personName.Valid { t.PersonName = personName.String }
	return t, nil
}

func (s *Store) TransactionList(ctx context.Context, personID string, limit, offset int) ([]*Transaction, error) {
	if limit <= 0 { limit = 200 }
	q := `SELECT t.id,t.person_id,t.kind,t.direction,t.amount_fen,t.title,t.occurred_at,t.due_date,t.settled,t.settled_at,t.created_at,p.name,
COALESCE((SELECT COALESCE(SUM(r.amount_fen),0) FROM repayments r WHERE r.transaction_id=t.id),0)
FROM transactions t LEFT JOIN people p ON p.id=t.person_id WHERE 1=1`
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
		t := &Transaction{}
		var title, due, settledAt, personName sql.NullString
		if err := rows.Scan(&t.ID, &t.PersonID, &t.Kind, &t.Direction, &t.AmountFen, &title, &t.OccurredAt, &due, &t.Settled, &settledAt, &t.CreatedAt, &personName, &t.RepaidFen); err != nil {
			return nil, err
		}
		if title.Valid { t.Title = title.String }
		if due.Valid { t.DueDate = due.String }
		if settledAt.Valid { t.SettledAt = settledAt.String }
		if personName.Valid { t.PersonName = personName.String }
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
	_, err := s.DB.ExecContext(ctx, "UPDATE anniversaries SET person_id=?,title=?,date=?,is_lunar=?,repeat_yearly=?,remind_days=? WHERE id=?",
		personID, a.Title, a.Date, a.IsLunar, a.RepeatYearly, a.RemindDays, a.ID)
	return err
}

func (s *Store) AnniversaryDelete(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM anniversaries WHERE id=?", id)
	return err
}

func (s *Store) AnniversaryList(ctx context.Context) ([]*Anniversary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id,a.person_id,a.title,a.date,a.is_lunar,a.repeat_yearly,a.remind_days,a.created_at,p.name
FROM anniversaries a LEFT JOIN people p ON p.id=a.person_id ORDER BY a.date`)
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
	return list, rows.Err()
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
	_, err := s.DB.ExecContext(ctx, "UPDATE reminders SET person_id=?,ref_type=?,ref_id=?,title=?,due_at=?,status=?,completed_at=? WHERE id=?",
		personID, r.RefType, refID, r.Title, r.DueAt, r.Status, completedAt, r.ID)
	return err
}

func (s *Store) ReminderDelete(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM reminders WHERE id=?", id)
	return err
}

func (s *Store) ReminderDone(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE reminders SET status='done',completed_at=? WHERE id=?", nowUTC(), id)
	return err
}

func (s *Store) ReminderList(ctx context.Context, status string, limit, offset int) ([]*Reminder, error) {
	if limit <= 0 { limit = 50 }
	q := `SELECT r.id,r.person_id,r.ref_type,r.ref_id,r.title,r.due_at,r.status,r.created_at,r.completed_at,p.name
FROM reminders r LEFT JOIN people p ON r.person_id=p.id WHERE 1=1`
	var args []any
	if status != "" {
		q += " AND r.status=?"; args = append(args, status)
	}
	q += " ORDER BY r.due_at ASC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
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

// ReminderUpcoming returns explicit reminders plus anniversary-derived entries
// whose next solar occurrence falls within `horizonDays`.
func (s *Store) ReminderUpcoming(ctx context.Context, horizonDays int) ([]*Reminder, error) {
	now := time.Now().UTC()
	dueCutoff := now.AddDate(0, 0, horizonDays).UTC().Format(timeFormat)
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id,r.person_id,r.ref_type,r.ref_id,r.title,r.due_at,r.status,r.created_at,r.completed_at,p.name
FROM reminders r LEFT JOIN people p ON r.person_id=p.id
WHERE r.status='pending' AND r.due_at<=?
ORDER BY r.due_at ASC`, dueCutoff)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*Reminder{}
	for rows.Next() {
		r := &Reminder{}
		var pid, refid, completed, pn sql.NullString
		if err := rows.Scan(&r.ID, &pid, &r.RefType, &refid, &r.Title, &r.DueAt, &r.Status, &r.CreatedAt, &completed, &pn); err != nil {
			return nil, err
		}
		if pid.Valid { r.PersonID = pid.String }
		if refid.Valid { r.RefID = refid.String }
		if completed.Valid { r.CompletedAt = completed.String }
		if pn.Valid { r.PersonName = pn.String }
		list = append(list, r)
	}
	// Merge anniversary-derived reminders (lunar→solar)
	annivs, err := s.AnniversaryUpcoming(ctx, horizonDays)
	if err == nil {
		list = append(list, annivs...)
		sort.Slice(list, func(i, j int) bool { return list[i].DueAt < list[j].DueAt })
	}
	return list, nil
}

// AnniversaryUpcoming returns Reminder-shaped entries for anniversaries whose
// next occurrence (lunar→solar if is_lunar=true) falls within `horizonDays`.
func (s *Store) AnniversaryUpcoming(ctx context.Context, horizonDays int) ([]*Reminder, error) {
	today := qiansiLunar.NowLocal()
	cutoff := today.AddDays(horizonDays)
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id,a.person_id,a.title,a.date,a.is_lunar,a.remind_days,p.name
FROM anniversaries a LEFT JOIN people p ON p.id=a.person_id`)
	if err != nil { return nil, err }
	defer rows.Close()
	list := []*Reminder{}
	for rows.Next() {
		var anID, personID, title, date, remindDays, personName sql.NullString
		var isLunar bool
		if err := rows.Scan(&anID, &personID, &title, &date, &isLunar, &remindDays, &personName); err != nil {
			return nil, err
		}
		if !date.Valid || !title.Valid || title.String == "" { continue }
		anchor := qiansiLunar.ParseDate(date.String)
		next := qiansiLunar.NextOccurrence(anchor, isLunar, today)
		offsets := qiansiLunar.RemindDates(remindDays.String)
		if len(offsets) == 0 { offsets = []int{0} }
		for _, offset := range offsets {
			rd := next.AddDays(-offset)
			if rd.Before(today) { continue }
			if horizonDays > 0 && cutoff.Before(rd) { continue }
			personIDStr := ""
			if personID.Valid { personIDStr = personID.String }
			pn := ""
			if personName.Valid { pn = personName.String }
			typeTag := ""
			if isLunar { typeTag = " 农历" }
			whenTag := ""
			if offset > 0 { whenTag = "，还有 " + strconv.Itoa(offset) + " 天" } else { whenTag = "，今天" }
			r := &Reminder{
				ID:         "anniv:" + anID.String + ":" + next.String() + ":" + strconv.Itoa(offset),
				PersonID:   personIDStr,
				RefType:    "anniversary",
				RefID:      anID.String,
				Title:      title.String + "（" + next.String() + typeTag + whenTag + "）",
				DueAt:      rd.String() + "T09:00:00Z",
				Status:     "pending",
				PersonName: pn,
			}
			list = append(list, r)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].DueAt < list[j].DueAt })
	return list, rows.Err()
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
  (SELECT GROUP_CONCAT(p2.name, ',') FROM people p2 JOIN event_participants ep2 ON ep2.person_id=p2.id WHERE ep2.event_id=events.id) AS title2,
  id FROM events WHERE id IN (SELECT event_id FROM event_participants WHERE person_id=?)
UNION ALL
SELECT said_at,'memo',content,?, (SELECT name FROM people WHERE id=?), id FROM memos WHERE person_id=?
UNION ALL
SELECT occurred_at,'transaction',title,?, (SELECT name FROM people WHERE id=?), id FROM transactions WHERE person_id=?
UNION ALL
SELECT date,'anniversary',title,?, (SELECT name FROM people WHERE id=?), id FROM anniversaries WHERE person_id=?
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
  (SELECT GROUP_CONCAT(p2.name, ', ') FROM people p2 JOIN event_participants ep2 ON ep2.person_id=p2.id WHERE ep2.event_id=events.id) AS participants,
  id FROM events
UNION ALL
SELECT said_at,'memo',content, (SELECT name FROM people WHERE id=person_id), id FROM memos
UNION ALL
SELECT occurred_at,'transaction',title, (SELECT name FROM people WHERE id=person_id), id FROM transactions
UNION ALL
SELECT date,'anniversary',title, (SELECT name FROM people WHERE id=person_id), id FROM anniversaries
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

func (s *Store) DashboardStats(ctx context.Context) (map[string]any, error) {
	var totalPeople, totalEvents int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM people WHERE archived=0`).Scan(&totalPeople)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&totalEvents)
	var upcoming7, dueToday, pendingPromises int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reminders WHERE status='pending' AND date(due_at) BETWEEN date('now') AND date('now','+7 day')`).Scan(&upcoming7)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reminders WHERE status='pending' AND date(due_at)=date('now')`).Scan(&dueToday)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM memos WHERE is_promise=1 AND status='open'`).Scan(&pendingPromises)
	var lendFen, borrowFen sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_fen)-COALESCE((SELECT SUM(r.amount_fen) FROM repayments r WHERE r.transaction_id=t.id),0),0) FROM transactions t WHERE t.direction='out' AND t.settled=0`).Scan(&lendFen)
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_fen)-COALESCE((SELECT SUM(r.amount_fen) FROM repayments r WHERE r.transaction_id=t.id),0),0) FROM transactions t WHERE t.direction='in' AND t.settled=0`).Scan(&borrowFen)
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

func (s *Store) StatsByMonth(ctx context.Context, months int) ([]map[string]any, error) {
	if months <= 0 { months = 12 }
	rows, err := s.DB.QueryContext(ctx, `SELECT strftime('%Y-%m', event_date) AS month,
COUNT(*) AS event_count,
(SELECT COUNT(*) FROM memos WHERE strftime('%Y-%m', said_at)=strftime('%Y-%m', e.event_date)) AS memo_count,
(SELECT COUNT(*) FROM transactions WHERE strftime('%Y-%m', occurred_at)=strftime('%Y-%m', e.event_date)) AS tx_count
FROM events e
GROUP BY month ORDER BY month DESC LIMIT ?`, months)
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
	rows, err := s.DB.QueryContext(ctx, `SELECT grade, COUNT(*) FROM people WHERE archived=0 GROUP BY grade ORDER BY grade`)
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
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id,e.title,e.type_id,e.event_date,e.location,e.summary,e.created_at,e.updated_at,et.name,et.color
FROM events e LEFT JOIN event_types et ON e.type_id=et.id WHERE e.id=?`, id)
	if err != nil { return nil, err }
	defer rows.Close()
	if !rows.Next() { return nil, nil }
	e := &Event{}
	var tn, tc sql.NullString
	if err := rows.Scan(&e.ID, &e.Title, &e.TypeID, &e.EventDate, &e.Location, &e.Summary, &e.CreatedAt, &e.UpdatedAt, &tn, &tc); err != nil {
		return nil, err
	}
	if tn.Valid { e.TypeName = &tn.String }
	if tc.Valid { e.TypeColor = &tc.String }
	prows, _ := s.DB.QueryContext(ctx, `SELECT p.id,p.name FROM people p JOIN event_participants ep ON ep.person_id=p.id WHERE ep.event_id=? ORDER BY ep.rowid`, e.ID)
	defer prows.Close()
	for prows.Next() {
		var pid, pn string
		if prows.Scan(&pid, &pn) == nil { e.Participants = append(e.Participants, &Person{ID: pid, Name: pn}) }
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
	_, err := s.DB.ExecContext(ctx, "DELETE FROM repayments WHERE id=?", id)
	return err
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
	_, err := s.DB.ExecContext(ctx, "UPDATE people SET archived=1 WHERE id=?", id)
	return err
}

func (s *Store) PersonUnarchive(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE people SET archived=0 WHERE id=?", id)
	return err
}

// ===== PersonIntimacy / WordCloud =====

type PersonIntimacy struct {
	CurrentScore   int      `json:"current_score"`
	Grade          int      `json:"grade"`
	RecentEvents   int      `json:"recent_events"`
	LastInteraction string  `json:"last_interaction,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

func (s *Store) PersonIntimacy(ctx context.Context, personID string) (*PersonIntimacy, error) {
	p, err := s.PersonGet(ctx, personID)
	if err != nil { return nil, err }
	grade := 3
	if p != nil { grade = p.Grade }
	// Recent event count
	var recentCount int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM events e WHERE EXISTS (SELECT 1 FROM event_participants ep WHERE ep.event_id=e.id AND ep.person_id=?) AND e.event_date >= date('now','-60 day')`, personID).Scan(&recentCount)
	// Last interaction
	var last sql.NullString
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(event_date) FROM events e WHERE EXISTS (SELECT 1 FROM event_participants ep WHERE ep.event_id=e.id AND ep.person_id=?)`, personID).Scan(&last)
	// Score = grade*20 + recentEvents*3 + (archived ? 0 : 10)
	score := grade * 20 + recentCount*3 + 10
	if p != nil && p.Archived { score -= 15 }
	if score > 100 { score = 100 }
	if score < 0 { score = 0 }
	out := &PersonIntimacy{CurrentScore: score, Grade: grade, RecentEvents: recentCount}
	if last.Valid { out.LastInteraction = last.String }
	return out, nil
}

func (s *Store) PersonWordCloudText(ctx context.Context, personID string) ([]map[string]any, error) {
	// naive: collect memo content words
	rows, err := s.DB.QueryContext(ctx, "SELECT content FROM memos WHERE person_id=?", personID)
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
LEFT JOIN people pf ON pf.id=r.from_person_id LEFT JOIN people pt ON pt.id=r.to_person_id
WHERE r.from_person_id=? OR r.to_person_id=?`, personID, personID)
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

func (s *Store) RelationshipGraph(ctx context.Context) ([]*Person, []*Relationship, error) {
	people, err := s.PersonList(ctx, "", 0, 0, false, 0, 500, 0)
	if err != nil { return nil, nil, err }
	rels, err := s.RelationshipList(ctx)
	if err != nil { return nil, nil, err }
	return people, rels, nil
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
