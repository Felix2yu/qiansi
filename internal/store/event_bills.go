package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// ===== 往来关联的货殖账单 =====
//
// 账单本体在货殖，这里只存「关联 + 快照」。快照是缓存：货殖不可达时界面还能显示
// 「这次花了多少、花在哪儿」；刷新时按 remote_id 批量重取覆盖，不做增量合并。

// EventBill 一条关联账单。
type EventBill struct {
	ID           string `json:"id"`
	EventID      string `json:"event_id"`
	RemoteID     string `json:"remote_id"`
	RemoteBase   string `json:"remote_base,omitempty"`
	AmountFen    int    `json:"amount_fen"`
	Type         string `json:"type,omitempty"`
	TxDate       string `json:"tx_date,omitempty"`
	Description  string `json:"description,omitempty"`
	CategoryName string `json:"category_name,omitempty"`
	AccountName  string `json:"account_name,omitempty"`
	BookName     string `json:"book_name,omitempty"`
	CreatedAt    string `json:"created_at"`
	SyncedAt     string `json:"synced_at"`
}

const eventBillColumns = `id,event_id,remote_id,remote_base,amount_fen,type,tx_date,description,category_name,account_name,book_name,created_at,synced_at`

func scanEventBill(rows *sql.Rows) (*EventBill, error) {
	b := &EventBill{}
	if err := rows.Scan(&b.ID, &b.EventID, &b.RemoteID, &b.RemoteBase, &b.AmountFen, &b.Type,
		&b.TxDate, &b.Description, &b.CategoryName, &b.AccountName, &b.BookName,
		&b.CreatedAt, &b.SyncedAt); err != nil {
		return nil, err
	}
	return b, nil
}

// EventBills 返回一个往来关联的全部账单，按账单日期倒序。
func (s *Store) EventBills(ctx context.Context, eventID string) ([]*EventBill, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+eventBillColumns+`
FROM event_bills WHERE event_id=? ORDER BY tx_date DESC, created_at DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*EventBill{}
	for rows.Next() {
		b, err := scanEventBill(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, b)
	}
	return list, rows.Err()
}

// EventBillLink 关联一条账单（幂等）。
//
// 同一往来重复关联同一张账单时走 UPDATE：保留原 id 与 created_at，只刷新快照。
// 用 (event_id, remote_id) 唯一约束兜底，前端重复点击不会造出两行。
func (s *Store) EventBillLink(ctx context.Context, b *EventBill) (*EventBill, error) {
	if b.EventID == "" || b.RemoteID == "" {
		return nil, sql.ErrNoRows
	}
	now := nowUTC()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO event_bills
(id,event_id,remote_id,remote_base,amount_fen,type,tx_date,description,category_name,account_name,book_name,created_at,synced_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(event_id,remote_id) DO UPDATE SET
  remote_base=excluded.remote_base, amount_fen=excluded.amount_fen, type=excluded.type,
  tx_date=excluded.tx_date, description=excluded.description, category_name=excluded.category_name,
  account_name=excluded.account_name, book_name=excluded.book_name, synced_at=excluded.synced_at`,
		uuid.NewString(), b.EventID, b.RemoteID, b.RemoteBase, b.AmountFen, b.Type,
		b.TxDate, b.Description, b.CategoryName, b.AccountName, b.BookName, now, now)
	if err != nil {
		return nil, err
	}
	return s.eventBillByRemote(ctx, b.EventID, b.RemoteID)
}

func (s *Store) eventBillByRemote(ctx context.Context, eventID, remoteID string) (*EventBill, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+eventBillColumns+`
FROM event_bills WHERE event_id=? AND remote_id=?`, eventID, remoteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	return scanEventBill(rows)
}

// EventBillUnlink 解除关联。只删关联，不动货殖那边的账单——
// 删账是账本里的事，牵丝没有资格替用户决定。
func (s *Store) EventBillUnlink(ctx context.Context, eventID, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM event_bills WHERE id=? AND event_id=?`, id, eventID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// EventBillRefresh 用最新快照覆盖一行，保留 id 与 created_at。
func (s *Store) EventBillRefresh(ctx context.Context, eventID, id string, b *EventBill) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE event_bills SET
remote_base=?, amount_fen=?, type=?, tx_date=?, description=?, category_name=?, account_name=?, book_name=?, synced_at=?
WHERE id=? AND event_id=?`,
		b.RemoteBase, b.AmountFen, b.Type, b.TxDate, b.Description, b.CategoryName,
		b.AccountName, b.BookName, nowUTC(), id, eventID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// EventHasBills 是否有任何关联账单。写入路径用它决定「手填开销还作不作数」。
func (s *Store) EventHasBills(ctx context.Context, eventID string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_bills WHERE event_id=?`, eventID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// EventBillRemoteIDs 取出某往来已关联的远端 id，供刷新时批量重取。
func (s *Store) EventBillRemoteIDs(ctx context.Context, eventID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT remote_id FROM event_bills WHERE event_id=? ORDER BY tx_date DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// EventBillsCounts 批量取多个往来的账单数（列表页徽标用），避免每行一次查询。
func (s *Store) EventBillsCounts(ctx context.Context, eventIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(eventIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(eventIDs))
	for _, id := range eventIDs {
		args = append(args, id)
	}
	q := `SELECT event_id, COUNT(*) FROM event_bills WHERE event_id IN (?` + placeholders(len(eventIDs)-1) + `) GROUP BY event_id`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// placeholderSuffix 生成 ",?,?,?" 这样的占位符尾巴。
func placeholders(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += ",?"
	}
	return s
}
