package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
)

// ===== Settings =====

func (s *Store) SettingGet(ctx context.Context, key string) (string, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=?", key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) SettingSet(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx,
		"INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return err
}

func (s *Store) SettingAll(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT key,value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// ===== People =====

type Person struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Nickname           string  `json:"nickname,omitempty"`
	Gender             string  `json:"gender,omitempty"`
	Birthday           string  `json:"birthday,omitempty"`
	BirthdayIsLunar    bool    `json:"birthday_is_lunar"`
	AvatarAttachmentID string  `json:"avatar_attachment_id,omitempty"`
	Phone              string  `json:"phone,omitempty"`
	Wechat             string  `json:"wechat,omitempty"`
	Location           string  `json:"location,omitempty"`
	Notes              string  `json:"notes,omitempty"`
	Grade              int     `json:"grade"`
	CategoryID         *int    `json:"category_id,omitempty"`
	Archived           bool    `json:"archived"`
	XAbUID             string  `json:"x_abuid,omitempty"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	CategoryName       *string `json:"category_name,omitempty"`
	Intimacy           *int    `json:"intimacy,omitempty"`
	// BirthdayAnniversaryID 指向由生日自动生成的纪念日，为空表示尚未生成。
	BirthdayAnniversaryID string `json:"birthday_anniversary_id,omitempty"`
}

func (s *Store) PersonCreate(ctx context.Context, p *Person) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	p.CreatedAt = nowUTC()
	p.UpdatedAt = p.CreatedAt
	if p.Grade == 0 {
		p.Grade = 3
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO people(id,name,nickname,gender,birthday,birthday_is_lunar,avatar_attachment_id,phone,wechat,location,notes,grade,category_id,archived,x_abuid,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Nickname, p.Gender, p.Birthday, p.BirthdayIsLunar, p.AvatarAttachmentID,
		p.Phone, p.Wechat, p.Location, p.Notes, p.Grade, p.CategoryID, p.Archived, p.XAbUID, p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *Store) PersonUpdate(ctx context.Context, p *Person) error {
	p.UpdatedAt = nowUTC()
	_, err := s.DB.ExecContext(ctx, `UPDATE people SET name=?,nickname=?,gender=?,birthday=?,birthday_is_lunar=?,avatar_attachment_id=?,phone=?,wechat=?,location=?,notes=?,grade=?,category_id=?,archived=?,x_abuid=?,updated_at=? WHERE id=?`,
		p.Name, p.Nickname, p.Gender, p.Birthday, p.BirthdayIsLunar, p.AvatarAttachmentID,
		p.Phone, p.Wechat, p.Location, p.Notes, p.Grade, p.CategoryID, p.Archived, p.XAbUID, p.UpdatedAt, p.ID)
	return err
}

func (s *Store) PersonDelete(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM people WHERE id=?", id)
	return err
}

// PersonDetachAttachments 返回该人物的附件列表，并把引用清空。
// 磁盘文件由调用方（API 层）删除——store 不碰文件系统。
func (s *Store) PersonDetachAttachments(ctx context.Context, id string) ([]*Attachment, error) {
	atts, err := s.AttachmentsOf(ctx, "person", id)
	if err != nil {
		return nil, err
	}
	if _, err := s.DB.ExecContext(ctx, "UPDATE people SET avatar_attachment_id=NULL WHERE id=?", id); err != nil {
		return nil, err
	}
	return atts, nil
}

// Duplicates 找出可能与给定人物重复的记录：同名，或电话/微信相同。
// excludeID 用于编辑时排除自己。
func (s *Store) PersonDuplicates(ctx context.Context, name, phone, wechat, excludeID string) ([]*Person, error) {
	name = strings.TrimSpace(name)
	if name == "" && strings.TrimSpace(phone) == "" && strings.TrimSpace(wechat) == "" {
		return []*Person{}, nil
	}
	var conds []string
	var args []any
	if name != "" {
		conds = append(conds, "(p.name=? OR p.nickname=?)")
		args = append(args, name, name)
	}
	for _, col := range []string{"phone", "wechat"} {
		v := ""
		if col == "phone" {
			v = strings.TrimSpace(phone)
		} else {
			v = strings.TrimSpace(wechat)
		}
		if v != "" {
			conds = append(conds, "p."+col+"=?")
			args = append(args, v)
		}
	}
	if len(conds) == 0 {
		return []*Person{}, nil
	}
	q := `SELECT p.id,p.name,p.nickname,p.phone,p.wechat,p.grade,p.category_id,p.archived,c.name
FROM people p LEFT JOIN categories c ON p.category_id=c.id WHERE (` + strings.Join(conds, " OR ") + ")"
	if excludeID != "" {
		q += " AND p.id<>?"
		args = append(args, excludeID)
	}
	q += " ORDER BY p.updated_at DESC LIMIT 10"
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Person{}
	for rows.Next() {
		p := &Person{}
		var nick, ph, wc, cat sql.NullString
		var catID sql.NullInt64
		if err := rows.Scan(&p.ID, &p.Name, &nick, &ph, &wc, &p.Grade, &catID, &p.Archived, &cat); err != nil {
			return nil, err
		}
		if nick.Valid { p.Nickname = nick.String }
		if ph.Valid { p.Phone = ph.String }
		if wc.Valid { p.Wechat = wc.String }
		if catID.Valid { v := int(catID.Int64); p.CategoryID = &v }
		if cat.Valid { v := cat.String; p.CategoryName = &v }
		out = append(out, p)
	}
	return out, rows.Err()
}

// PersonMerge 把 source 的关联数据全部迁到 target，然后删除 source。
// 迁移范围：事件参与人、备忘、交易、纪念日、提醒、标签、自定义字段、关系的两端。
// 事件参与人用 INSERT OR IGNORE，避免同一事件里出现重复条目。
func (s *Store) PersonMerge(ctx context.Context, sourceID, targetID string) error {
	if sourceID == "" || targetID == "" || sourceID == targetID {
		return errors.New("需要两个不同的联系人")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	steps := []struct {
		sql  string
		args []any
	}{
		{"INSERT OR IGNORE INTO event_participants(event_id,person_id) SELECT event_id,? FROM event_participants WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE memos SET person_id=? WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE transactions SET person_id=? WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE anniversaries SET person_id=? WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE reminders SET person_id=? WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE OR IGNORE taggings SET target_id=? WHERE target_type='person' AND target_id=?", []any{targetID, sourceID}},
		{"UPDATE person_fields SET person_id=? WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE OR IGNORE relationships SET from_person_id=? WHERE from_person_id=?", []any{targetID, sourceID}},
		{"UPDATE OR IGNORE relationships SET to_person_id=? WHERE to_person_id=?", []any{targetID, sourceID}},
		{"DELETE FROM relationships WHERE from_person_id=? OR to_person_id=?", []any{sourceID, sourceID}},
		{"DELETE FROM event_participants WHERE person_id=?", []any{sourceID}},
		{"DELETE FROM taggings WHERE target_type='person' AND target_id=?", []any{sourceID}},
		{"DELETE FROM people WHERE id=?", []any{sourceID}},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.sql, st.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SyncBirthdayAnniversary 让「人物生日」与「纪念日」保持双向一致，
// 否则 people.birthday 只是一个孤立字段，永远不会进入提醒与建议。
//
// - 有生日：创建或更新一条 source='birthday' 的纪念日，并回写 people.birthday_anniversary_id
// - 无生日：删除此前自动生成的纪念日，避免留下幽灵提醒
func (s *Store) SyncBirthdayAnniversary(ctx context.Context, p *Person) error {
	if p == nil || p.ID == "" {
		return nil
	}
	if strings.TrimSpace(p.Birthday) == "" {
		if p.BirthdayAnniversaryID != "" {
			if _, err := s.DB.ExecContext(ctx, "DELETE FROM anniversaries WHERE id=? AND source='birthday'", p.BirthdayAnniversaryID); err != nil {
				return err
			}
		}
		if _, err := s.DB.ExecContext(ctx, "UPDATE people SET birthday_anniversary_id=NULL WHERE id=?", p.ID); err != nil {
			return err
		}
		p.BirthdayAnniversaryID = ""
		return nil
	}

	// 优先复用已登记的关联纪念日；没有再按来源找一条，避免重复新建。
	annivID := p.BirthdayAnniversaryID
	if annivID == "" {
		var found string
		err := s.DB.QueryRowContext(ctx,
			"SELECT id FROM anniversaries WHERE person_id=? AND source='birthday' ORDER BY created_at LIMIT 1", p.ID).Scan(&found)
		if err == nil && found != "" {
			annivID = found
		}
	}

	title := p.Name + "的生日"
	if annivID != "" {
		if _, err := s.DB.ExecContext(ctx,
			"UPDATE anniversaries SET person_id=?,title=?,date=?,is_lunar=?,repeat_yearly=1 WHERE id=?",
			p.ID, title, p.Birthday, p.BirthdayIsLunar, annivID); err != nil {
			return err
		}
	} else {
		annivID = uuid.NewString()
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO anniversaries(id,person_id,title,date,is_lunar,repeat_yearly,remind_days,created_at,source)
VALUES(?,?,?,?,?,1,'7,3,1,0',?, 'birthday')`,
			annivID, p.ID, title, p.Birthday, p.BirthdayIsLunar, nowUTC()); err != nil {
			return err
		}
	}
	if _, err := s.DB.ExecContext(ctx, "UPDATE people SET birthday_anniversary_id=? WHERE id=?", annivID, p.ID); err != nil {
		return err
	}
	p.BirthdayAnniversaryID = annivID
	return nil
}

func (s *Store) PersonGet(ctx context.Context, id string) (*Person, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT p.id,p.name,p.nickname,p.gender,p.birthday,p.birthday_is_lunar,p.avatar_attachment_id,p.phone,p.wechat,p.location,p.notes,p.grade,p.category_id,p.archived,p.x_abuid,p.created_at,p.updated_at,c.name,p.birthday_anniversary_id
FROM people p LEFT JOIN categories c ON p.category_id=c.id WHERE p.id=?`, id)
	p := &Person{}
	var grade int
	var cat sql.NullString
	var catID sql.NullInt64
	var birthdayAnniv sql.NullString
	if err := row.Scan(&p.ID, &p.Name, &p.Nickname, &p.Gender, &p.Birthday, &p.BirthdayIsLunar, &p.AvatarAttachmentID,
		&p.Phone, &p.Wechat, &p.Location, &p.Notes, &grade, &catID, &p.Archived, &p.XAbUID, &p.CreatedAt, &p.UpdatedAt, &cat, &birthdayAnniv); err != nil {
		return nil, err
	}
	p.Grade = grade
	if birthdayAnniv.Valid { p.BirthdayAnniversaryID = birthdayAnniv.String }
	if catID.Valid {
		v := int(catID.Int64)
		p.CategoryID = &v
	}
	if cat.Valid {
		v := cat.String
		p.CategoryName = &v
	}
	return p, nil
}

func (s *Store) PersonList(ctx context.Context, q string, categoryID, grade int, archived bool, tagID int, limit, offset int) ([]*Person, error) {
	if limit <= 0 {
		limit = 50
	}
	var cond []string
	var args []any
	cond = append(cond, "1=1")
	if !archived {
		cond = append(cond, "p.archived=0")
	}
	if q != "" {
		cond = append(cond, "(p.name LIKE ? OR p.nickname LIKE ? OR p.notes LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if categoryID > 0 {
		cond = append(cond, "p.category_id=?")
		args = append(args, categoryID)
	}
	if grade > 0 {
		cond = append(cond, "p.grade=?")
		args = append(args, grade)
	}
	if tagID > 0 {
		cond = append(cond, "p.id IN (SELECT target_id FROM taggings WHERE tag_id=? AND target_type='person')")
		args = append(args, tagID)
	}
	where := strings.Join(cond, " AND ")
	query := fmt.Sprintf(`SELECT p.id,p.name,p.nickname,p.gender,p.birthday,p.birthday_is_lunar,p.avatar_attachment_id,p.phone,p.wechat,p.location,p.notes,p.grade,p.category_id,p.archived,p.x_abuid,p.created_at,p.updated_at,c.name
FROM people p LEFT JOIN categories c ON p.category_id=c.id
WHERE %s ORDER BY p.grade DESC, p.updated_at DESC LIMIT ? OFFSET ?`, where)
	args = append(args, limit, offset)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Person{}
	for rows.Next() {
		p := &Person{}
		var grade int
		var cat sql.NullString
		var catID sql.NullInt64
	if err := rows.Scan(&p.ID, &p.Name, &p.Nickname, &p.Gender, &p.Birthday, &p.BirthdayIsLunar, &p.AvatarAttachmentID,
		&p.Phone, &p.Wechat, &p.Location, &p.Notes, &grade, &catID, &p.Archived, &p.XAbUID, &p.CreatedAt, &p.UpdatedAt, &cat); err != nil {
			return nil, err
		}
		p.Grade = grade
		if catID.Valid {
			v := int(catID.Int64)
			p.CategoryID = &v
		}
		if cat.Valid {
			v := cat.String
			p.CategoryName = &v
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

func (s *Store) PersonCount(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM people WHERE archived=0").Scan(&n)
	return n, err
}

// ===== PersonFields =====

type PersonField struct {
	ID        string `json:"id"`
	PersonID  string `json:"person_id"`
	Label     string `json:"label"`
	Value     string `json:"value"`
	SortOrder int    `json:"sort_order"`
}

func (s *Store) PersonFieldList(ctx context.Context, personID string) ([]*PersonField, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,person_id,label,value,sort_order FROM person_fields WHERE person_id=? ORDER BY sort_order,label", personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*PersonField{}
	for rows.Next() {
		f := &PersonField{}
		if err := rows.Scan(&f.ID, &f.PersonID, &f.Label, &f.Value, &f.SortOrder); err != nil {
			return nil, err
		}
		list = append(list, f)
	}
	return list, rows.Err()
}

func (s *Store) PersonFieldUpsert(ctx context.Context, f *PersonField) error {
	if f.ID == "" {
		f.ID = uuid.NewString()
	}
	_, err := s.DB.ExecContext(ctx,
		"INSERT INTO person_fields(id,person_id,label,value,sort_order) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET label=excluded.label,value=excluded.value,sort_order=excluded.sort_order",
		f.ID, f.PersonID, f.Label, f.Value, f.SortOrder)
	return err
}

func (s *Store) PersonFieldDelete(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM person_fields WHERE id=?", id)
	return err
}

// ===== Categories / Tags / EventTypes =====

type Category struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sort_order"`
}

func (s *Store) CategoryList(ctx context.Context) ([]*Category, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,name,color,icon,sort_order FROM categories ORDER BY sort_order,name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Category{}
	for rows.Next() {
		c := &Category{}
		if err := rows.Scan(&c.ID, &c.Name, &c.Color, &c.Icon, &c.SortOrder); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) CategoryUpsert(ctx context.Context, c *Category) error {
	if c.ID == 0 {
		r, err := s.DB.ExecContext(ctx, "INSERT INTO categories(name,color,icon,sort_order) VALUES(?,?,?,?)", c.Name, c.Color, c.Icon, c.SortOrder)
		if err != nil {
			return err
		}
		id, _ := r.LastInsertId()
		c.ID = int(id)
		return nil
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE categories SET name=?,color=?,icon=?,sort_order=? WHERE id=?", c.Name, c.Color, c.Icon, c.SortOrder, c.ID)
	return err
}

// CategoryDelete 删除圈子，并把所属人物的圈子置空，避免留下悬空引用。
func (s *Store) CategoryDelete(ctx context.Context, id int) error {
	if _, err := s.DB.ExecContext(ctx, "UPDATE people SET category_id=NULL WHERE category_id=?", id); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, "DELETE FROM categories WHERE id=?", id)
	return err
}

type Tag struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Store) TagList(ctx context.Context) ([]*Tag, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,name,color FROM tags ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Tag{}
	for rows.Next() {
		t := &Tag{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

func (s *Store) TagUpsert(ctx context.Context, t *Tag) error {
	if t.ID == 0 {
		r, err := s.DB.ExecContext(ctx, "INSERT INTO tags(name,color) VALUES(?,?)", t.Name, t.Color)
		if err != nil {
			return err
		}
		id, _ := r.LastInsertId()
		t.ID = int(id)
		return nil
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE tags SET name=?,color=? WHERE id=?", t.Name, t.Color, t.ID)
	return err
}

// TagDelete 删除标签。taggings 有外键级联，这里只需保证记录本身被清掉。
func (s *Store) TagDelete(ctx context.Context, id int) error {
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM taggings WHERE tag_id=?", id); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, "DELETE FROM tags WHERE id=?", id)
	return err
}

type EventType struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Icon      string `json:"icon"`
	IsDefault bool   `json:"is_default"`
	SortOrder int    `json:"sort_order"`
}

func (s *Store) EventTypeList(ctx context.Context) ([]*EventType, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,name,color,icon,is_default,sort_order FROM event_types ORDER BY sort_order,name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*EventType{}
	for rows.Next() {
		e := &EventType{}
		if err := rows.Scan(&e.ID, &e.Name, &e.Color, &e.Icon, &e.IsDefault, &e.SortOrder); err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

func (s *Store) EventTypeUpsert(ctx context.Context, e *EventType) error {
	if e.ID == 0 {
		r, err := s.DB.ExecContext(ctx, "INSERT INTO event_types(name,color,icon,is_default,sort_order) VALUES(?,?,?,?,?)",
			e.Name, e.Color, e.Icon, e.IsDefault, e.SortOrder)
		if err != nil {
			return err
		}
		id, _ := r.LastInsertId()
		e.ID = int(id)
		return nil
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE event_types SET name=?,color=?,icon=?,is_default=?,sort_order=? WHERE id=?",
		e.Name, e.Color, e.Icon, e.IsDefault, e.SortOrder, e.ID)
	return err
}

// EventTypeDelete 删除事件类型，并把引用它的往来置为「未分类」。
func (s *Store) EventTypeDelete(ctx context.Context, id int) error {
	if _, err := s.DB.ExecContext(ctx, "UPDATE events SET type_id=NULL WHERE type_id=?", id); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, "DELETE FROM event_types WHERE id=?", id)
	return err
}

// ===== Tagging =====

func (s *Store) TagAdd(ctx context.Context, targetType, targetID string, tagID int) error {
	_, err := s.DB.ExecContext(ctx, "INSERT OR IGNORE INTO taggings(tag_id,target_type,target_id) VALUES(?,?,?)", tagID, targetType, targetID)
	return err
}

func (s *Store) TagRemove(ctx context.Context, targetType, targetID string, tagID int) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM taggings WHERE tag_id=? AND target_type=? AND target_id=?", tagID, targetType, targetID)
	return err
}

func (s *Store) TagsOf(ctx context.Context, targetType, targetID string) ([]*Tag, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT t.id,t.name,t.color FROM tags t JOIN taggings g ON g.tag_id=t.id WHERE g.target_type=? AND g.target_id=? ORDER BY t.name`,
		targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Tag{}
	for rows.Next() {
		t := &Tag{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// ===== Intimacy helpers =====

func calcIntimacyScore(grade, eventCount, memoCount, txCount, daysSinceLast int) int {
	base := grade * 8
	activity := eventCount*10 + memoCount*4 + txCount*3
	decay := math.Exp(-float64(daysSinceLast) / 60.0)
	score := float64(base) + float64(activity)*decay
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	return int(score)
}
