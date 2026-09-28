package store

import (
	"context"
	"database/sql"
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

func (s *Store) PersonGet(ctx context.Context, id string) (*Person, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT p.id,p.name,p.nickname,p.gender,p.birthday,p.birthday_is_lunar,p.avatar_attachment_id,p.phone,p.wechat,p.location,p.notes,p.grade,p.category_id,p.archived,p.x_abuid,p.created_at,p.updated_at,c.name
FROM people p LEFT JOIN categories c ON p.category_id=c.id WHERE p.id=?`, id)
	p := &Person{}
	var grade int
	var cat sql.NullString
	var catID sql.NullInt64
	if err := row.Scan(&p.ID, &p.Name, &p.Nickname, &p.Gender, &p.Birthday, &p.BirthdayIsLunar, &p.AvatarAttachmentID,
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

func (s *Store) CategoryDelete(ctx context.Context, id int) error {
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

func (s *Store) TagDelete(ctx context.Context, id int) error {
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

func (s *Store) EventTypeDelete(ctx context.Context, id int) error {
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
