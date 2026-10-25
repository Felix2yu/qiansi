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
	rows, err := s.CurrentDB().QueryContext(ctx, `SELECT `+eventBillColumns+`
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
	// type 空串按支出计：开销合计只数支出账单（见 eventColumns），
	// 而「没带 type」和「是支出」在调用方（测试、老数据）里分不清，
	// 归一化成支出比静默漏进合计更安全
	if b.Type == "" {
		b.Type = "expense"
	}
	now := nowUTC()
	_, err := s.CurrentDB().ExecContext(ctx, `INSERT INTO event_bills
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
	rows, err := s.CurrentDB().QueryContext(ctx, `SELECT `+eventBillColumns+`
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
	res, err := s.CurrentDB().ExecContext(ctx, `DELETE FROM event_bills WHERE id=? AND event_id=?`, id, eventID)
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
	// type 归一化同 EventBillLink：远端若对已关联账单返回空 type，
	// 直接落库会让它退出「支出账单」合计，账单权威口径悄悄翻回手填
	if b.Type == "" {
		b.Type = "expense"
	}
	res, err := s.CurrentDB().ExecContext(ctx, `UPDATE event_bills SET
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
	// 只数支出账单：入账/转账账单不构成「这次往来的开销」，
	// 拿它们当权威会把手填开销的编辑权也一起禁掉
	err := s.CurrentDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM event_bills WHERE event_id=? AND type='expense'`, eventID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// EventBillRemoteIDs 取出某往来已关联的远端 id，供刷新时批量重取。
func (s *Store) EventBillRemoteIDs(ctx context.Context, eventID string) ([]string, error) {
	rows, err := s.CurrentDB().QueryContext(ctx, `SELECT remote_id FROM event_bills WHERE event_id=? ORDER BY tx_date DESC`, eventID)
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

// EventBillsCounts 已经删除：列表页徽标改由 eventColumns 里的 COUNT 子查询
// 一次算出（见 events.go），这个批量接口没有任何调用方，只剩测试在引用。
