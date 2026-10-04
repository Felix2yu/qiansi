package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// ===== 明细导出（N3）=====

// ExportFilter 是明细导出的三个维度。零值表示不加这一层过滤。
type ExportFilter struct {
	Year     int    // 按自然年，取日期前 4 位
	PersonID string
	EventID  string // 一场宴席的礼单单独导出去对账
}

// ExportDetail 返回表头与数据行，供 CSV 下载。
//
// 金额一律换算成「元」的字符串：拿到这份表的人关心的是 800，不是 80000 分。
// 列名用中文，因为导出的目的就是发给别人（配偶、管事）在 Excel 里看。
func (s *Store) ExportDetail(ctx context.Context, what string, f ExportFilter) ([]string, [][]string, error) {
	switch what {
	case "events":
		return s.exportEvents(ctx, f)
	case "transactions":
		return s.exportTransactions(ctx, f)
	case "memos":
		return s.exportMemos(ctx, f)
	default:
		return nil, nil, fmt.Errorf("不支持导出的明细类型: %s", what)
	}
}

// yearPersonCond 拼出「按年 + 按人」两个通用过滤。列名由调用处写死，用户只给值。
// event_id 只有金钱表有，所以不在这里，由 exportTransactions 自己加。
func (f ExportFilter) yearPersonCond(dateCol, personCol string) (string, []any) {
	var conds []string
	var args []any
	if f.Year > 0 {
		conds = append(conds, "substr("+dateCol+",1,4)=?")
		args = append(args, strconv.Itoa(f.Year))
	}
	if f.PersonID != "" {
		conds = append(conds, personCol+"=?")
		args = append(args, f.PersonID)
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

const exportEventCols = `e.event_date,e.title,COALESCE(et.name,''),COALESCE(e.location,''),
COALESCE((SELECT group_concat(p.name,'、') FROM event_participants ep JOIN people p ON p.id=ep.person_id WHERE ep.event_id=e.id),''),
COALESCE((SELECT t.direction FROM transactions t WHERE t.id=e.gift_transaction_id),''),
COALESCE((SELECT t.amount_fen FROM transactions t WHERE t.id=e.gift_transaction_id),0),
(SELECT COALESCE(SUM(t.amount_fen),0) FROM transactions t WHERE t.event_id=e.id AND t.direction='out' AND t.id IS NOT e.gift_transaction_id),
e.has_gift,COALESCE(e.gift,''),COALESCE(e.summary,'')`

func (s *Store) exportEvents(ctx context.Context, f ExportFilter) ([]string, [][]string, error) {
	// 事件的「按人」是参与人，不是某个外键列，所以这里不用 ExportFilter.where
	var conds []string
	var args []any
	if f.Year > 0 {
		conds = append(conds, "substr(e.event_date,1,4)=?")
		args = append(args, strconv.Itoa(f.Year))
	}
	if f.PersonID != "" {
		conds = append(conds, "e.id IN (SELECT ep.event_id FROM event_participants ep WHERE ep.person_id=?)")
		args = append(args, f.PersonID)
	}
	if f.EventID != "" {
		conds = append(conds, "e.id=?")
		args = append(args, f.EventID)
	}
	qry := "SELECT " + exportEventCols + " FROM events e LEFT JOIN event_types et ON et.id=e.type_id"
	if len(conds) > 0 {
		qry += " WHERE " + strings.Join(conds, " AND ")
	}
	qry += " ORDER BY e.event_date DESC, e.created_at DESC"
	rows, err := s.DB.QueryContext(ctx, qry, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	head := []string{"日期", "标题", "类型", "地点", "参与人", "礼金方向", "礼金（元）", "开销（元）", "有礼物", "礼物说明", "备注"}
	out := [][]string{}
	for rows.Next() {
		var date, title, typeName, loc, peopleStr, giftDir, giftName, summary string
		var giftFen, expenseFen int
		var hasGift bool
		if err := rows.Scan(&date, &title, &typeName, &loc, &peopleStr, &giftDir, &giftFen, &expenseFen, &hasGift, &giftName, &summary); err != nil {
			return nil, nil, err
		}
		out = append(out, []string{
			date, title, typeName, loc, peopleStr, dirLabel(giftDir), yuanCell(giftFen), yuanCell(expenseFen),
			yesNo(hasGift), giftName, summary,
		})
	}
	return head, out, rows.Err()
}

func (s *Store) exportTransactions(ctx context.Context, f ExportFilter) ([]string, [][]string, error) {
	qry := `SELECT t.occurred_at,COALESCE(p.name,''),t.kind,t.direction,t.amount_fen,
COALESCE(t.title,''),COALESCE(t.due_date,''),t.settled,COALESCE(t.settled_at,''),
COALESCE((SELECT SUM(r.amount_fen) FROM repayments r WHERE r.transaction_id=t.id),0),
COALESCE((SELECT ev.title FROM events ev WHERE ev.id=t.event_id),'')
FROM transactions t LEFT JOIN people p ON p.id=t.person_id`
	cond, args := f.yearPersonCond("t.occurred_at", "t.person_id")
	if f.EventID != "" {
		if cond == "" {
			cond = " WHERE "
		} else {
			cond += " AND "
		}
		cond += "t.event_id=?"
		args = append(args, f.EventID)
	}
	rows, err := s.DB.QueryContext(ctx, qry+cond+" ORDER BY t.occurred_at DESC, t.created_at DESC", args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	head := []string{"日期", "人物", "类型", "方向", "金额（元）", "标题", "到期日", "已结清", "结清日", "已还（元）", "关联事件"}
	out := [][]string{}
	for rows.Next() {
		var day, person, kind, dir, title, due, settledAt, eventTitle string
		var amount, repaid int
		var settled bool
		if err := rows.Scan(&day, &person, &kind, &dir, &amount, &title, &due, &settled, &settledAt, &repaid, &eventTitle); err != nil {
			return nil, nil, err
		}
		out = append(out, []string{
			day, person, txKindLabel(kind), dirLabel(dir), yuanCell(amount), title, due,
			yesNo(settled), settledAt, yuanCell(repaid), eventTitle,
		})
	}
	return head, out, rows.Err()
}

func (s *Store) exportMemos(ctx context.Context, f ExportFilter) ([]string, [][]string, error) {
	qry := `SELECT m.said_at,COALESCE(p.name,''),m.speaker,m.content,m.is_promise,COALESCE(m.due_date,''),m.status
FROM memos m LEFT JOIN people p ON p.id=m.person_id`
	// 对话不挂事件，EventID 这一层对 memos 无意义
	cond, args := f.yearPersonCond("m.said_at", "m.person_id")
	rows, err := s.DB.QueryContext(ctx, qry+cond+" ORDER BY m.said_at DESC", args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	head := []string{"时间", "人物", "说话人", "内容", "是承诺", "到期日", "状态"}
	out := [][]string{}
	for rows.Next() {
		var saidAt, person, speaker, content, due, status string
		var promise bool
		if err := rows.Scan(&saidAt, &person, &speaker, &content, &promise, &due, &status); err != nil {
			return nil, nil, err
		}
		out = append(out, []string{saidAt, person, speakerLabel(speaker), content, yesNo(promise), due, memoStatusLabel(status)})
	}
	return head, out, rows.Err()
}

func yuanCell(fen int) string {
	if fen == 0 {
		return ""
	}
	return strconv.FormatFloat(float64(fen)/100, 'f', 2, 64)
}

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return ""
}

func dirLabel(dir string) string {
	switch dir {
	case "out":
		return "支出"
	case "in":
		return "收入"
	}
	return ""
}

func txKindLabel(kind string) string {
	switch kind {
	case "loan":
		return "借还"
	case "gift":
		return "礼物"
	case "expense":
		return "花销"
	case "other":
		return "其它"
	}
	return kind
}

func memoStatusLabel(status string) string {
	switch status {
	case "open":
		return "进行中"
	case "fulfilled":
		return "已兑现"
	case "broken":
		return "未兑现"
	}
	return status
}

func speakerLabel(speaker string) string {
	switch speaker {
	case "me":
		return "我说"
	case "other":
		return "对方说"
	}
	return speaker
}
