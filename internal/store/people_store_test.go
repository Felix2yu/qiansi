package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ===== 测试辅助（名字带 people 前缀，避免与其他测试文件冲突） =====

func pctx(t *testing.T) context.Context {
	t.Helper()
	// 超时 30s：CI 上 SQLite 首次建库稍慢，但正常情况下所有用例都远小于此。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// pCancelledCtx 返回一个已取消的 ctx，用来触发 DB 错误分支。
func pCancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func pMustCreate(t *testing.T, s *Store, p *Person) *Person {
	t.Helper()
	if err := s.PersonCreate(pctx(t), p); err != nil {
		t.Fatalf("PersonCreate(%s): %v", p.Name, err)
	}
	return p
}

// pRaw 执行原始 SQL，失败即 fatal。
func pRaw(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.DB.Exec(query, args...); err != nil {
		t.Fatalf("raw exec %q: %v", query, err)
	}
}

func pRawScanInt(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("raw scan %q: %v", query, err)
	}
	return n
}

func pRawScanStr(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	var v string
	if err := s.DB.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("raw scan %q: %v", query, err)
	}
	return v
}

func pContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func pIDs(list []*Person) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.ID)
	}
	return out
}

func pNames(list []*Person) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.Name)
	}
	return out
}

func pFieldLabels(list []*PersonField) []string {
	out := make([]string, 0, len(list))
	for _, f := range list {
		out = append(out, f.Label)
	}
	return out
}

func pTagNames(list []*Tag) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.Name)
	}
	return out
}

// ===== New / 时间辅助（store.go） =====

func TestStoreNewWrapsDB(t *testing.T) {
	db := newTestDB(t)
	s := New(db)
	if s == nil || s.DB != db {
		t.Fatalf("New 应把 *sql.DB 原样挂到 Store.DB 上")
	}
	if err := s.DB.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestStoreTodayLocalAndDaysAgoLocal(t *testing.T) {
	today := TodayLocal()
	if _, err := time.Parse("2006-01-02", today); err != nil {
		t.Fatalf("TodayLocal 格式应为 YYYY-MM-DD，得到 %q: %v", today, err)
	}
	// 与本地时间手工格式化结果一致（不是 UTC）
	if want := time.Now().Format("2006-01-02"); today != want {
		t.Fatalf("TodayLocal = %q，期望本地日期 %q", today, want)
	}
	if got := DaysAgoLocal(0); got != today {
		t.Fatalf("DaysAgoLocal(0) = %q，应等于 TodayLocal %q", got, today)
	}

	tests := []struct {
		n      int
		offset int
	}{
		{1, -1},
		{7, -7},
		{-3, 3}, // 负数天 => 未来
	}
	for _, tc := range tests {
		got := DaysAgoLocal(tc.n)
		want := time.Now().AddDate(0, 0, tc.offset).Format("2006-01-02")
		if got != want {
			t.Errorf("DaysAgoLocal(%d) = %q, 期望 %q", tc.n, got, want)
		}
	}
	// 内部辅助与导出版本保持一致
	if todayLocal() != TodayLocal() {
		t.Errorf("todayLocal/TodayLocal 不一致")
	}
	if daysFromTodayLocal(-5) != DaysAgoLocal(5) {
		t.Errorf("daysFromTodayLocal 与 DaysAgoLocal 不一致")
	}
	if len(nowUTC()) == 0 || len(nowLocal()) == 0 || len(localCutoff(3)) == 0 {
		t.Errorf("时间辅助函数不应返回空串")
	}
	if got := nowUTC(); !strings.HasSuffix(got, "Z") {
		t.Errorf("nowUTC 应为 UTC 带时区写法，得到 %q", got)
	}
}

// ===== Settings =====

func TestPeopleSettingSetGetAllAndNotFound(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	// 不存在的 key 返回空串而非错误
	v, err := s.SettingGet(ctx, "missing.key")
	if err != nil {
		t.Fatalf("SettingGet(missing): %v", err)
	}
	if v != "" {
		t.Fatalf("缺失 key 应返回空串，得到 %q", v)
	}

	if err := s.SettingSet(ctx, "theme", "dark"); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	if v, _ := s.SettingGet(ctx, "theme"); v != "dark" {
		t.Fatalf("SettingGet(theme) = %q，期望 dark", v)
	}
	// upsert：同名 key 覆盖
	if err := s.SettingSet(ctx, "theme", "light"); err != nil {
		t.Fatalf("SettingSet(update): %v", err)
	}
	if v, _ := s.SettingGet(ctx, "theme"); v != "light" {
		t.Fatalf("覆盖后 = %q，期望 light", v)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM settings WHERE key='theme'"); n != 1 {
		t.Fatalf("upsert 不应新增行，行数 = %d", n)
	}

	if err := s.SettingSet(ctx, "locale", "zh-CN"); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	all, err := s.SettingAll(ctx)
	if err != nil {
		t.Fatalf("SettingAll: %v", err)
	}
	if len(all) != 2 || all["theme"] != "light" || all["locale"] != "zh-CN" {
		t.Fatalf("SettingAll = %+v，期望两条记录", all)
	}

	// DB 错误分支
	if _, err := s.SettingGet(pCancelledCtx(), "theme"); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.SettingSet(pCancelledCtx(), "k", "v"); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if _, err := s.SettingAll(pCancelledCtx()); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}

	// settings.key 是 TEXT 主键，SQLite 允许 NULL：这种脏行会让 SettingAll 扫描失败
	bad := newTestStore(t)
	pRaw(t, bad, "INSERT INTO settings(key,value) VALUES(NULL,'脏数据')")
	if _, err := bad.SettingAll(pctx(t)); err == nil {
		t.Errorf("key 为 NULL 时 SettingAll 应返回扫描错误")
	}
}

// ===== PersonCreate / PersonGet =====

func TestPeopleCreateDefaultsAndRoundTrip(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	// grade 不再默认赋值（0 = 未设置亲密度）；ID 为空时自动生成 uuid
	p := &Person{
		Name:            "张三丰",
		FamilyName:      "张",
		GivenName:       "三丰",
		Nickname:        "三哥",
		Gender:          "male",
		Birthday:        "1990-05-06",
		Phone:           "13800000000",
		Wechat:          "zsf_wx",
		Location:        "杭州",
		Notes:           "武当山同事",
		Archived:        false,
		XAbUID:          "ABUID-1",
		BirthdayIsLunar: true,
	}
	if err := s.PersonCreate(ctx, p); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	if p.ID == "" || len(p.ID) < 32 {
		t.Fatalf("应自动生成 uuid，得到 %q", p.ID)
	}
	if p.Grade != 0 {
		t.Fatalf("grade 默认应为 0（未设置亲密度），得到 %d", p.Grade)
	}
	if p.CreatedAt == "" || p.UpdatedAt != p.CreatedAt {
		t.Fatalf("created_at/updated_at 应被写入且相同: %q %q", p.CreatedAt, p.UpdatedAt)
	}

	got, err := s.PersonGet(ctx, p.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if got.Name != "张三丰" || got.FamilyName != "张" || got.GivenName != "三丰" ||
		got.Nickname != "三哥" || got.Gender != "male" || got.Birthday != "1990-05-06" ||
		!got.BirthdayIsLunar || got.Phone != "13800000000" || got.Wechat != "zsf_wx" ||
		got.Location != "杭州" || got.Notes != "武当山同事" || got.Grade != 0 ||
		got.Archived || got.XAbUID != "ABUID-1" || got.ID != p.ID ||
		got.CreatedAt != p.CreatedAt {
		t.Fatalf("PersonGet 回读不一致: %+v", got)
	}
	if len(got.Categories) != 0 {
		t.Fatalf("无圈子时 Categories 应为空: %+v", got.Categories)
	}
	if got.BirthdayAnniversaryID != "" {
		t.Fatalf("未生成纪念日时 BirthdayAnniversaryID 应为空，得到 %q", got.BirthdayAnniversaryID)
	}

	// 显式指定 ID + grade 时保持不变
	p2 := &Person{ID: "fixed-id", Name: "李四", Grade: 5, AvatarAttachmentID: "att-1"}
	pMustCreate(t, s, p2)
	got2, err := s.PersonGet(ctx, "fixed-id")
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if got2.Grade != 5 || got2.AvatarAttachmentID != "att-1" {
		t.Fatalf("显式字段被改写: grade=%d avatar=%q", got2.Grade, got2.AvatarAttachmentID)
	}

	// 主键冲突 -> 返回错误
	if err := s.PersonCreate(ctx, &Person{ID: "fixed-id", Name: "重复"}); err == nil {
		t.Fatalf("重复主键应报错")
	}
	// not-found
	if _, err := s.PersonGet(ctx, "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("PersonGet(not found) 错误 = %v，期望 sql.ErrNoRows", err)
	}
	// DB 错误分支
	if _, err := s.PersonGet(pCancelledCtx(), "fixed-id"); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
}

// ===== PersonUpdate / PersonUpdateNameParts =====

func TestPeopleUpdateAndNameParts(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	cat := &Category{Name: "家人", Color: "#ff0000", Icon: "home", SortOrder: 1}
	pMustCategory(t, s, ctx, cat)

	p := &Person{Name: "王五", Grade: 2, Notes: "旧备注"}
	pMustCreate(t, s, p)
	createdAt := p.CreatedAt

	p.Name = "王五改"
	p.FamilyName = "王"
	p.GivenName = "五改"
	p.Nickname = "阿王"
	p.Gender = "female"
	p.Birthday = "2000-01-02"
	p.BirthdayIsLunar = true
	p.AvatarAttachmentID = "avatar-9"
	p.Phone = "13900001111"
	p.Wechat = "wx9"
	p.Location = "北京"
	p.Notes = "新备注"
	p.Grade = 4
	p.CategoryIDs = []int{cat.ID}
	p.Archived = true
	p.XAbUID = "AB-2"
	if err := s.PersonUpdate(ctx, p); err != nil {
		t.Fatalf("PersonUpdate: %v", err)
	}
	got, err := s.PersonGet(ctx, p.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if got.Name != "王五改" || got.Gender != "female" || got.Notes != "新备注" || got.Grade != 4 ||
		!got.Archived || got.XAbUID != "AB-2" || got.AvatarAttachmentID != "avatar-9" ||
		!got.BirthdayIsLunar || got.Wechat != "wx9" || got.Location != "北京" || got.Phone != "13900001111" {
		t.Fatalf("更新未生效: %+v", got)
	}
	if len(got.Categories) != 1 || got.Categories[0].ID != cat.ID || got.Categories[0].Name != "家人" {
		t.Fatalf("圈子未写入: %+v", got.Categories)
	}
	if got.CreatedAt != createdAt {
		t.Fatalf("created_at 不应被更新改写: %q -> %q", createdAt, got.CreatedAt)
	}
	if got.UpdatedAt == "" || got.UpdatedAt < createdAt {
		t.Fatalf("updated_at 应被刷新: %q", got.UpdatedAt)
	}

	// 圈子传空即清空（PUT 整行覆盖）
	got.CategoryIDs = nil
	if err := s.PersonUpdate(ctx, got); err != nil {
		t.Fatalf("PersonUpdate(clear category): %v", err)
	}
	again, _ := s.PersonGet(ctx, p.ID)
	if len(again.Categories) != 0 {
		t.Fatalf("圈子应被清空，得到 %+v", again.Categories)
	}

	// 更新不存在的 ID：影响 0 行 → ErrNoRows（API 侧翻译为 404，不能再静默成功）
	if err := s.PersonUpdate(ctx, &Person{ID: "ghost", Name: "x"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("PersonUpdate(不存在) 应 ErrNoRows: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM people WHERE id='ghost'"); n != 0 {
		t.Fatalf("不应插入新行")
	}
	if err := s.PersonUpdate(pCancelledCtx(), p); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}

	// PersonUpdateNameParts 只动姓名三件套
	src := &Person{Name: "赵六", FamilyName: "赵", GivenName: "六", Nickname: "小赵",
		Notes: "保持不变", Phone: "123", Grade: 5}
	pMustCreate(t, s, src)
	before := src.UpdatedAt
	if err := s.PersonUpdateNameParts(ctx, src.ID, "赵六新", "赵", "六新"); err != nil {
		t.Fatalf("PersonUpdateNameParts: %v", err)
	}
	up, err := s.PersonGet(ctx, src.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if up.Name != "赵六新" || up.FamilyName != "赵" || up.GivenName != "六新" {
		t.Fatalf("姓名未回填: %+v", up)
	}
	if up.Nickname != "小赵" || up.Notes != "保持不变" || up.Phone != "123" || up.Grade != 5 {
		t.Fatalf("其他字段被误改: %+v", up)
	}
	// updated_at 采用 UTC 秒级格式，同一秒内改写可能相同，只要求不倒退
	if up.UpdatedAt < before {
		t.Errorf("updated_at 不应倒退: %q -> %q", before, up.UpdatedAt)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM people WHERE id=? AND updated_at=?", src.ID, up.UpdatedAt); n != 1 {
		t.Errorf("updated_at 应被写入，得到 %q", up.UpdatedAt)
	}
	// 不存在的 ID 不报错
	if err := s.PersonUpdateNameParts(ctx, "ghost-id", "a", "b", "c"); err != nil {
		t.Fatalf("PersonUpdateNameParts(不存在) 不应报错: %v", err)
	}
	if err := s.PersonUpdateNameParts(pCancelledCtx(), src.ID, "a", "b", "c"); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
}

// ===== ComposeName / hasHan =====

func TestPeopleComposeNameTable(t *testing.T) {
	tests := []struct {
		name   string
		family string
		given  string
		want   string
	}{
		{"both 中文按姓前名后", "欧阳", "修", "欧阳修"},
		{"both 西文按名前姓后", "Smith", "John", "John Smith"},
		{"混合含汉字走姓前", "张", "Smith", "张Smith"},
		{"只有姓", "李", "", "李"},
		{"只有名", "", "Mary", "Mary"},
		{"都没有", "", "", ""},
		{"空格保留（只有姓）", "  ", "", "  "},
		{"空格保留（只有名）", "", " ", " "},
		{"西文两侧空格原样拼接", "Smith ", " John", " John" + " " + "Smith "},
		{"中文单字", "王", "芳", "王芳"},
		{"日文汉字也判为 Han 区间外?", "ｱ", "B", "B ｱ"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComposeName(tc.family, tc.given); got != tc.want {
				t.Fatalf("ComposeName(%q,%q) = %q, 期望 %q", tc.family, tc.given, got, tc.want)
			}
		})
	}

	// hasHan 边界：CJK 统一汉字区间首尾字符
	if !hasHan("\u4e00") || !hasHan("\u9fff") {
		t.Errorf("hasHan 应覆盖 0x4E00-0x9FFF 首尾")
	}
	if hasHan("") || hasHan("abc 123 ,") || hasHan("\u3042") /* 平假名不在该区间 */ {
		t.Errorf("hasHan 误判")
	}
}

// ===== PersonDelete =====

func TestPeopleDeleteAndCascade(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	p := &Person{Name: "待删", Grade: 1}
	pMustCreate(t, s, p)
	pRaw(t, s, "INSERT INTO person_fields(id,person_id,label,value,sort_order) VALUES('f1',?,?,?,1)",
		p.ID, "标签", "值")
	pRaw(t, s, "INSERT INTO anniversaries(id,person_id,title,date,created_at) VALUES('a1',?,?,?,?)",
		p.ID, "周年", "2020-01-01", "2020-01-01T00:00:00Z")

	if err := s.PersonDelete(ctx, p.ID); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}
	if _, err := s.PersonGet(ctx, p.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除后应查不到，错误 = %v", err)
	}
	// 进回收站只是藏起来：关联记录一行都不动，恢复时原样回来
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE person_id=?", p.ID); n != 1 {
		t.Fatalf("纪念日应留在表里等着恢复，剩余 %d", n)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM live_anniversaries WHERE person_id=?", p.ID); n != 0 {
		t.Fatalf("联系人还在回收站时纪念日不该可见，可见 %d", n)
	}
	// 已经在回收站里 → 再删一次 ErrNoRows
	if err := s.PersonDelete(ctx, p.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("PersonDelete(回收站里) 应 ErrNoRows: %v", err)
	}
	// 删不存在的 ID → ErrNoRows
	if err := s.PersonDelete(ctx, "never-existed"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("PersonDelete(不存在) 应 ErrNoRows: %v", err)
	}
	if err := s.PersonDelete(pCancelledCtx(), p.ID); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	// 彻底删除才由外键级联收尾
	if err := s.PersonPurge(ctx, p.ID); err != nil {
		t.Fatalf("PersonPurge: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE person_id=?", p.ID); n != 0 {
		t.Fatalf("纪念日应随人物级联删除，剩余 %d", n)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM person_fields WHERE person_id=?", p.ID); n != 0 {
		t.Fatalf("自定义字段应随人物级联删除，剩余 %d", n)
	}
}

// ===== PersonDetachAttachments =====

func TestPeopleDetachAttachments(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	p := &Person{Name: "附件主人", AvatarAttachmentID: "att-1"}
	pMustCreate(t, s, p)
	for i, id := range []string{"att-1", "att-2"} {
		pRaw(t, s, `INSERT INTO attachments(id,entity_type,entity_id,file_name,stored_name,mime,size,created_at)
VALUES(?,?,?,?,?,?,?,?)`, id, "person", p.ID, "f"+string(rune('a'+i))+".png", id+".png", "image/png", 100+i, "2024-01-0"+string(rune('0'+i))+"T00:00:00Z")
	}

	atts, err := s.PersonDetachAttachments(ctx, p.ID)
	if err != nil {
		t.Fatalf("PersonDetachAttachments: %v", err)
	}
	if len(atts) != 2 {
		t.Fatalf("应返回 2 个附件，得到 %d", len(atts))
	}
	if atts[0].ID != "att-2" || atts[1].ID != "att-1" {
		t.Fatalf("附件应按 created_at DESC 排序，得到 %s %s", atts[0].ID, atts[1].ID)
	}
	if atts[0].FileName != "fb.png" || atts[0].Mime != "image/png" || atts[0].Size != 101 {
		t.Fatalf("附件字段回读不一致: %+v", atts[0])
	}
	// 头像引用被清空（置为 NULL），磁盘文件仍在表里由 API 层处理
	if n := pRawScanInt(t, s,
		"SELECT COUNT(avatar_attachment_id) FROM people WHERE id=?", p.ID); n != 0 {
		t.Fatalf("avatar_attachment_id 应被置 NULL，COUNT=%d", n)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM attachments WHERE entity_id=?", p.ID); n != 2 {
		t.Fatalf("store 不应删除附件记录，剩余 %d", n)
	}

	// 无附件的人物：返回空切片而非 nil，且不报错
	other := &Person{Name: "无附件"}
	pMustCreate(t, s, other)
	empty, err := s.PersonDetachAttachments(ctx, other.ID)
	if err != nil {
		t.Fatalf("detach 空列表: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("期望空切片，得到 %d 项", len(empty))
	}

	if _, err := s.PersonDetachAttachments(pCancelledCtx(), p.ID); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}

	// AttachmentsOf 成功但清空头像引用失败：people 表缺失时触发 UPDATE 错误分支
	bad := newTestStore(t)
	bp := &Person{Name: "错误分支"}
	pMustCreate(t, bad, bp)
	pRaw(t, bad, `INSERT INTO attachments(id,entity_type,entity_id,file_name,stored_name,mime,size,created_at)
VALUES('x1','person',?,'a.png','a.png','image/png',1,'2024-01-01T00:00:00Z')`, bp.ID)
	pRaw(t, bad, "DROP TABLE people")
	if _, err := bad.PersonDetachAttachments(ctx, bp.ID); err == nil {
		t.Errorf("people 表缺失时 UPDATE 应报错")
	}
}

// ===== PersonDuplicates =====

func TestPeopleDuplicatesBranches(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	cat := &Category{Name: "老同学", Color: "#123456", Icon: "users", SortOrder: 2}
	pMustCategory(t, s, ctx, cat)

	same := &Person{Name: "陈明", Nickname: "小明", Phone: "13811112222", Wechat: "chenming", Grade: 4}
	pMustCreate(t, s, same)
	same.CategoryIDs = []int{cat.ID}
	if err := s.PersonUpdate(ctx, same); err != nil {
		t.Fatalf("PersonUpdate: %v", err)
	}
	// 同名不同人
	other := &Person{Name: "陈明", Phone: "999", Grade: 2}
	pMustCreate(t, s, other)
	// 同微信
	byWx := &Person{Name: "别人", Wechat: "chenming"}
	pMustCreate(t, s, byWx)
	// 空字符串列（NULL）：由原始 SQL 插入，验证 NullString 处理
	pRaw(t, s, `INSERT INTO people(id,name,nickname,gender,birthday,avatar_attachment_id,phone,wechat,location,notes,
		grade,archived,created_at,updated_at)
		VALUES('nullrow','陈明',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,3,0,?,?)`,
		"2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")

	// 全空输入 -> 空结果
	got, err := s.PersonDuplicates(ctx, "  ", " ", "\t", "")
	if err != nil {
		t.Fatalf("PersonDuplicates(空): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("全空输入不应查询，得到 %d 条", len(got))
	}

	// 按姓名（含 nickname 命中与 NULL 列容忍）
	byName, err := s.PersonDuplicates(ctx, " 陈明 ", "", "", "")
	if err != nil {
		t.Fatalf("PersonDuplicates(name): %v", err)
	}
	ids := pIDs(byName)
	for _, want := range []string{same.ID, other.ID, "nullrow"} {
		if !pContains(ids, want) {
			t.Errorf("姓名匹配缺少 %s，得到 %v", want, ids)
		}
	}
	// 同一行同时命中姓名与电话时不应重复出现：same(名+电话+微信)、other(名)、nullrow(名)、byWx(微信)
	both, err := s.PersonDuplicates(ctx, "陈明", "13811112222", "chenming", "")
	if err != nil {
		t.Fatalf("PersonDuplicates(both): %v", err)
	}
	if len(both) != 4 {
		t.Fatalf("多条件应按 OR 合并去重为 4 条，得到 %d: %v", len(both), pNames(both))
	}
	for _, p := range both {
		if p.ID == same.ID && p.Grade != 4 {
			t.Errorf("same 的 grade 回读不正确: %+v", p)
		}
		if p.Archived {
			t.Errorf("归档位应为 false")
		}
	}

	// 只按昵称命中
	byNick, err := s.PersonDuplicates(ctx, "小明", "", "", "")
	if err != nil {
		t.Fatalf("PersonDuplicates(nickname): %v", err)
	}
	if len(byNick) != 1 || byNick[0].ID != same.ID {
		t.Fatalf("昵称匹配结果 = %v", pNames(byNick))
	}
	if byNick[0].Nickname != "小明" || byNick[0].Phone != "13811112222" || byNick[0].Wechat != "chenming" {
		t.Fatalf("可空列回读不一致: %+v", byNick[0])
	}
	// NULL 行不应导致扫描失败，且空字段保持空
	var nullRow *Person
	for _, p := range byName {
		if p.ID == "nullrow" {
			nullRow = p
		}
	}
	if nullRow == nil || nullRow.Nickname != "" || nullRow.Phone != "" || nullRow.Wechat != "" ||
		nullRow.Grade != 3 {
		t.Fatalf("NULL 列处理不正确: %+v", nullRow)
	}

	// 只按电话
	byPhone, err := s.PersonDuplicates(ctx, "", " 999 ", "", "")
	if err != nil {
		t.Fatalf("按电话: %v", err)
	}
	if len(byPhone) != 1 || byPhone[0].ID != other.ID {
		t.Fatalf("电话匹配 = %v", pNames(byPhone))
	}

	// 只按微信 + excludeID
	byWxOnly, err := s.PersonDuplicates(ctx, "", "", "chenming", same.ID)
	if err != nil {
		t.Fatalf("按微信: %v", err)
	}
	if len(byWxOnly) != 1 || byWxOnly[0].ID != byWx.ID {
		t.Fatalf("排除自己后应只剩 %s，得到 %v", byWx.ID, pNames(byWxOnly))
	}

	// 无任何命中
	none, err := s.PersonDuplicates(ctx, "查无此人", "", "", "")
	if err != nil {
		t.Fatalf("无命中: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("期望空结果，得到 %d", len(none))
	}

	// LIMIT 10：造 12 个同名人物，结果应被截断且按 updated_at DESC
	for i := 0; i < 12; i++ {
		pRaw(t, s, `INSERT INTO people(id,name,grade,archived,created_at,updated_at) VALUES(?,?,1,0,?,?)`,
			"dup"+string(rune('a'+i)), "大量同名", "2020-01-01T00:00:00Z",
			time.Now().Add(-time.Duration(i)*time.Hour).Format(timeFormat))
	}
	capped, err := s.PersonDuplicates(ctx, "大量同名", "", "", "")
	if err != nil {
		t.Fatalf("limit 查询: %v", err)
	}
	if len(capped) != 10 {
		t.Fatalf("应受 LIMIT 10 约束，得到 %d", len(capped))
	}
	// updated_at 倒序：dupa 最新，dupk/dupl 应被截断
	ids = pIDs(capped)
	if !pContains(ids, "dupa") || pContains(ids, "dupk") || pContains(ids, "dupl") {
		t.Errorf("LIMIT 10 应按 updated_at 倒序截断，得到 %v", ids)
	}

	if _, err := s.PersonDuplicates(pCancelledCtx(), "陈明", "", "", ""); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
}

// ===== PersonMerge =====

func TestPeopleMergeValidation(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	pMustCreate(t, s, &Person{ID: "A", Name: "A"})

	if err := s.PersonMerge(ctx, "", "A"); err == nil {
		t.Errorf("空 sourceID 应报错")
	}
	if err := s.PersonMerge(ctx, "A", ""); err == nil {
		t.Errorf("空 targetID 应报错")
	}
	if err := s.PersonMerge(ctx, "A", "A"); err == nil {
		t.Errorf("source==target 应报错")
	}
	// 两侧都不存在：所有语句 0 行受影响，事务正常提交且不报错
	if err := s.PersonMerge(ctx, "ghost1", "ghost2"); err != nil {
		t.Errorf("合并两个不存在的 ID 不应报错: %v", err)
	}
	if err := s.PersonMerge(pCancelledCtx(), "A", "B"); err == nil {
		t.Errorf("已取消的 ctx 应让 BeginTx 失败")
	}
	// 合并后源人物仍在（前面无有效合并发生）
	if _, err := s.PersonGet(ctx, "A"); err != nil {
		t.Fatalf("校验失败不应改动数据: %v", err)
	}
}

func TestPeopleMergeMovesEverything(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	pMustCreate(t, s, &Person{ID: "src", Name: "源"})
	pMustCreate(t, s, &Person{ID: "tgt", Name: "目标"})
	pMustCreate(t, s, &Person{ID: "x", Name: "X"})
	pMustCreate(t, s, &Person{ID: "y", Name: "Y"})

	// 事件：e1 同时有 src+tgt（触发 INSERT OR IGNORE 去重），e2 只有 src
	pRaw(t, s, "INSERT INTO events(id,title,event_date,created_at,updated_at) VALUES('e1','共','2024-01-01',?,?)", nowUTC(), nowUTC())
	pRaw(t, s, "INSERT INTO events(id,title,event_date,created_at,updated_at) VALUES('e2','独','2024-01-02',?,?)", nowUTC(), nowUTC())
	pRaw(t, s, "INSERT INTO event_participants(event_id,person_id) VALUES('e1','src'),('e1','tgt'),('e2','src')")
	// 备忘 / 交易 / 纪念日 / 提醒
	pRaw(t, s, "INSERT INTO memos(id,person_id,content,said_at,created_at) VALUES('m1','src','说','2024-01-01T00:00:00',?)", nowUTC())
	pRaw(t, s, "INSERT INTO transactions(id,person_id,kind,direction,amount_fen,occurred_at,created_at) VALUES('t1','src','loan','in',100,'2024-01-01',?)", nowUTC())
	pRaw(t, s, "INSERT INTO anniversaries(id,person_id,title,date,created_at) VALUES('an1','src','纪念日','2024-01-01',?)", nowUTC())
	pRaw(t, s, "INSERT INTO reminders(id,person_id,ref_type,ref_id,title,due_at,created_at) VALUES('r1','src','memo','m1','提醒','2024-01-01T00:00:00',?)", nowUTC())
	// 标签：tgt 已有同标签 -> UPDATE OR IGNORE 冲突后由 DELETE 清理
	tag := &Tag{Name: "重要", Color: "#fff"}
	pMustTag(t, s, ctx, tag)
	pRaw(t, s, "INSERT INTO taggings(tag_id,target_type,target_id) VALUES(?,'person','src')", tag.ID)
	pRaw(t, s, "INSERT INTO taggings(tag_id,target_type,target_id) VALUES(?,'person','tgt')", tag.ID)
	// 自定义字段
	pRaw(t, s, "INSERT INTO person_fields(id,person_id,label,value,sort_order) VALUES('pf1','src','职业','老师',1)")
	// 关系：src->x 与 tgt->x 冲突；y->src 与 y->tgt 冲突；src->y 唯一
	pRaw(t, s, "INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at) VALUES('rel1','src','x','同事',?)", nowUTC())
	pRaw(t, s, "INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at) VALUES('rel2','tgt','x','同事',?)", nowUTC())
	pRaw(t, s, "INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at) VALUES('rel3','y','src','同学',?)", nowUTC())
	pRaw(t, s, "INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at) VALUES('rel4','src','y','同学',?)", nowUTC())

	if err := s.PersonMerge(ctx, "src", "tgt"); err != nil {
		t.Fatalf("PersonMerge: %v", err)
	}

	if _, err := s.PersonGet(ctx, "src"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("源人物应被删除，错误 = %v", err)
	}
	if got, err := s.PersonGet(ctx, "tgt"); err != nil || got.Name != "目标" {
		t.Fatalf("目标人物应保留: %v %+v", err, got)
	}
	checks := []struct {
		label string
		q     string
		want  int
	}{
		{"事件 e1 参与人去重", "SELECT COUNT(*) FROM event_participants WHERE event_id='e1' AND person_id='tgt'", 1},
		{"事件 e1 无残留 src", "SELECT COUNT(*) FROM event_participants WHERE event_id='e1' AND person_id='src'", 0},
		{"事件 e2 迁移到 tgt", "SELECT COUNT(*) FROM event_participants WHERE event_id='e2' AND person_id='tgt'", 1},
		{"备忘迁移", "SELECT COUNT(*) FROM memos WHERE id='m1' AND person_id='tgt'", 1},
		{"交易迁移", "SELECT COUNT(*) FROM transactions WHERE id='t1' AND person_id='tgt'", 1},
		{"纪念日迁移", "SELECT COUNT(*) FROM anniversaries WHERE id='an1' AND person_id='tgt'", 1},
		{"提醒迁移", "SELECT COUNT(*) FROM reminders WHERE id='r1' AND person_id='tgt'", 1},
		{"标签归并到 tgt", "SELECT COUNT(*) FROM taggings WHERE target_type='person' AND target_id='tgt'", 1},
		{"标签无 src 残留", "SELECT COUNT(*) FROM taggings WHERE target_type='person' AND target_id='src'", 0},
		{"自定义字段迁移", "SELECT COUNT(*) FROM person_fields WHERE id='pf1' AND person_id='tgt'", 1},
		{"冲突关系 rel1 被清理", "SELECT COUNT(*) FROM relationships WHERE id='rel1'", 0},
		{"关系 rel2 保留", "SELECT COUNT(*) FROM relationships WHERE id='rel2' AND from_person_id='tgt' AND to_person_id='x'", 1},
		{"关系 rel3 保留（src 侧清空）", "SELECT COUNT(*) FROM relationships WHERE id='rel3'", 1},
		{"src 不再出现在关系两端", "SELECT COUNT(*) FROM relationships WHERE from_person_id='src' OR to_person_id='src'", 0},
	}
	for _, c := range checks {
		if got := pRawScanInt(t, s, c.q); got != c.want {
			t.Errorf("%s: 期望 %d 得到 %d (%s)", c.label, c.want, got, c.q)
		}
	}
	// rel3 y->src 被改写成 y->tgt（tgt 无 y->tgt 时成功）
	if got := pRawScanInt(t, s, "SELECT COUNT(*) FROM relationships WHERE id='rel3' AND to_person_id='tgt'"); got != 1 {
		t.Errorf("rel3 的 to_person_id 应改为 tgt，得到 %d", got)
	}
}

// 关系边改成「一对人可并存多种类型」后，合并只该并掉同名的那条，
// 不同类型必须各自留下——旧 UNIQUE(from,to) 会把它们一起吞掉。
func TestPeopleMergeKeepsDistinctRelationTypes(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	pMustCreate(t, s, &Person{ID: "m1", Name: "甲"})
	pMustCreate(t, s, &Person{ID: "m2", Name: "乙"})
	pMustCreate(t, s, &Person{ID: "m3", Name: "丙"})

	pRaw(t, s, "INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at) VALUES('keep','m1','m3','老友',?)", nowUTC())
	pRaw(t, s, "INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at) VALUES('dup','m1','m3','同事',?)", nowUTC())
	pRaw(t, s, "INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at) VALUES('base','m2','m3','同事',?)", nowUTC())

	if err := s.PersonMerge(ctx, "m1", "m2"); err != nil {
		t.Fatalf("PersonMerge: %v", err)
	}
	if got := pRawScanInt(t, s, "SELECT COUNT(*) FROM relationships WHERE id='keep' AND from_person_id='m2' AND to_person_id='m3' AND type='老友'"); got != 1 {
		t.Errorf("不同类型的边被合并吞掉了，keep 现存 %d", got)
	}
	if got := pRawScanInt(t, s, "SELECT COUNT(*) FROM relationships WHERE id='dup'"); got != 0 {
		t.Errorf("与目标同名的边应被并掉，dup 现存 %d", got)
	}
	if got := pRawScanInt(t, s, "SELECT COUNT(*) FROM relationships WHERE from_person_id='m2' AND to_person_id='m3'"); got != 2 {
		t.Errorf("m2→m3 应剩老友+同事两条，得到 %d", got)
	}
	if got := pRawScanInt(t, s, "SELECT COUNT(*) FROM relationships WHERE from_person_id='m2' AND to_person_id='m3' AND type='同事'"); got != 1 {
		t.Errorf("同名的同事应只留一条，得到 %d", got)
	}
}

func TestPeopleMergeRollbackOnError(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	pMustCreate(t, s, &Person{ID: "s2", Name: "S2"})
	pRaw(t, s, "INSERT INTO person_fields(id,person_id,label,value,sort_order) VALUES('pf2','s2','a','b',1)")
	// target 不存在 -> person_fields 的外键约束在事务中途失败，整个合并回滚
	if err := s.PersonMerge(ctx, "s2", "不存在的目标"); err == nil {
		t.Fatalf("外键冲突应返回错误")
	}
	if _, err := s.PersonGet(ctx, "s2"); err != nil {
		t.Fatalf("失败时应回滚，源人物仍在: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM person_fields WHERE id='pf2' AND person_id='s2'"); n != 1 {
		t.Fatalf("字段应回滚未迁移，得到 %d", n)
	}
}

// 合并双方都有生日时：source 的生日纪念日不能跟着迁移，否则 target 会收到两条生日提醒；
// target 自己的关联保持有效，手动纪念日照常迁移。
func TestPeopleMergeDropsSourceBirthdayAnniversary(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	src := pMustCreate(t, s, &Person{ID: "src2", Name: "甲", Birthday: "1991-03-05"})
	tgt := pMustCreate(t, s, &Person{ID: "tgt2", Name: "乙", Birthday: "1990-01-01"})
	for _, p := range []*Person{src, tgt} {
		if err := s.SyncBirthdayAnniversary(ctx, p); err != nil {
			t.Fatalf("同步 %s 生日: %v", p.Name, err)
		}
	}
	pRaw(t, s, "INSERT INTO anniversaries(id,person_id,title,date,created_at) VALUES('an2',?,'纪念日','2024-01-01',?)", src.ID, nowUTC())

	if err := s.PersonMerge(ctx, src.ID, tgt.ID); err != nil {
		t.Fatalf("PersonMerge: %v", err)
	}

	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE source='birthday'"); n != 1 {
		t.Fatalf("生日纪念日应只剩 target 一条，得到 %d", n)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE id=?", src.BirthdayAnniversaryID); n != 0 {
		t.Fatalf("source 的生日纪念日应被删除，剩余 %d", n)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE id=? AND person_id=? AND date='1990-01-01'", tgt.BirthdayAnniversaryID, tgt.ID); n != 1 {
		t.Fatalf("target 的生日纪念日应保留并归属 target")
	}
	if got := pRawScanStr(t, s, "SELECT birthday_anniversary_id FROM people WHERE id=?", tgt.ID); got != tgt.BirthdayAnniversaryID {
		t.Fatalf("target 的关联应保持自己的纪念日，得到 %q 期望 %q", got, tgt.BirthdayAnniversaryID)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE id='an2' AND person_id=?", tgt.ID); n != 1 {
		t.Fatalf("手动纪念日应迁移到 target")
	}
}

// target 只有 birthday 却从未生成纪念日（例如直接写库的老数据）：合并后按 target 的生日补一条。
func TestPeopleMergeRebuildsTargetBirthday(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	src := pMustCreate(t, s, &Person{ID: "src3", Name: "甲", Birthday: "1991-03-05"})
	if err := s.SyncBirthdayAnniversary(ctx, src); err != nil {
		t.Fatalf("同步 source 生日: %v", err)
	}
	tgt := &Person{ID: "tgt3", Name: "乙", Birthday: "1990-01-01"}
	pMustCreate(t, s, tgt)

	if err := s.PersonMerge(ctx, src.ID, tgt.ID); err != nil {
		t.Fatalf("PersonMerge: %v", err)
	}

	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE source='birthday' AND person_id=?", tgt.ID); n != 1 {
		t.Fatalf("应补建 target 的生日纪念日，得到 %d", n)
	}
	if got := pRawScanStr(t, s, "SELECT date FROM anniversaries WHERE person_id=? AND source='birthday'", tgt.ID); got != "1990-01-01" {
		t.Fatalf("补建的日期应取 target 的生日，得到 %q", got)
	}
	if got := pRawScanStr(t, s, "SELECT IFNULL(birthday_anniversary_id,'<NULL>') FROM people WHERE id=?", tgt.ID); got == "<NULL>" {
		t.Fatalf("应回写 target 的 birthday_anniversary_id")
	}
	// target 无生日：source 的生日纪念日直接消失，不留幽灵提醒
	plain := pMustCreate(t, s, &Person{ID: "tgt4", Name: "丙"})
	src4 := pMustCreate(t, s, &Person{ID: "src4", Name: "丁", Birthday: "1992-02-02"})
	if err := s.SyncBirthdayAnniversary(ctx, src4); err != nil {
		t.Fatalf("同步 source4 生日: %v", err)
	}
	if err := s.PersonMerge(ctx, src4.ID, plain.ID); err != nil {
		t.Fatalf("PersonMerge: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE id=?", src4.BirthdayAnniversaryID); n != 0 {
		t.Fatalf("target 无生日时 source 的生日纪念日应被删除，剩余 %d", n)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE person_id=?", plain.ID); n != 0 {
		t.Fatalf("target 不应留下任何纪念日，得到 %d", n)
	}
}

// ===== SyncBirthdayAnniversary =====

func TestPeopleSyncBirthdayAnniversary(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	// nil / 空 ID 直接返回
	if err := s.SyncBirthdayAnniversary(ctx, nil); err != nil {
		t.Fatalf("nil person: %v", err)
	}
	if err := s.SyncBirthdayAnniversary(ctx, &Person{}); err != nil {
		t.Fatalf("空 ID: %v", err)
	}

	p := &Person{Name: "生日人", Birthday: "1991-03-04", BirthdayIsLunar: true}
	pMustCreate(t, s, p)

	// 首次：新建 source='birthday' 的纪念日并回写关联 ID
	if err := s.SyncBirthdayAnniversary(ctx, p); err != nil {
		t.Fatalf("首次同步: %v", err)
	}
	if p.BirthdayAnniversaryID == "" {
		t.Fatalf("应回填 BirthdayAnniversaryID")
	}
	row := struct{ id, title, date, remind, src string }{}
	if err := s.DB.QueryRowContext(ctx,
		"SELECT id,title,date,remind_days,source FROM anniversaries WHERE id=?", p.BirthdayAnniversaryID).
		Scan(&row.id, &row.title, &row.date, &row.remind, &row.src); err != nil {
		t.Fatalf("查询纪念日: %v", err)
	}
	if row.title != "生日人的生日" || row.date != "1991-03-04" || row.src != "birthday" || row.remind != "7,3,1,0" {
		t.Fatalf("纪念日内容不正确: %+v", row)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE repeat_yearly=1 AND id=?", p.BirthdayAnniversaryID); n != 1 {
		t.Fatalf("repeat_yearly 应为 1")
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE is_lunar=1 AND id=?", p.BirthdayAnniversaryID); n != 1 {
		t.Fatalf("is_lunar 应跟随人物设置")
	}
	if got := pRawScanStr(t, s, "SELECT birthday_anniversary_id FROM people WHERE id=?", p.ID); got != p.BirthdayAnniversaryID {
		t.Fatalf("people.birthday_anniversary_id = %q，期望 %q", got, p.BirthdayAnniversaryID)
	}

	// 再次同步（改生日）：走 UPDATE 分支，不新建
	p.Birthday = "1991-03-05"
	if err := s.SyncBirthdayAnniversary(ctx, p); err != nil {
		t.Fatalf("更新同步: %v", err)
	}
	if p.BirthdayAnniversaryID != row.id {
		t.Fatalf("不应新建纪念日: %q -> %q", row.id, p.BirthdayAnniversaryID)
	}
	if got := pRawScanStr(t, s, "SELECT date FROM anniversaries WHERE id=?", row.id); got != "1991-03-05" {
		t.Fatalf("生日未更新: %q", got)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE person_id=?", p.ID); n != 1 {
		t.Fatalf("应只有一条生日纪念日，得到 %d", n)
	}

	// 场景：库里已有 birthday 纪念日但内存对象没记录 ID -> 复用而非新建
	reloaded := &Person{ID: p.ID, Name: p.Name, Birthday: "1991-03-06"}
	if err := s.SyncBirthdayAnniversary(ctx, reloaded); err != nil {
		t.Fatalf("复用已有纪念日: %v", err)
	}
	if reloaded.BirthdayAnniversaryID != row.id {
		t.Fatalf("应复用已存在的纪念日 ID，得到 %q 期望 %q", reloaded.BirthdayAnniversaryID, row.id)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE person_id=?", p.ID); n != 1 {
		t.Fatalf("复用后仍应只有一条，得到 %d", n)
	}

	// 生日清空：删除自动生成的纪念日并置空关联
	p.Birthday = "   "
	if err := s.SyncBirthdayAnniversary(ctx, p); err != nil {
		t.Fatalf("清空生日: %v", err)
	}
	if p.BirthdayAnniversaryID != "" {
		t.Fatalf("应清空内存中的关联 ID，得到 %q", p.BirthdayAnniversaryID)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE id=?", row.id); n != 0 {
		t.Fatalf("生日纪念日应被删除，剩余 %d", n)
	}
	if got := pRawScanStr(t, s, "SELECT IFNULL(birthday_anniversary_id,'<NULL>') FROM people WHERE id=?", p.ID); got != "<NULL>" {
		t.Fatalf("关联字段应置 NULL，得到 %q", got)
	}

	// 手动纪念日（source=''）：有生日时被改写复用，无生日时不应被生日逻辑删除
	manual := &Person{Name: "手动党", Birthday: "2000-12-31"}
	pMustCreate(t, s, manual)
	pRaw(t, s, "INSERT INTO anniversaries(id,person_id,title,date,created_at,source) VALUES('keep',?,'手动','2020-01-01',?,'')",
		manual.ID, nowUTC())
	manual.BirthdayAnniversaryID = "keep"
	if err := s.SyncBirthdayAnniversary(ctx, manual); err != nil {
		t.Fatalf("带手动纪念日同步: %v", err)
	}
	if got := pRawScanStr(t, s, "SELECT title FROM anniversaries WHERE id='keep'"); got != "手动党的生日" {
		t.Fatalf("已登记的关联纪念日应被 UPDATE 复用，标题得到 %q", got)
	}
	if got := pRawScanStr(t, s, "SELECT date||'|'||source FROM anniversaries WHERE id='keep'"); got != "2000-12-31|" {
		t.Fatalf("日期应跟随生日且 source 不变，得到 %q", got)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE person_id=?", manual.ID); n != 1 {
		t.Fatalf("复用不应新增记录，得到 %d", n)
	}
	if manual.BirthdayAnniversaryID != "keep" {
		t.Fatalf("关联 ID 应保持不变，得到 %q", manual.BirthdayAnniversaryID)
	}

	// 无生日：source='' 的手动纪念日必须保留（DELETE 带 source='birthday' 限定）
	noBday := &Person{Name: "删不掉", Birthday: "  "}
	pMustCreate(t, s, noBday)
	pRaw(t, s, "INSERT INTO anniversaries(id,person_id,title,date,created_at,source) VALUES('keep2',?,'手动2','2020-01-01',?,'')",
		noBday.ID, nowUTC())
	noBday.BirthdayAnniversaryID = "keep2"
	if err := s.SyncBirthdayAnniversary(ctx, noBday); err != nil {
		t.Fatalf("无生日清理: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE id='keep2'"); n != 1 {
		t.Fatalf("非 birthday 来源的纪念日不应被删除")
	}
	if noBday.BirthdayAnniversaryID != "" {
		t.Fatalf("内存关联 ID 应被清空，得到 %q", noBday.BirthdayAnniversaryID)
	}
	if got := pRawScanStr(t, s, "SELECT IFNULL(birthday_anniversary_id,'<NULL>') FROM people WHERE id=?", noBday.ID); got != "<NULL>" {
		t.Fatalf("关联字段应置 NULL，得到 %q", got)
	}
	// 回读：PersonGet 应带出关联纪念日 ID
	withAnniv := &Person{Name: "可回读", Birthday: "1988-08-08"}
	pMustCreate(t, s, withAnniv)
	if err := s.SyncBirthdayAnniversary(ctx, withAnniv); err != nil {
		t.Fatalf("同步: %v", err)
	}
	roundTrip, err := s.PersonGet(ctx, withAnniv.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if roundTrip.BirthdayAnniversaryID != withAnniv.BirthdayAnniversaryID || roundTrip.Birthday != "1988-08-08" {
		t.Fatalf("PersonGet 应带出 birthday_anniversary_id: %+v", roundTrip)
	}
	without := pMustCreate(t, s, &Person{Name: "无关联"})
	gotNo, _ := s.PersonGet(ctx, without.ID)
	if gotNo.BirthdayAnniversaryID != "" {
		t.Fatalf("NULL 纪念日关联应读成空串，得到 %q", gotNo.BirthdayAnniversaryID)
	}

	// 无生日且无关联 ID：只置空，不报错
	plain := &Person{Name: "无生日"}
	pMustCreate(t, s, plain)
	if err := s.SyncBirthdayAnniversary(ctx, plain); err != nil {
		t.Fatalf("无生日: %v", err)
	}
	if plain.BirthdayAnniversaryID != "" {
		t.Fatalf("应保持为空")
	}

	// DB 错误分支
	if err := s.SyncBirthdayAnniversary(pCancelledCtx(), &Person{ID: plain.ID, Birthday: "2020-01-01"}); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.SyncBirthdayAnniversary(pCancelledCtx(), &Person{ID: plain.ID}); err == nil {
		t.Errorf("已取消的 ctx（清空生日分支）应返回错误")
	}
}

// ===== PersonList =====

func TestPeopleListFiltersAndSort(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	cat := &Category{Name: "球友", Color: "#0f0", Icon: "ball", SortOrder: 3}
	pMustCategory(t, s, ctx, cat)
	catID := cat.ID

	// grade 5（圈子+标签）、grade 3、grade 1、archived
	high := &Person{Name: "高强", Nickname: "阿强", Notes: "篮球搭子", Grade: 5, CategoryIDs: []int{catID}}
	pMustCreate(t, s, high)
	mid := &Person{Name: "中敏", Phone: "666", Grade: 3}
	pMustCreate(t, s, mid)
	low := &Person{Name: "低伟", Notes: "偶尔联系", Grade: 1}
	pMustCreate(t, s, low)
	old := &Person{Name: "归档人", Grade: 4, Archived: true}
	pMustCreate(t, s, old)

	tag := &Tag{Name: "常联系", Color: "#eee"}
	pMustTag(t, s, ctx, tag)
	if err := s.TagAdd(ctx, "person", high.ID, tag.ID); err != nil {
		t.Fatalf("TagAdd: %v", err)
	}

	// 固定 updated_at 以便验证排序
	pRaw(t, s, "UPDATE people SET updated_at=? WHERE id=?", "2024-01-03T00:00:00Z", high.ID)
	pRaw(t, s, "UPDATE people SET updated_at=? WHERE id=?", "2024-01-05T00:00:00Z", mid.ID)
	pRaw(t, s, "UPDATE people SET updated_at=? WHERE id=?", "2024-01-04T00:00:00Z", low.ID)
	pRaw(t, s, "UPDATE people SET updated_at=? WHERE id=?", "2024-01-06T00:00:00Z", old.ID)

	// 默认：排除归档，grade DESC, updated_at DESC
	all, err := s.PersonList(ctx, "", 0, 0, false, 0, 0, 0)
	if err != nil {
		t.Fatalf("PersonList: %v", err)
	}
	if got := pNames(all); strings.Join(got, ",") != "高强,中敏,低伟" {
		t.Fatalf("默认排序/过滤不正确: %v", got)
	}
	if len(all[0].Categories) != 1 || all[0].Categories[0].ID != catID {
		t.Errorf("应带出圈子: %+v", all[0].Categories)
	} else if all[0].Categories[0].Name != "球友" || all[0].Categories[0].Color != "#0f0" {
		t.Errorf("圈子应带出名字与颜色: %+v", all[0].Categories[0])
	}
	if len(all[1].Categories) != 0 {
		t.Errorf("无圈子应为空: %+v", all[1])
	}
	if all[0].Grade != 5 || all[0].Nickname != "阿强" {
		t.Errorf("字段回读不正确: %+v", all[0])
	}

	// 关键字：name / nickname / notes 三列
	for _, kw := range []struct{ q, want string }{
		{"强", "高强"}, {"阿强", "高强"}, {"篮球", "高强"}, {"偶尔", "低伟"},
	} {
		got, err := s.PersonList(ctx, kw.q, 0, 0, false, 0, 10, 0)
		if err != nil {
			t.Fatalf("PersonList(%q): %v", kw.q, err)
		}
		if len(got) != 1 || got[0].Name != kw.want {
			t.Fatalf("关键字 %q 应只命中 %s，得到 %v", kw.q, kw.want, pNames(got))
		}
	}
	if got, _ := s.PersonList(ctx, "不存在", 0, 0, false, 0, 10, 0); len(got) != 0 {
		t.Fatalf("无匹配应返回空切片，得到 %d", len(got))
	}

	// grade 过滤
	g, err := s.PersonList(ctx, "", 0, 3, false, 0, 0, 0)
	if err != nil {
		t.Fatalf("grade 过滤: %v", err)
	}
	if len(g) != 1 || g[0].Name != "中敏" {
		t.Fatalf("grade=3 结果 = %v", pNames(g))
	}

	// category 过滤
	c, err := s.PersonList(ctx, "", catID, 0, false, 0, 0, 0)
	if err != nil {
		t.Fatalf("category 过滤: %v", err)
	}
	if len(c) != 1 || c[0].Name != "高强" {
		t.Fatalf("category 结果 = %v", pNames(c))
	}

	// tag 过滤
	tg, err := s.PersonList(ctx, "", 0, 0, false, tag.ID, 0, 0)
	if err != nil {
		t.Fatalf("tag 过滤: %v", err)
	}
	if len(tg) != 1 || tg[0].ID != high.ID {
		t.Fatalf("tag 结果 = %v", pNames(tg))
	}
	miss, err := s.PersonList(ctx, "", 0, 0, false, 9999, 0, 0)
	if err != nil {
		t.Fatalf("未知 tag: %v", err)
	}
	if len(miss) != 0 {
		t.Fatalf("未知标签应无结果，得到 %d", len(miss))
	}

	// archived=true 时不加归档条件，返回全部（含归档）
	withArch, err := s.PersonList(ctx, "", 0, 0, true, 0, 0, 0)
	if err != nil {
		t.Fatalf("archived: %v", err)
	}
	if len(withArch) != 4 {
		t.Fatalf("archived=true 应含归档，得到 %v", pNames(withArch))
	}
	// grade DESC 优先于 updated_at：高强(5) > 归档人(4) > 中敏(3) > 低伟(1)
	if got := pNames(withArch); strings.Join(got, ",") != "高强,归档人,中敏,低伟" {
		t.Fatalf("archived=true 应含归档且按 grade DESC 排序，得到 %v", got)
	}
	if !withArch[1].Archived {
		t.Errorf("归档人 Archived 应为 true: %+v", withArch[1])
	}

	// 组合过滤 + 分页
	combined, err := s.PersonList(ctx, "强", catID, 5, false, tag.ID, 50, 0)
	if err != nil {
		t.Fatalf("组合过滤: %v", err)
	}
	if len(combined) != 1 || combined[0].ID != high.ID {
		t.Fatalf("组合过滤结果 = %v", pNames(combined))
	}
	page1, err := s.PersonList(ctx, "", 0, 0, false, 0, 2, 0)
	if err != nil {
		t.Fatalf("分页 1: %v", err)
	}
	page2, err := s.PersonList(ctx, "", 0, 0, false, 0, 2, 2)
	if err != nil {
		t.Fatalf("分页 2: %v", err)
	}
	if len(page1) != 2 || len(page2) != 1 || page2[0].Name != "低伟" {
		t.Fatalf("分页结果不正确: %v / %v", pNames(page1), pNames(page2))
	}
	// offset 越界
	off, err := s.PersonList(ctx, "", 0, 0, false, 0, 10, 99)
	if err != nil {
		t.Fatalf("越界 offset: %v", err)
	}
	if len(off) != 0 {
		t.Fatalf("越界 offset 应为空，得到 %d", len(off))
	}

	// DB 错误分支
	if _, err := s.PersonList(pCancelledCtx(), "", 0, 0, false, 0, 0, 0); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
}

func TestPeopleListNullColumnsFromRawRow(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	// 正常创建一条 + 归档过滤为 false 时不出现
	pMustCreate(t, s, &Person{ID: "ok", Name: "正常", Grade: 2})
	list, err := s.PersonList(ctx, "正常", 0, 0, false, 0, 50, 0)
	if err != nil {
		t.Fatalf("PersonList: %v", err)
	}
	if len(list) != 1 || list[0].ID != "ok" || list[0].FamilyName != "" {
		t.Fatalf("结果不正确: %+v", list)
	}
	if list[0].BirthdayAnniversaryID != "" {
		t.Errorf("PersonList 不返回纪念日 ID，应保持为空")
	}
}

func TestPeopleListDefaultLimitIsFifty(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	for i := 0; i < 55; i++ {
		pMustCreate(t, s, &Person{ID: fmt.Sprintf("bulk-%02d", i), Name: fmt.Sprintf("批量%02d", i), Grade: 1})
		pRaw(t, s, "UPDATE people SET updated_at=? WHERE id=?",
			fmt.Sprintf("2024-01-%02dT00:00:00Z", i%28+1), fmt.Sprintf("bulk-%02d", i))
	}
	// limit<=0 时走默认 50
	got, err := s.PersonList(ctx, "", 0, 0, false, 0, 0, 0)
	if err != nil {
		t.Fatalf("PersonList: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("limit=0 应回落为 50，得到 %d", len(got))
	}
	// 负数 limit 同样回落
	got2, err := s.PersonList(ctx, "", 0, 0, false, 0, -5, 0)
	if err != nil {
		t.Fatalf("PersonList(负 limit): %v", err)
	}
	if len(got2) != 50 {
		t.Fatalf("limit=-5 应回落为 50，得到 %d", len(got2))
	}
	// 同 grade 时按 updated_at DESC
	for i := 1; i < len(got); i++ {
		if got[i-1].UpdatedAt < got[i].UpdatedAt {
			t.Fatalf("同 grade 应按 updated_at 倒序: %q < %q", got[i-1].UpdatedAt, got[i].UpdatedAt)
		}
	}
}

// 姓/名拆分与显示名拼装往返（vCard 场景）
func TestPeopleNameSplitRoundTrip(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	// 中文联系人：N:;欧阳;; -> 姓「欧阳」名「修」
	cn := &Person{ID: "cn", Name: ComposeName("欧阳", "修"), FamilyName: "欧阳", GivenName: "修", XAbUID: "uid-cn"}
	pMustCreate(t, s, cn)
	got, err := s.PersonGet(ctx, "cn")
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if got.Name != "欧阳修" || got.FamilyName != "欧阳" || got.GivenName != "修" {
		t.Fatalf("中文姓名拆分存储不正确: %+v", got)
	}
	// 重导入时回填新的姓名三件套，显示名与 ComposeName 保持一致
	if err := s.PersonUpdateNameParts(ctx, "cn", ComposeName("欧", "阳修"), "欧", "阳修"); err != nil {
		t.Fatalf("回填: %v", err)
	}
	again, _ := s.PersonGet(ctx, "cn")
	if again.FamilyName != "欧" || again.GivenName != "阳修" || again.Name != "欧阳修" {
		t.Fatalf("回填后的姓名三件套不正确: %+v", again)
	}
	if again.Name != ComposeName(again.FamilyName, again.GivenName) {
		t.Fatalf("显示名应始终等于 ComposeName(姓,名): %q vs %q", again.Name, ComposeName(again.FamilyName, again.GivenName))
	}
	if again.XAbUID != "uid-cn" {
		t.Fatalf("回填不应触碰 X-ABUID，得到 %q", again.XAbUID)
	}

	// 西文联系人：名在前
	en := &Person{ID: "en", FamilyName: "Smith", GivenName: "John"}
	en.Name = ComposeName(en.FamilyName, en.GivenName)
	pMustCreate(t, s, en)
	list, err := s.PersonList(ctx, "Smith", 0, 0, false, 0, 10, 0)
	if err != nil {
		t.Fatalf("PersonList: %v", err)
	}
	if len(list) != 1 || list[0].Name != "John Smith" || list[0].FamilyName != "Smith" || list[0].GivenName != "John" {
		t.Fatalf("列表中的西文姓名不正确: %+v", list)
	}
}

// ===== PersonCount =====

func TestPeopleCount(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	n, err := s.PersonCount(ctx)
	if err != nil {
		t.Fatalf("空库 PersonCount: %v", err)
	}
	if n != 0 {
		t.Fatalf("空库应为 0，得到 %d", n)
	}
	pMustCreate(t, s, &Person{ID: "c1", Name: "一"})
	pMustCreate(t, s, &Person{ID: "c2", Name: "二"})
	pMustCreate(t, s, &Person{ID: "c3", Name: "三", Archived: true})
	if n, _ := s.PersonCount(ctx); n != 2 {
		t.Fatalf("应排除归档，得到 %d", n)
	}
	if _, err := s.PersonCount(pCancelledCtx()); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
}

// ===== PersonFields =====

func TestPeopleFieldUpsertListDelete(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	p := pMustCreate(t, s, &Person{ID: "pf-person", Name: "字段人"})

	list, err := s.PersonFieldList(ctx, p.ID)
	if err != nil {
		t.Fatalf("空列表: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("应为空，得到 %d", len(list))
	}

	f := &PersonField{PersonID: p.ID, Label: "职业", Value: "医生", SortOrder: 2}
	if err := s.PersonFieldUpsert(ctx, f); err != nil {
		t.Fatalf("Upsert 新建: %v", err)
	}
	if f.ID == "" {
		t.Fatalf("应自动生成 ID")
	}
	fixed := &PersonField{ID: "f-fixed", PersonID: p.ID, Label: "城市", Value: "上海", SortOrder: 1}
	if err := s.PersonFieldUpsert(ctx, fixed); err != nil {
		t.Fatalf("Upsert 第二条: %v", err)
	}
	// 排在最前的同 sort_order 时按 label 排序
	third := &PersonField{ID: "f-a", PersonID: p.ID, Label: "A标签", Value: "x", SortOrder: 1}
	if err := s.PersonFieldUpsert(ctx, third); err != nil {
		t.Fatalf("Upsert 第三条: %v", err)
	}

	got, err := s.PersonFieldList(ctx, p.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if labels := pFieldLabels(got); strings.Join(labels, ",") != "A标签,城市,职业" {
		t.Fatalf("应按 sort_order,label 排序，得到 %v", labels)
	}
	if got[2].Value != "医生" || got[2].SortOrder != 2 || got[2].PersonID != p.ID {
		t.Fatalf("字段内容不正确: %+v", got[2])
	}

	// 同 ID 再次 upsert -> 走 DO UPDATE 分支
	if err := s.PersonFieldUpsert(ctx, &PersonField{ID: got[2].ID, PersonID: p.ID, Label: "职称", Value: "主任", SortOrder: 5}); err != nil {
		t.Fatalf("Upsert 更新: %v", err)
	}
	after, _ := s.PersonFieldList(ctx, p.ID)
	if len(after) != 3 {
		t.Fatalf("更新不应新增行，得到 %d 行", len(after))
	}
	var found *PersonField
	for _, x := range after {
		if x.ID == got[2].ID {
			found = x
		}
	}
	if found == nil || found.Label != "职称" || found.Value != "主任" || found.SortOrder != 5 {
		t.Fatalf("更新未生效: %+v", found)
	}

	// 删除
	if err := s.PersonFieldDelete(ctx, "f-a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	left, _ := s.PersonFieldList(ctx, p.ID)
	if len(left) != 2 {
		t.Fatalf("删除后应剩 2 条，得到 %d", len(left))
	}
	if err := s.PersonFieldDelete(ctx, "不存在"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除不存在应 ErrNoRows: %v", err)
	}

	// 外键约束：未知 person_id 应报错
	if err := s.PersonFieldUpsert(ctx, &PersonField{PersonID: "ghost", Label: "x"}); err == nil {
		t.Errorf("未知 person_id 应因外键约束报错")
	}
	// DB 错误分支
	if err := s.PersonFieldUpsert(pCancelledCtx(), fixed); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if _, err := s.PersonFieldList(pCancelledCtx(), p.ID); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.PersonFieldDelete(pCancelledCtx(), "f-fixed"); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if _, err := s.PersonFieldList(ctx, "从未有过字段的人"); err != nil {
		t.Errorf("未知人物应返回空列表: %v", err)
	}
}

// ===== Categories =====

func TestPeopleCategoryCrudAndDeleteClearsPeople(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	empty, err := s.CategoryList(ctx)
	if err != nil {
		t.Fatalf("空列表: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("应为空，得到 %d", len(empty))
	}

	c1 := &Category{Name: "同事", Color: "#aaa", Icon: "briefcase", SortOrder: 2}
	pMustCategory(t, s, ctx, c1)
	if c1.ID == 0 {
		t.Fatalf("新建应回填自增 ID")
	}
	c2 := &Category{Name: "家人", Color: "#bbb", Icon: "home", SortOrder: 1}
	pMustCategory(t, s, ctx, c2)
	// 更新分支
	c2.Name = "亲人"
	c2.Color = "#ccc"
	c2.Icon = "heart"
	c2.SortOrder = 9
	if err := s.CategoryUpsert(ctx, c2); err != nil {
		t.Fatalf("更新: %v", err)
	}
	list, err := s.CategoryList(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("更新不应新增，得到 %d", len(list))
	}
	// sort_order 优先，其次 name
	if list[0].Name != "同事" || list[0].ID != c1.ID {
		t.Fatalf("排序不正确: %+v %+v", list[0], list[1])
	}
	if list[1].ID != c2.ID || list[1].Name != "亲人" || list[1].Color != "#ccc" ||
		list[1].Icon != "heart" || list[1].SortOrder != 9 {
		t.Fatalf("更新未持久化: %+v", list[1])
	}

	// 删除圈子应连带解除成员关系（ON DELETE CASCADE）
	p := pMustCreate(t, s, &Person{ID: "cat-person", Name: "有圈子", CategoryIDs: []int{c1.ID}})
	if err := s.CategoryDelete(ctx, c1.ID); err != nil {
		t.Fatalf("CategoryDelete: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM categories WHERE id=?", c1.ID); n != 0 {
		t.Fatalf("圈子未删除")
	}
	after, err := s.PersonGet(ctx, p.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if len(after.Categories) != 0 {
		t.Fatalf("圈子删除后人物不应再挂该圈子: %+v", after.Categories)
	}
	if err := s.CategoryDelete(ctx, 987654); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除不存在的圈子应 ErrNoRows: %v", err)
	}

	if _, err := s.CategoryList(pCancelledCtx()); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.CategoryUpsert(pCancelledCtx(), &Category{Name: "x"}); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.CategoryUpsert(pCancelledCtx(), c2); err == nil {
		t.Errorf("已取消的 ctx（更新分支）应返回错误")
	}
	if err := s.CategoryDelete(pCancelledCtx(), c2.ID); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
}

// ===== Tags / Tagging =====

func TestPeopleTagCrudAndTagging(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	if list, err := s.TagList(ctx); err != nil || len(list) != 0 {
		t.Fatalf("空标签列表 = %v / %v", list, err)
	}
	t1 := &Tag{Name: "zeta", Color: "#111"}
	pMustTag(t, s, ctx, t1)
	if t1.ID == 0 {
		t.Fatalf("应回填 ID")
	}
	t2 := &Tag{Name: "alpha", Color: "#222"}
	pMustTag(t, s, ctx, t2)
	if err := s.TagUpsert(ctx, t2); err != nil { // 同 ID -> 更新分支
		t.Fatalf("更新: %v", err)
	}
	t2.Name = "beta"
	t2.Color = "#333"
	if err := s.TagUpsert(ctx, t2); err != nil {
		t.Fatalf("更新 2: %v", err)
	}
	list, err := s.TagList(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if names := pTagNames(list); strings.Join(names, ",") != "beta,zeta" {
		t.Fatalf("应按名称排序，得到 %v", names)
	}

	// 唯一约束：重名插入应报错
	if err := s.TagUpsert(ctx, &Tag{Name: "beta", Color: "#000"}); err == nil {
		t.Errorf("标签名唯一约束应报错")
	}

	// tagging：INSERT OR IGNORE 幂等
	p := pMustCreate(t, s, &Person{ID: "tagged", Name: "被标签"})
	if err := s.TagAdd(ctx, "person", p.ID, t1.ID); err != nil {
		t.Fatalf("TagAdd: %v", err)
	}
	if err := s.TagAdd(ctx, "person", p.ID, t1.ID); err != nil {
		t.Fatalf("重复 TagAdd 应被忽略: %v", err)
	}
	if err := s.TagAdd(ctx, "person", p.ID, t2.ID); err != nil {
		t.Fatalf("TagAdd 2: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM taggings WHERE target_id='tagged'"); n != 2 {
		t.Fatalf("taggings 应为 2 条，得到 %d", n)
	}
	of, err := s.TagsOf(ctx, "person", p.ID)
	if err != nil {
		t.Fatalf("TagsOf: %v", err)
	}
	if got := pTagNames(of); strings.Join(got, ",") != "beta,zeta" {
		t.Fatalf("TagsOf 排序/内容不正确: %v", got)
	}
	if of[0].Color != "#333" {
		t.Fatalf("更新后的颜色应生效: %+v", of[0])
	}
	// 未打标签的实体类型
	none, err := s.TagsOf(ctx, "event", p.ID)
	if err != nil || len(none) != 0 {
		t.Fatalf("未打标应空: %v / %v", none, err)
	}

	if err := s.TagRemove(ctx, "person", p.ID, t1.ID); err != nil {
		t.Fatalf("TagRemove: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM taggings WHERE tag_id=?", t1.ID); n != 0 {
		t.Fatalf("TagRemove 未生效")
	}
	if err := s.TagRemove(ctx, "person", p.ID, 999999); err != nil {
		t.Fatalf("移除不存在不应报错: %v", err)
	}

	// TagDelete 连带清理 taggings
	if err := s.TagAdd(ctx, "person", p.ID, t1.ID); err != nil {
		t.Fatalf("TagAdd 复原: %v", err)
	}
	if err := s.TagDelete(ctx, t1.ID); err != nil {
		t.Fatalf("TagDelete: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM taggings WHERE tag_id=?", t1.ID); n != 0 {
		t.Fatalf("TagDelete 应清掉 taggings，剩余 %d", n)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM tags WHERE id=?", t1.ID); n != 0 {
		t.Fatalf("标签本体未删除")
	}
	if err := s.TagDelete(ctx, 987654); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除不存在标签应 ErrNoRows: %v", err)
	}

	if _, err := s.TagList(pCancelledCtx()); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.TagUpsert(pCancelledCtx(), &Tag{Name: "x"}); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.TagUpsert(pCancelledCtx(), t2); err == nil {
		t.Errorf("已取消的 ctx（更新分支）应返回错误")
	}
	if err := s.TagAdd(pCancelledCtx(), "person", p.ID, t2.ID); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.TagRemove(pCancelledCtx(), "person", p.ID, t2.ID); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if _, err := s.TagsOf(pCancelledCtx(), "person", p.ID); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.TagDelete(pCancelledCtx(), t2.ID); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
}

// ===== EventTypes =====

func TestPeopleEventTypeCrud(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	if list, err := s.EventTypeList(ctx); err != nil || len(list) != 0 {
		t.Fatalf("空列表 = %v / %v", list, err)
	}
	e1 := &EventType{Name: "聚会", Color: "#123", Icon: "users", IsDefault: true, SortOrder: 2}
	pMustEventType(t, s, ctx, e1)
	if e1.ID == 0 {
		t.Fatalf("应回填 ID")
	}
	e2 := &EventType{Name: "吃饭", Color: "#456", Icon: "food", SortOrder: 1}
	pMustEventType(t, s, ctx, e2)
	e2.Name = "聚餐"
	e2.IsDefault = true
	e2.SortOrder = 5
	if err := s.EventTypeUpsert(ctx, e2); err != nil {
		t.Fatalf("更新: %v", err)
	}
	list, err := s.EventTypeList(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("更新不应新增，得到 %d", len(list))
	}
	if list[0].Name != "聚会" || list[1].Name != "聚餐" {
		t.Fatalf("排序不正确: %v %v", list[0].Name, list[1].Name)
	}
	if !list[1].IsDefault || list[1].Color != "#456" || list[1].Icon != "food" || list[1].SortOrder != 5 {
		t.Fatalf("更新未持久化: %+v", list[1])
	}

	// 删除事件类型时把 events.type_id 置空
	pRaw(t, s, "INSERT INTO events(id,title,type_id,event_date,created_at,updated_at) VALUES('ev1','事件',?,'2024-01-01',?,?)",
		e1.ID, nowUTC(), nowUTC())
	if err := s.EventTypeDelete(ctx, e1.ID); err != nil {
		t.Fatalf("EventTypeDelete: %v", err)
	}
	if got := pRawScanStr(t, s, "SELECT IFNULL(type_id,'<NULL>') FROM events WHERE id='ev1'"); got != "<NULL>" {
		t.Fatalf("events.type_id 应置空，得到 %q", got)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM event_types WHERE id=?", e1.ID); n != 0 {
		t.Fatalf("类型未删除")
	}
	if err := s.EventTypeDelete(ctx, 999999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除不存在应 ErrNoRows: %v", err)
	}

	if _, err := s.EventTypeList(pCancelledCtx()); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.EventTypeUpsert(pCancelledCtx(), &EventType{Name: "x"}); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
	if err := s.EventTypeUpsert(pCancelledCtx(), e2); err == nil {
		t.Errorf("已取消的 ctx（更新分支）应返回错误")
	}
	if err := s.EventTypeDelete(pCancelledCtx(), e2.ID); err == nil {
		t.Errorf("已取消的 ctx 应返回错误")
	}
}

// ===== calcIntimacyScore =====

func TestPeopleCalcIntimacyScore(t *testing.T) {
	// 公式：grade*8 + (events*10+memos*4+tx*3) * exp(-days/60)，结果夹在 [0,100]
	tests := []struct {
		name                           string
		grade, events, memos, tx, days int
		want                           int
	}{
		{"只按等级", 5, 0, 0, 0, 0, 40},
		{"零等级零活跃", 0, 0, 0, 0, 0, 0},
		{"近期活跃全部计入", 3, 2, 1, 1, 0, 51},
		{"60 天衰减到 1/e", 3, 2, 1, 1, 60, 33},
		{"很久没联系只剩基础分", 3, 0, 0, 0, 365, 24},
		{"封顶 100", 5, 20, 10, 10, 0, 100},
		{"高等级但久不联系仍封顶", 100, 0, 0, 0, 100000, 100},
		{"负分不跌破 0", -10, 0, 0, 0, 0, 0},
		{"负数活跃也拉低到 0 以下", -1, -5, -5, -5, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := calcIntimacyScore(tc.grade, tc.events, tc.memos, tc.tx, tc.days)
			if got != tc.want {
				t.Fatalf("calcIntimacyScore(%d,%d,%d,%d,%d) = %d，期望 %d",
					tc.grade, tc.events, tc.memos, tc.tx, tc.days, got, tc.want)
			}
		})
	}
}

// ===== NULL 列导致的扫描错误 =====

// 这些用例记录了一个真实约束：people/categories/tags/event_types 里的可空 TEXT 列
// 若为 NULL，扁平结构体（非 sql.NullString）会扫描失败。生产写入路径总是写空串，
// 只有外部/原始 SQL 写入才可能踩到，这里把行为固定下来。
func TestPeopleListScanErrorOnNullColumns(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	pRaw(t, s, `INSERT INTO people(id,name,nickname,grade,archived,created_at,updated_at)
VALUES('bad-person','坏数据',NULL,1,0,?,?)`, "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")

	if _, err := s.PersonList(ctx, "坏数据", 0, 0, false, 0, 10, 0); err == nil {
		t.Errorf("nickname 为 NULL 时 PersonList 应返回扫描错误")
	}
	if _, err := s.PersonDuplicates(ctx, "坏数据", "", "", ""); err != nil {
		t.Errorf("PersonDuplicates 用 NullString 扫描，NULL 列不应报错: %v", err)
	}

	// person_fields.value 可空
	p := pMustCreate(t, s, &Person{ID: "bad-owner", Name: "坏字段主人"})
	pRaw(t, s, "INSERT INTO person_fields(id,person_id,label,value,sort_order) VALUES('bad-field',?,'职业',NULL,1)", p.ID)
	if _, err := s.PersonFieldList(ctx, p.ID); err == nil {
		t.Errorf("value 为 NULL 时 PersonFieldList 应返回扫描错误")
	}

	// categories / tags / event_types 的 color 可空
	pRaw(t, s, "INSERT INTO categories(id,name,color,icon,sort_order) VALUES(91,'坏圈子',NULL,NULL,NULL)")
	if _, err := s.CategoryList(ctx); err == nil {
		t.Errorf("color 为 NULL 时 CategoryList 应返回扫描错误")
	}
	pRaw(t, s, "INSERT INTO tags(id,name,color) VALUES(91,'坏标签',NULL)")
	if _, err := s.TagList(ctx); err == nil {
		t.Errorf("color 为 NULL 时 TagList 应返回扫描错误")
	}
	if _, err := s.TagsOf(ctx, "person", "bad-owner"); err != nil {
		t.Errorf("无 tagging 时 TagsOf 不应报错: %v", err)
	}
	pRaw(t, s, "INSERT INTO taggings(tag_id,target_type,target_id) VALUES(91,'person','bad-owner')")
	if _, err := s.TagsOf(ctx, "person", "bad-owner"); err == nil {
		t.Errorf("标签 color 为 NULL 时 TagsOf 应返回扫描错误")
	}
	pRaw(t, s, "INSERT INTO event_types(id,name,color,icon,is_default,sort_order) VALUES(91,'坏类型',NULL,NULL,NULL,NULL)")
	if _, err := s.EventTypeList(ctx); err == nil {
		t.Errorf("is_default 为 NULL 时 EventTypeList 应返回扫描错误")
	}
	// PersonDuplicates 的 grade 为 NULL 时也应报错（int 扫描不接受 NULL）
	pRaw(t, s, `INSERT INTO people(id,name,nickname,phone,wechat,grade,archived,created_at,updated_at)
VALUES('bad-grade','坏等级',NULL,NULL,NULL,NULL,0,?,?)`, "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z")
	if _, err := s.PersonDuplicates(ctx, "坏等级", "", "", ""); err == nil {
		t.Errorf("grade 为 NULL 时 PersonDuplicates 应返回扫描错误")
	}
}

// ===== SyncBirthdayAnniversary 的 DB 错误分支 =====

func TestPeopleSyncBirthdayErrorBranches(t *testing.T) {
	ctx := pctx(t)

	// DELETE 失败：无生日但登记了关联纪念日
	s1 := newTestStore(t)
	p1 := pMustCreate(t, s1, &Person{ID: "sb1", Name: "删除失败", BirthdayAnniversaryID: "sb1-a"})
	pRaw(t, s1, "INSERT INTO anniversaries(id,person_id,title,date,created_at,source) VALUES('sb1-a',?,'t','2024-01-01',?,'birthday')",
		p1.ID, nowUTC())
	if err := s1.SyncBirthdayAnniversary(pCancelledCtx(), p1); err == nil {
		t.Errorf("取消的 ctx 应让删除纪念日报错")
	}
	if n := pRawScanInt(t, s1, "SELECT COUNT(*) FROM anniversaries WHERE id='sb1-a'"); n != 1 {
		t.Errorf("删除失败时纪念日应仍在，得到 %d", n)
	}
	// 无关联 ID 时只需置空 people.birthday_anniversary_id
	p1b := pMustCreate(t, s1, &Person{ID: "sb1b", Name: "仅置空"})
	if err := s1.SyncBirthdayAnniversary(pCancelledCtx(), p1b); err == nil {
		t.Errorf("取消的 ctx 应让置空更新报错")
	}

	// UPDATE 纪念日失败
	s2 := newTestStore(t)
	p2 := pMustCreate(t, s2, &Person{ID: "sb2", Name: "更新失败", Birthday: "1990-01-01", BirthdayAnniversaryID: "sb2-a"})
	pRaw(t, s2, "INSERT INTO anniversaries(id,person_id,title,date,created_at,source) VALUES('sb2-a',?,'t','1990-01-01',?,'birthday')",
		p2.ID, nowUTC())
	if err := s2.SyncBirthdayAnniversary(pCancelledCtx(), p2); err == nil {
		t.Errorf("取消的 ctx 应让更新纪念日报错")
	}

	// 新建纪念日成功、但回写 people.birthday_anniversary_id 失败
	s3 := newTestStore(t)
	p3 := pMustCreate(t, s3, &Person{ID: "sb3", Name: "回写失败", Birthday: "1991-01-01"})
	pRaw(t, s3, "CREATE TRIGGER no_update_people BEFORE UPDATE ON people BEGIN SELECT RAISE(ABORT,'禁止更新 people'); END")
	err := s3.SyncBirthdayAnniversary(ctx, p3)
	if err == nil {
		t.Fatalf("回写关联 ID 失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "禁止更新 people") {
		t.Fatalf("错误应来自触发器，得到 %v", err)
	}
	if p3.BirthdayAnniversaryID != "" {
		t.Fatalf("失败时不应回填内存 ID，得到 %q", p3.BirthdayAnniversaryID)
	}
	if n := pRawScanInt(t, s3, "SELECT COUNT(*) FROM anniversaries WHERE person_id='sb3' AND source='birthday'"); n != 1 {
		t.Fatalf("纪念日应已插入，得到 %d", n)
	}

	// UPDATE 分支后回写失败
	s4 := newTestStore(t)
	p4 := pMustCreate(t, s4, &Person{ID: "sb4", Name: "更新回写失败", Birthday: "1992-02-02", BirthdayAnniversaryID: "sb4-a"})
	pRaw(t, s4, "INSERT INTO anniversaries(id,person_id,title,date,created_at,source) VALUES('sb4-a',?,'t','1992-02-02',?,'birthday')",
		p4.ID, nowUTC())
	pRaw(t, s4, "CREATE TRIGGER no_update_people BEFORE UPDATE ON people BEGIN SELECT RAISE(ABORT,'禁止更新 people'); END")
	if err := s4.SyncBirthdayAnniversary(ctx, p4); err == nil {
		t.Fatalf("回写失败应返回错误")
	}
	if got := pRawScanStr(t, s4, "SELECT title FROM anniversaries WHERE id='sb4-a'"); got != "更新回写失败的生日" {
		t.Fatalf("纪念日应已被 UPDATE，标题得到 %q", got)
	}

	// 复用查询命中但结果为空串时不应误用（person 无纪念日 -> 新建）
	s5 := newTestStore(t)
	p5 := pMustCreate(t, s5, &Person{ID: "sb5", Name: "复用查询", Birthday: "1993-03-03"})
	if err := s5.SyncBirthdayAnniversary(ctx, p5); err != nil {
		t.Fatalf("首次同步: %v", err)
	}
	if p5.BirthdayAnniversaryID == "" {
		t.Fatalf("应生成纪念日 ID")
	}
	if n := pRawScanInt(t, s5, "SELECT COUNT(*) FROM anniversaries WHERE person_id='sb5'"); n != 1 {
		t.Fatalf("应只有一条，得到 %d", n)
	}
}

// ===== 小工具 =====

func pMustCategory(t *testing.T, s *Store, ctx context.Context, c *Category) {
	t.Helper()
	if err := s.CategoryUpsert(ctx, c); err != nil {
		t.Fatalf("CategoryUpsert(%s): %v", c.Name, err)
	}
}

func pMustTag(t *testing.T, s *Store, ctx context.Context, tag *Tag) {
	t.Helper()
	if err := s.TagUpsert(ctx, tag); err != nil {
		t.Fatalf("TagUpsert(%s): %v", tag.Name, err)
	}
}

func pMustEventType(t *testing.T, s *Store, ctx context.Context, e *EventType) {
	t.Helper()
	if err := s.EventTypeUpsert(ctx, e); err != nil {
		t.Fatalf("EventTypeUpsert(%s): %v", e.Name, err)
	}
}
