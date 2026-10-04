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
	ID    string `json:"id"`
	Name  string `json:"name"`
	// FamilyName/GivenName 保留 vCard 的姓/名结构，显示顺序由 ComposeName 决定
	FamilyName         string  `json:"family_name,omitempty"`
	GivenName          string  `json:"given_name,omitempty"`
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
	Archived           bool    `json:"archived"`
	XAbUID             string  `json:"x_abuid,omitempty"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	// CategoryIDs 只用于写入（PUT 是整行覆盖，缺省即清空圈子）；
	// Categories 只用于读取，带颜色供列表胶囊与图谱拼色。
	CategoryIDs []int              `json:"category_ids,omitempty"`
	Categories  []*PersonCategory  `json:"categories,omitempty"`
	Intimacy    *int               `json:"intimacy,omitempty"`
	// BirthdayAnniversaryID 指向由生日自动生成的纪念日，为空表示尚未生成。
	BirthdayAnniversaryID string `json:"birthday_anniversary_id,omitempty"`
	// IntroducedByPersonID 认识来源：通过谁认识此人；空串表示与我直接认识。
	// 引荐人链沿这一列自引用串成，关系图上用它还原「我—…—TA」的认识路径。
	IntroducedByPersonID string `json:"introduced_by_person_id,omitempty"`
	// IntroducedByName 只用于读取，PersonGet 时带出引荐人姓名。
	IntroducedByName string `json:"introduced_by_name,omitempty"`
}

// ErrIntroMissing 引荐人指向了不存在的人。
var ErrIntroMissing = errors.New("引荐人不存在")

// ErrIntroCycle 引荐人链回到了自己（含直接选自己）。
var ErrIntroCycle = errors.New("引荐人不能形成循环，也不能是自己")

// checkIntroducer 校验引荐人：可空；非空时必须存在，且沿链不能回到 personID 自身。
func (s *Store) checkIntroducer(ctx context.Context, personID, introID string) error {
	introID = strings.TrimSpace(introID)
	if introID == "" {
		return nil
	}
	// personID 预置进 seen：新建时本人尚不存在，但若客户端把引荐人填成本人 ID 也在这拦住。
	seen := map[string]bool{personID: true}
	cur := introID
	for {
		if seen[cur] {
			return ErrIntroCycle
		}
		var next sql.NullString
		err := s.DB.QueryRowContext(ctx, "SELECT introduced_by_person_id FROM people WHERE id=?", cur).Scan(&next)
		if err == sql.ErrNoRows {
			return ErrIntroMissing
		}
		if err != nil {
			return err
		}
		seen[cur] = true
		if !next.Valid || next.String == "" {
			return nil
		}
		cur = next.String
	}
}

func (s *Store) PersonCreate(ctx context.Context, p *Person) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if err := s.checkIntroducer(ctx, p.ID, p.IntroducedByPersonID); err != nil {
		return err
	}
	p.CreatedAt = nowUTC()
	p.UpdatedAt = p.CreatedAt
	_, err := s.DB.ExecContext(ctx, `INSERT INTO people(id,name,family_name,given_name,nickname,gender,birthday,birthday_is_lunar,avatar_attachment_id,phone,wechat,location,notes,grade,archived,x_abuid,created_at,updated_at,introduced_by_person_id)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.FamilyName, p.GivenName, p.Nickname, p.Gender, p.Birthday, p.BirthdayIsLunar, p.AvatarAttachmentID,
		p.Phone, p.Wechat, p.Location, p.Notes, p.Grade, p.Archived, p.XAbUID, p.CreatedAt, p.UpdatedAt, nullableString(p.IntroducedByPersonID))
	if err != nil {
		return err
	}
	return s.replacePersonCategories(ctx, p.ID, p.CategoryIDs)
}

func (s *Store) PersonUpdate(ctx context.Context, p *Person) error {
	if err := s.checkIntroducer(ctx, p.ID, p.IntroducedByPersonID); err != nil {
		return err
	}
	p.UpdatedAt = nowUTC()
	if err := execOne(ctx, s.DB, `UPDATE people SET name=?,family_name=?,given_name=?,nickname=?,gender=?,birthday=?,birthday_is_lunar=?,avatar_attachment_id=?,phone=?,wechat=?,location=?,notes=?,grade=?,archived=?,x_abuid=?,updated_at=?,introduced_by_person_id=? WHERE id=?`,
		p.Name, p.FamilyName, p.GivenName, p.Nickname, p.Gender, p.Birthday, p.BirthdayIsLunar, p.AvatarAttachmentID,
		p.Phone, p.Wechat, p.Location, p.Notes, p.Grade, p.Archived, p.XAbUID, p.UpdatedAt, nullableString(p.IntroducedByPersonID), p.ID); err != nil {
		return err
	}
	return s.replacePersonCategories(ctx, p.ID, p.CategoryIDs)
}

// nullableString 把空串落成 SQL NULL，避免引荐人列存进无意义的 ''。
func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// replacePersonCategories 用 ids 整体替换一个人的圈子（PUT 是整行覆盖，圈子同样按覆盖处理）。
func (s *Store) replacePersonCategories(ctx context.Context, personID string, ids []int) error {
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM person_categories WHERE person_id=?", personID); err != nil {
		return err
	}
	for _, id := range dedupeInts(ids) {
		if _, err := s.DB.ExecContext(ctx, "INSERT OR IGNORE INTO person_categories(person_id,category_id) VALUES(?,?)", personID, id); err != nil {
			return err
		}
	}
	return nil
}

// PersonCategory 是读路径上挂在人身上的圈子，带颜色供列表胶囊与图谱拼色。
type PersonCategory struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// categoriesFor 一次取回一批人的圈子。没有并进主查询的 JOIN：
// 多对多之后一人出多行，主查询的 LIMIT/OFFSET 会按行数分页，每页人数就错了。
// 排序固定按 sort_order/name，拼色的分段顺序才不会随查询而变。
func (s *Store) categoriesFor(ctx context.Context, ids []string) (map[string][]*PersonCategory, error) {
	out := map[string][]*PersonCategory{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT g.person_id,c.id,c.name,c.color
FROM person_categories g JOIN categories c ON c.id=g.category_id
WHERE g.person_id IN (`+ph+`) ORDER BY c.sort_order,c.name,c.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid string
		c := &PersonCategory{}
		if err := rows.Scan(&pid, &c.ID, &c.Name, &c.Color); err != nil {
			return nil, err
		}
		out[pid] = append(out[pid], c)
	}
	return out, rows.Err()
}

// attachCategories 给一组人补上圈子。
func (s *Store) attachCategories(ctx context.Context, list []*Person) error {
	ids := make([]string, len(list))
	for i, p := range list {
		ids[i] = p.ID
	}
	m, err := s.categoriesFor(ctx, ids)
	if err != nil {
		return err
	}
	for _, p := range list {
		p.Categories = m[p.ID]
	}
	return nil
}

// TagsFor 批量取一批人身上的标签，关系图用它一次性带出标签做筛选，
// 避免前端逐人请求 /taggings/of。
func (s *Store) TagsFor(ctx context.Context, ids []string) (map[string][]*Tag, error) {
	out := map[string][]*Tag{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT g.target_id,t.id,t.name,t.color
FROM taggings g JOIN tags t ON t.id=g.tag_id
WHERE g.target_type='person' AND g.target_id IN (`+ph+`) ORDER BY t.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid string
		t := &Tag{}
		if err := rows.Scan(&pid, &t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		out[pid] = append(out[pid], t)
	}
	return out, rows.Err()
}

// dedupeInts 去掉 0 与重复值：前端用 0 表示「未归入圈子」。
func dedupeInts(ids []int) []int {
	out := []int{}
	seen := map[int]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// PersonUpdateNameParts 只更新姓名三件套（显示名/姓/名），
// 供 vCard 重导入时按 X-ABUID 匹配回填，不触碰用户在应用内维护的其他字段。
//
// 这里刻意不用 execOne：id 来自同一批导入开头读出的人物列表，中间被并发删除属于
// 正常现象，报 ErrNoRows 会让整份 vcf 停在半路，而导入侧本就按「命中即回填」计数。
func (s *Store) PersonUpdateNameParts(ctx context.Context, id, name, familyName, givenName string) error {
	_, err := s.DB.ExecContext(ctx,
		"UPDATE people SET name=?,family_name=?,given_name=?,updated_at=? WHERE id=?",
		name, familyName, givenName, nowUTC(), id)
	return err
}

// ComposeName 由姓/名拼出显示名：含中文按「姓+名」（中文习惯姓在前），
// 纯西文按「名 姓」（Western 习惯）。只有其一则直接用那个。
func ComposeName(family, given string) string {
	switch {
	case family != "" && given != "":
		if hasHan(family + given) {
			return family + given
		}
		return given + " " + family
	case family != "":
		return family
	default:
		return given
	}
}

func hasHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func (s *Store) PersonDelete(ctx context.Context, id string) error {
	return execOne(ctx, s.DB, "DELETE FROM people WHERE id=?", id)
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
	q := `SELECT p.id,p.name,p.nickname,p.phone,p.wechat,p.grade,p.archived
FROM people p WHERE (` + strings.Join(conds, " OR ") + ")"
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
		var nick, ph, wc sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &nick, &ph, &wc, &p.Grade, &p.Archived); err != nil {
			return nil, err
		}
		if nick.Valid { p.Nickname = nick.String }
		if ph.Valid { p.Phone = ph.String }
		if wc.Valid { p.Wechat = wc.String }
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachCategories(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// PersonMerge 把 source 的关联数据全部迁到 target，然后删除 source。
// 迁移范围：事件参与人、备忘、交易、纪念日、提醒、标签、圈子、自定义字段、关系的两端。
// 事件参与人用 INSERT OR IGNORE，避免同一事件里出现重复条目。
// source 的生日纪念日不迁移（它代表 source 自身的生日，而 birthday 字段留在 target 不变），
// 合并完成后按 target 自己的生日重新同步一次。
func (s *Store) PersonMerge(ctx context.Context, sourceID, targetID string) error {
	if sourceID == "" || targetID == "" || sourceID == targetID {
		return errors.New("需要两个不同的联系人")
	}
	// 引荐人重定向要特殊处理 target 自己：target 原本若通过 source 认识，
	// source 一删这条链就断了，提前把 source 的引荐人续给 target；
	// 若 source 的引荐人恰好是 target（两人互指），只能置空，避免留自环。
	var srcIntro sql.NullString
	if err := s.DB.QueryRowContext(ctx, "SELECT introduced_by_person_id FROM people WHERE id=?", sourceID).Scan(&srcIntro); err != nil && err != sql.ErrNoRows {
		return err
	}
	var targetIntro any
	if srcIntro.Valid && srcIntro.String != targetID {
		targetIntro = srcIntro.String
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
		// 先处理 target 自身的引荐人（仅当它原本指向 source）
		{"UPDATE people SET introduced_by_person_id=? WHERE id=? AND introduced_by_person_id=?", []any{targetIntro, targetID, sourceID}},
		// 其余指向 source 的人改指 target（target 行已在上一步处理完，排除掉）
		{"UPDATE people SET introduced_by_person_id=? WHERE introduced_by_person_id=? AND id<>?", []any{targetID, sourceID, targetID}},
		{"INSERT OR IGNORE INTO event_participants(event_id,person_id) SELECT event_id,? FROM event_participants WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE memos SET person_id=? WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE transactions SET person_id=? WHERE person_id=?", []any{targetID, sourceID}},
		// 生日纪念日跟着 source 的 birthday 走，而 birthday 不参与合并，先删掉避免与 target 的重复提醒
		{"DELETE FROM anniversaries WHERE source='birthday' AND (person_id=? OR id=(SELECT birthday_anniversary_id FROM people WHERE id=?))", []any{sourceID, sourceID}},
		{"UPDATE anniversaries SET person_id=? WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE reminders SET person_id=? WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE OR IGNORE taggings SET target_id=? WHERE target_type='person' AND target_id=?", []any{targetID, sourceID}},
		// 圈子可多个，交并集：target 已有的组合靠 OR IGNORE 跳过
		{"INSERT OR IGNORE INTO person_categories(person_id,category_id) SELECT ?,category_id FROM person_categories WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE person_fields SET person_id=? WHERE person_id=?", []any{targetID, sourceID}},
		{"UPDATE OR IGNORE relationships SET from_person_id=? WHERE from_person_id=?", []any{targetID, sourceID}},
		{"UPDATE OR IGNORE relationships SET to_person_id=? WHERE to_person_id=?", []any{targetID, sourceID}},
		{"DELETE FROM relationships WHERE from_person_id=? OR to_person_id=?", []any{sourceID, sourceID}},
		{"DELETE FROM event_participants WHERE person_id=?", []any{sourceID}},
		{"DELETE FROM taggings WHERE target_type='person' AND target_id=?", []any{sourceID}},
		{"DELETE FROM person_categories WHERE person_id=?", []any{sourceID}},
		{"DELETE FROM people WHERE id=?", []any{sourceID}},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.sql, st.args...); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// 合并后 target 的生日关联可能失效（例如 target 只有 birthday 却从未生成纪念日），
	// 按 target 自身的生日补同步。必须在提交之后：Sync 走独立连接，
	// 而单连接池下未提交的事务会与之互等。
	t, err := s.PersonGet(ctx, targetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // target 本就不存在，前面的语句都是 0 行
		}
		return err
	}
	return s.SyncBirthdayAnniversary(ctx, t)
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
	row := s.DB.QueryRowContext(ctx, `SELECT p.id,p.name,p.family_name,p.given_name,p.nickname,p.gender,p.birthday,p.birthday_is_lunar,p.avatar_attachment_id,p.phone,p.wechat,p.location,p.notes,p.grade,p.archived,p.x_abuid,p.created_at,p.updated_at,p.birthday_anniversary_id,p.introduced_by_person_id,
(SELECT pi.name FROM people pi WHERE pi.id=p.introduced_by_person_id)
FROM people p WHERE p.id=?`, id)
	p := &Person{}
	var grade int
	var birthdayAnniv sql.NullString
	// 删除头像时 PersonDetachAttachments 会把该列写成 NULL，不能用裸 string 扫。
	var avatar sql.NullString
	var intro, introName sql.NullString
	if err := row.Scan(&p.ID, &p.Name, &p.FamilyName, &p.GivenName, &p.Nickname, &p.Gender, &p.Birthday, &p.BirthdayIsLunar, &avatar,
		&p.Phone, &p.Wechat, &p.Location, &p.Notes, &grade, &p.Archived, &p.XAbUID, &p.CreatedAt, &p.UpdatedAt, &birthdayAnniv, &intro, &introName); err != nil {
		return nil, err
	}
	p.Grade = grade
	p.AvatarAttachmentID = avatar.String
	if birthdayAnniv.Valid { p.BirthdayAnniversaryID = birthdayAnniv.String }
	if intro.Valid { p.IntroducedByPersonID = intro.String }
	if introName.Valid { p.IntroducedByName = introName.String }
	if err := s.attachCategories(ctx, []*Person{p}); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) PersonList(ctx context.Context, q string, categoryID, grade int, archived bool, tagID int, limit, offset int) ([]*Person, error) {
	return s.queryPeople(ctx, q, categoryID, grade, archived, false, tagID, limit, offset)
}

// PersonArchivedOnly 只翻已归档的人物，供列表页的「已归档」筛选使用。
func (s *Store) PersonArchivedOnly(ctx context.Context, q string, categoryID, grade, tagID, limit, offset int) ([]*Person, error) {
	return s.queryPeople(ctx, q, categoryID, grade, false, true, tagID, limit, offset)
}

func (s *Store) queryPeople(ctx context.Context, q string, categoryID, grade int, includeArchived, onlyArchived bool, tagID, limit, offset int) ([]*Person, error) {
	if limit <= 0 {
		limit = 50
	}
	var cond []string
	var args []any
	cond = append(cond, "1=1")
	switch {
	case onlyArchived:
		cond = append(cond, "p.archived=1")
	case !includeArchived:
		cond = append(cond, "p.archived=0")
	}
	if q != "" {
		cond = append(cond, "(p.name LIKE ? OR p.nickname LIKE ? OR p.notes LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if categoryID > 0 {
		cond = append(cond, "p.id IN (SELECT person_id FROM person_categories WHERE category_id=?)")
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
	// 圈子不在这个查询里取：多对多之后 JOIN 会一人出多行，
	// 下面的 LIMIT/OFFSET 是按行分页的，每页人数就会被圈子数压掉。改成取完当页再批量补。
	query := fmt.Sprintf(`SELECT p.id,p.name,p.family_name,p.given_name,p.nickname,p.gender,p.birthday,p.birthday_is_lunar,p.avatar_attachment_id,p.phone,p.wechat,p.location,p.notes,p.grade,p.archived,p.x_abuid,p.created_at,p.updated_at,p.introduced_by_person_id
FROM people p
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
		var avatar sql.NullString
		var intro sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &p.FamilyName, &p.GivenName, &p.Nickname, &p.Gender, &p.Birthday, &p.BirthdayIsLunar, &avatar,
			&p.Phone, &p.Wechat, &p.Location, &p.Notes, &grade, &p.Archived, &p.XAbUID, &p.CreatedAt, &p.UpdatedAt, &intro); err != nil {
			return nil, err
		}
		p.Grade = grade
		p.AvatarAttachmentID = avatar.String
		if intro.Valid { p.IntroducedByPersonID = intro.String }
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachCategories(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
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
	return execOne(ctx, s.DB, "DELETE FROM person_fields WHERE id=?", id)
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
	return execOne(ctx, s.DB, "UPDATE categories SET name=?,color=?,icon=?,sort_order=? WHERE id=?", c.Name, c.Color, c.Icon, c.SortOrder, c.ID)
}

// CategoryDelete 删除圈子。people 侧不再挂列，成员关系由
// person_categories 的 ON DELETE CASCADE 一并清掉。
func (s *Store) CategoryDelete(ctx context.Context, id int) error {
	return execOne(ctx, s.DB, "DELETE FROM categories WHERE id=?", id)
}

// PeopleAddCategories 把一批人各加进若干个圈子，只增不减，已在圈子里的跳过。
// ids 里的幽灵 id（选人后被别人删掉）由 JOIN people 自然过滤掉，
// 返回值是真正新增的成员关系条数，供前端提示「其中 N 位原本已在」。
func (s *Store) PeopleAddCategories(ctx context.Context, ids []string, categoryIDs []int) (int, error) {
	ids = dedupeStrings(ids)
	cats := dedupeInts(categoryIDs)
	if len(ids) == 0 || len(cats) == 0 {
		return 0, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	added := 0
	for _, cat := range cats {
		args = args[:0]
		args = append(args, cat)
		for _, id := range ids {
			args = append(args, id)
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO person_categories(person_id,category_id)
SELECT p.id,? FROM people p WHERE p.id IN (`+ph+`) ON CONFLICT DO NOTHING`, args...)
		if err != nil {
			return 0, err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return 0, err
		}
		added += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return added, nil
}

// dedupeStrings 保序去重，并丢掉空串。
func dedupeStrings(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
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
	return execOne(ctx, s.DB, "UPDATE tags SET name=?,color=? WHERE id=?", t.Name, t.Color, t.ID)
}

// TagDelete 删除标签。taggings 有外键级联，这里只需保证记录本身被清掉。
func (s *Store) TagDelete(ctx context.Context, id int) error {
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM taggings WHERE tag_id=?", id); err != nil {
		return err
	}
	return execOne(ctx, s.DB, "DELETE FROM tags WHERE id=?", id)
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
	return execOne(ctx, s.DB, "UPDATE event_types SET name=?,color=?,icon=?,is_default=?,sort_order=? WHERE id=?",
		e.Name, e.Color, e.Icon, e.IsDefault, e.SortOrder, e.ID)
}

// EventTypeDelete 删除事件类型，并把引用它的往来置为「未分类」。
func (s *Store) EventTypeDelete(ctx context.Context, id int) error {
	if _, err := s.DB.ExecContext(ctx, "UPDATE events SET type_id=NULL WHERE type_id=?", id); err != nil {
		return err
	}
	return execOne(ctx, s.DB, "DELETE FROM event_types WHERE id=?", id)
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
