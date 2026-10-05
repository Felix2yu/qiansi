package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
)

// ===== 回收站 =====
//
// 六类主记录的删除都是软删除（deleted_at 置位），这里负责把它们重新摆回眼前。
// 读路径的「看不见」由迁移 013 里的 live_* 视图统一裁决，所以恢复只需清掉标记：
// 随联系人消失的那些往来/对话/账目会自己回来，不必在这里逐个补写。

// TrashItem 是回收站里的一行。Type 用 API 上的类型名，恢复与彻底删除按它路由。
type TrashItem struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Detail     string `json:"detail,omitempty"`
	PersonName string `json:"person_name,omitempty"`
	DeletedAt  string `json:"deleted_at"`
	// Hidden 仅联系人有：恢复这个人会连带回到列表的记录条数。
	Hidden int `json:"hidden,omitempty"`
	// PersonTrashed：这条即使恢复也还看不见，它归属的联系人还在回收站里。
	PersonTrashed bool `json:"person_trashed"`
}

// trashTables 是软删除覆盖的全部表，键是 API 上的类型名。
// SQL 里的表名只能从这张写死的表取，未知类型一律拒绝，用户输入永远不进语句。
var trashTables = map[string]string{
	"person":      "people",
	"event":       "events",
	"memo":        "memos",
	"transaction": "transactions",
	"anniversary": "anniversaries",
	"reminder":    "reminders",
}

// ErrUnknownTrashKind 回收站不认识这个类型（拼错的路径或不该进回收站的记录）。
var ErrUnknownTrashKind = errors.New("回收站里没有这一类记录")

func trashTable(kind string) (string, error) {
	t, ok := trashTables[kind]
	if !ok {
		return "", ErrUnknownTrashKind
	}
	return t, nil
}

// trashQueries 按类型逐条查：六张表要展示的列各不相同，
// 硬拼一条 UNION 得让每个分支凑出同样的列数和含义，反而更难读。
// 列序固定为：id、标题、次要信息、归属人、连带恢复条数、归属人是否也在回收站、删除时间。
// 连接池只有一条连接，所以这里一次把 deleted_at 读出来，不在遍历行时再发查询。
var trashQueries = []struct {
	kind string
	sql  string
}{
	{"person", `SELECT p.id,p.name,'','',
 (SELECT COUNT(*) FROM memos m WHERE m.person_id=p.id AND m.deleted_at IS NULL)
+(SELECT COUNT(*) FROM transactions t WHERE t.person_id=p.id AND t.deleted_at IS NULL)
+(SELECT COUNT(*) FROM anniversaries a WHERE a.person_id=p.id AND a.deleted_at IS NULL)
+(SELECT COUNT(*) FROM reminders r WHERE r.person_id=p.id AND r.deleted_at IS NULL)
+(SELECT COUNT(*) FROM events e WHERE e.deleted_at IS NULL AND EXISTS
  (SELECT 1 FROM event_participants ep WHERE ep.event_id=e.id AND ep.person_id=p.id)),
 0,p.deleted_at
FROM people p WHERE p.deleted_at IS NOT NULL`},
	{"event", `SELECT e.id,e.title,substr(e.event_date,1,10),
 COALESCE((SELECT group_concat(p.name,'、') FROM event_participants ep JOIN people p ON p.id=ep.person_id WHERE ep.event_id=e.id),''),
 0,EXISTS (SELECT 1 FROM event_participants ep JOIN people p ON p.id=ep.person_id
   WHERE ep.event_id=e.id AND p.deleted_at IS NOT NULL),
 e.deleted_at
FROM events e WHERE e.deleted_at IS NOT NULL`},
	{"memo", `SELECT m.id,substr(m.content,1,60),substr(m.said_at,1,10),COALESCE(p.name,''),
 0,(p.deleted_at IS NOT NULL),m.deleted_at
FROM memos m LEFT JOIN people p ON p.id=m.person_id WHERE m.deleted_at IS NOT NULL`},
	{"transaction", `SELECT t.id,COALESCE(NULLIF(t.title,''),CASE t.kind
   WHEN 'loan' THEN '借还' WHEN 'gift' THEN '礼物' WHEN 'expense' THEN '花销' ELSE '其它' END)
  ||' ¥'||printf('%.2f',t.amount_fen/100.0),
 substr(t.occurred_at,1,10),COALESCE(p.name,''),0,(p.deleted_at IS NOT NULL),t.deleted_at
FROM transactions t LEFT JOIN people p ON p.id=t.person_id WHERE t.deleted_at IS NOT NULL`},
	{"anniversary", `SELECT a.id,a.title,substr(a.date,1,10),COALESCE(p.name,''),
 0,(p.deleted_at IS NOT NULL),a.deleted_at
FROM anniversaries a LEFT JOIN people p ON p.id=a.person_id WHERE a.deleted_at IS NOT NULL`},
	{"reminder", `SELECT r.id,r.title,substr(r.due_at,1,10),COALESCE(p.name,''),
 0,(p.deleted_at IS NOT NULL),r.deleted_at
FROM reminders r LEFT JOIN people p ON p.id=r.person_id WHERE r.deleted_at IS NOT NULL`},
}

func (s *Store) TrashList(ctx context.Context) ([]*TrashItem, error) {
	out := []*TrashItem{}
	for _, q := range trashQueries {
		rows, err := s.DB.QueryContext(ctx, q.sql)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			it := &TrashItem{Type: q.kind}
			var title, detail, personName, deletedAt sql.NullString
			var hidden, trashedPerson int
			if err := rows.Scan(&it.ID, &title, &detail, &personName, &hidden, &trashedPerson, &deletedAt); err != nil {
				rows.Close()
				return nil, err
			}
			it.Title = title.String
			it.Detail = detail.String
			it.PersonName = personName.String
			it.DeletedAt = deletedAt.String
			it.Hidden = hidden
			it.PersonTrashed = trashedPerson != 0
			out = append(out, it)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	// 最后删掉的最先出现：误删之后回到回收站，第一眼就该看到它
	sort.SliceStable(out, func(i, j int) bool { return out[i].DeletedAt > out[j].DeletedAt })
	return out, nil
}

// TrashRestore 把一行请回列表。它归属的联系人还在回收站时，这一行依旧看不见，
// 但标记已经清了——恢复联系人时它会跟着回来，语义上仍是「恢复成功」。
func (s *Store) TrashRestore(ctx context.Context, kind, id string) error {
	table, err := trashTable(kind)
	if err != nil {
		return err
	}
	return execOne(ctx, s.DB, "UPDATE "+table+" SET deleted_at=NULL WHERE id=? AND deleted_at IS NOT NULL", id)
}

// TrashHas 这一行是否还躺在回收站里。API 在碰磁盘文件之前先问一句：
// 网页上两个动作之间数据可能已经变了（比如在另一个标签页里恢复过），
// 不能拿着过期的行号去彻底删除一条还在使用的记录。
func (s *Store) TrashHas(ctx context.Context, kind, id string) (bool, error) {
	table, err := trashTable(kind)
	if err != nil {
		return false, err
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE id=? AND deleted_at IS NOT NULL", id).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// TrashPurge 真删除回收站里的一行。各类型的连带关系由外键决定，与软删除之前的删除一致：
// 往来 detach 掉挂账、联系人级联掉字段与关系边、账目级联掉还款、
// 事件删掉参与人行。头像文件不在这里删，由 API 层处理。
func (s *Store) TrashPurge(ctx context.Context, kind, id string) error {
	table, err := trashTable(kind)
	if err != nil {
		return err
	}
	if kind == "event" {
		if _, err := s.DB.ExecContext(ctx, "UPDATE transactions SET event_id=NULL WHERE event_id=?", id); err != nil {
			return err
		}
	}
	// 只删回收站里的行：还没进回收站的记录由这个接口删不掉（404），
	// 避免一次点击把正在使用的记录抹成不可恢复。
	return execOne(ctx, s.DB, "DELETE FROM "+table+" WHERE id=? AND deleted_at IS NOT NULL", id)
}

// TrashEmptyResult 是清空回收站的回执：各类删掉多少行，以及需要清理磁盘的头像。
type TrashEmptyResult struct {
	Purged      map[string]int `json:"purged"`
	Attachments []*Attachment  `json:"-"`
}

// TrashEmpty 清空回收站。先取回将被删掉的联系人的头像，再按「先子后父」删：
// 事件要先把挂着的账解绑，联系人交给外键级联收拾它剩下的关联记录。
func (s *Store) TrashEmpty(ctx context.Context) (*TrashEmptyResult, error) {
	res := &TrashEmptyResult{Purged: map[string]int{}}
	atts, err := s.DB.QueryContext(ctx, `SELECT a.id,a.entity_type,a.entity_id,a.file_name,a.stored_name,a.mime,a.size,a.created_at
FROM attachments a JOIN people p ON p.id=a.entity_id
WHERE a.entity_type='person' AND p.deleted_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	for atts.Next() {
		one := &Attachment{}
		if err := atts.Scan(&one.ID, &one.EntityType, &one.EntityID, &one.FileName, &one.StoredName, &one.Mime, &one.Size, &one.CreatedAt); err != nil {
			atts.Close()
			return nil, err
		}
		res.Attachments = append(res.Attachments, one)
	}
	if err := atts.Err(); err != nil {
		atts.Close()
		return nil, err
	}
	atts.Close()

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	count := func(kind, query string, args ...any) error {
		r, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		res.Purged[kind] += int(n)
		return nil
	}
	// 往来留着（钱确实花过），只是不再挂在被删掉的事件上——与单场删除同口径。
	if _, err := tx.ExecContext(ctx, "UPDATE transactions SET event_id=NULL WHERE event_id IN (SELECT id FROM events WHERE deleted_at IS NOT NULL)"); err != nil {
		return nil, err
	}
	for _, st := range []struct{ kind, sql string }{
		{"event", "DELETE FROM events WHERE deleted_at IS NOT NULL"},
		{"memo", "DELETE FROM memos WHERE deleted_at IS NOT NULL"},
		{"transaction", "DELETE FROM transactions WHERE deleted_at IS NOT NULL"},
		{"anniversary", "DELETE FROM anniversaries WHERE deleted_at IS NOT NULL"},
		{"reminder", "DELETE FROM reminders WHERE deleted_at IS NOT NULL"},
	} {
		if err := count(st.kind, st.sql); err != nil {
			return nil, err
		}
	}
	// 联系人的关联记录（含没进回收站、只是被他带着消失的那些）由外键级联收尾，
	// 这正是「彻底删除联系人」一直以来的代价，所以前一步才要先把头像挑出来。
	if err := count("person", "DELETE FROM people WHERE deleted_at IS NOT NULL"); err != nil {
		return nil, err
	}
	// attachments 没有指向 people 的外键，级联带不掉它，按开头读出的 id 显式清行。
	if len(res.Attachments) > 0 {
		ids := make([]any, 0, len(res.Attachments))
		for _, a := range res.Attachments {
			ids = append(ids, a.ID)
		}
		ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		if _, err := tx.ExecContext(ctx, "DELETE FROM attachments WHERE id IN ("+ph+")", ids...); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}
