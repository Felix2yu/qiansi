package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/qiansi/app/internal/store"
)

// 联系节奏的三组端点：设置页那张表、渐远名单、以及「联系过了」打卡。
// 这里盯的是待办页——派生项改得了吗（不能）、勾掉之后是不是真的不再催。

func rhythmBody(days map[int]int) []map[string]any {
	out := []map[string]any{}
	for _, g := range []int{5, 4, 3, 2, 1, 0} {
		out = append(out, map[string]any{"grade": g, "days": days[g]})
	}
	return out
}

func rhythmDaysOf(t *testing.T, body string) map[int]int {
	t.Helper()
	var tiers []store.RhythmTier
	if err := json.Unmarshal([]byte(body), &tiers); err != nil {
		t.Fatalf("decode rhythm %q: %v", body, err)
	}
	if len(tiers) != 6 {
		t.Fatalf("六档要齐，收到 %+v", tiers)
	}
	got := map[int]int{}
	for _, x := range tiers {
		got[x.Grade] = x.Days
	}
	return got
}

func TestAPIContactRhythm(t *testing.T) {
	s := newTestServer(t)

	got := rhythmDaysOf(t, apiArrayStr(t, s, http.MethodGet, "/api/v1/contact-rhythm"))
	for g, want := range map[int]int{5: 7, 4: 7, 3: 30, 2: 30, 1: 90, 0: 90} {
		if got[g] != want {
			t.Errorf("默认 %d 级 = %d 天，应为 %d", g, got[g], want)
		}
	}

	full := map[int]int{5: 14, 4: 7, 3: 30, 2: 30, 1: 90, 0: 90}
	body := apiWantStatus(t, http.MethodPut, "/api/v1/contact-rhythm",
		s.do(http.MethodPut, "/api/v1/contact-rhythm", rhythmBody(full)), http.StatusOK)
	if got = rhythmDaysOf(t, body); got[5] != 14 {
		t.Errorf("PUT 响应 ♥×5 = %d 天，应为 14", got[5])
	}
	// 存下来了，不是只回显
	if got = rhythmDaysOf(t, apiArrayStr(t, s, http.MethodGet, "/api/v1/contact-rhythm")); got[5] != 14 {
		t.Errorf("重新读取 ♥×5 = %d 天，应已持久化成 14", got[5])
	}

	// 六档缺一档就是改坏了：宁可回 400，也不要让那一档悄悄退回默认
	bad := rhythmBody(full)[:5]
	apiWantError(t, http.MethodPut, "/api/v1/contact-rhythm",
		s.do(http.MethodPut, "/api/v1/contact-rhythm", bad), http.StatusBadRequest, "缺少")
	// 同一档写两遍：听谁的说不清，直接挡
	dup := append(rhythmBody(full), map[string]any{"grade": 5, "days": 3})
	apiWantError(t, http.MethodPut, "/api/v1/contact-rhythm",
		s.do(http.MethodPut, "/api/v1/contact-rhythm", dup), http.StatusBadRequest, "重复")
	// 0 天等于天天催，越界等于关掉提醒，都不接受
	zero := map[int]int{5: 0, 4: 7, 3: 30, 2: 30, 1: 90, 0: 90}
	apiWantError(t, http.MethodPut, "/api/v1/contact-rhythm",
		s.do(http.MethodPut, "/api/v1/contact-rhythm", rhythmBody(zero)), http.StatusBadRequest, "1–")
	huge := map[int]int{5: store.MaxRhythmDays + 1, 4: 7, 3: 30, 2: 30, 1: 90, 0: 90}
	apiWantError(t, http.MethodPut, "/api/v1/contact-rhythm",
		s.do(http.MethodPut, "/api/v1/contact-rhythm", rhythmBody(huge)), http.StatusBadRequest, "3650")
	sixth := append(rhythmBody(full), map[string]any{"grade": 6, "days": 7})
	apiWantError(t, http.MethodPut, "/api/v1/contact-rhythm",
		s.do(http.MethodPut, "/api/v1/contact-rhythm", sixth), http.StatusBadRequest, "0–5")
	apiWantStatus(t, http.MethodPut, "/api/v1/contact-rhythm",
		s.raw(http.MethodPut, "/api/v1/contact-rhythm", []byte("{这不是JSON"), "application/json"), http.StatusBadRequest)

	// 挡下坏请求后，旧配置还是原来那份
	if got = rhythmDaysOf(t, apiArrayStr(t, s, http.MethodGet, "/api/v1/contact-rhythm")); got[5] != 14 {
		t.Errorf("坏请求把配置改坏了：♥×5 = %d 天", got[5])
	}

	body = apiWantStatus(t, http.MethodDelete, "/api/v1/contact-rhythm",
		s.do(http.MethodDelete, "/api/v1/contact-rhythm", nil), http.StatusOK)
	if got = rhythmDaysOf(t, body); got[5] != 7 || got[0] != 90 {
		t.Errorf("恢复默认后 = %v", got)
	}
}

// 名单上逾期的人：整份、只筛逾期、分页、打卡后挪锚点。
func TestAPIDriftListAndCheckin(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	due := apiCreatePersonMap(t, s, map[string]any{"name": "该联系了", "grade": 5})["id"].(string)
	ok := apiCreatePersonMap(t, s, map[string]any{"name": "才聊过", "grade": 5})["id"].(string)
	trashed := apiCreatePersonMap(t, s, map[string]any{"name": "回收站里", "grade": 5})["id"].(string)
	for _, seed := range []struct {
		id   string
		day  string
		kind string
	}{{due, apiDate(-30), "gift"}, {ok, apiDate(-2), "gift"}, {trashed, apiDate(-30), "gift"}} {
		if err := s.Store.TransactionCreate(ctx, &store.Transaction{
			PersonID: seed.id, Kind: seed.kind, Direction: "out", AmountFen: 100,
			Title: "往来", OccurredAt: seed.day,
		}); err != nil {
			t.Fatalf("TransactionCreate: %v", err)
		}
	}
	if err := s.Store.PersonDelete(ctx, trashed); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}

	list := driftRows(t, s, "")
	if hasName(list, "回收站里") {
		t.Errorf("回收站里的人出现在名单：%+v", list)
	}
	if !hasName(list, "该联系了") || !hasName(list, "才聊过") {
		t.Fatalf("名单少了人：%+v", list)
	}
	if list[0]["name"] != "该联系了" {
		t.Errorf("逾期的该排最前，首位是 %v", list[0]["name"])
	}
	if overdue, _ := list[0]["overdue"].(bool); !overdue {
		t.Errorf("首位应标记逾期：%+v", list[0])
	}
	if n, ok := list[0]["overdue_days"].(float64); !ok || n != 23 {
		t.Errorf("逾期天数 = %v，应为 23（30 天前联系，节奏 7 天）", list[0]["overdue_days"])
	}

	only := driftRows(t, s, "?only=overdue")
	if len(only) != 1 || only[0]["name"] != "该联系了" {
		t.Fatalf("只筛逾期 = %+v", only)
	}
	page := driftRows(t, s, "?limit=1&offset=1")
	if len(page) != 1 || page[0]["name"] == "该联系了" {
		t.Fatalf("分页 = %+v", page)
	}

	apiWantStatus(t, http.MethodPost, "/api/v1/contacts/"+due+"/checkin",
		s.do(http.MethodPost, "/api/v1/contacts/"+due+"/checkin", nil), http.StatusNoContent)
	only = driftRows(t, s, "?only=overdue")
	if len(only) != 0 {
		t.Fatalf("打卡后仍被催：%+v", only)
	}

	rec := s.do(http.MethodPost, "/api/v1/contacts/没有这个人/checkin", nil)
	apiWantStatus(t, http.MethodPost, "/api/v1/contacts/没有这个人/checkin", rec, http.StatusNotFound)
	rec = s.do(http.MethodPost, "/api/v1/contacts/"+trashed+"/checkin", nil)
	apiWantStatus(t, http.MethodPost, "/api/v1/contacts/"+trashed+"/checkin", rec, http.StatusNotFound)
}

// 派生待办：进列表、进今日，能勾不能改不能删。
func TestAPIContactDerivedReminder(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	pid := apiCreatePersonMap(t, s, map[string]any{"name": "老友", "grade": 0})["id"].(string)
	if err := s.Store.TransactionCreate(ctx, &store.Transaction{
		PersonID: pid, Kind: "gift", Direction: "out", AmountFen: 100, Title: "随礼", OccurredAt: apiDate(-100),
	}); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}
	// 未分级默认 90 天：100 天没联系，逾期 10 天
	id := "contact:" + pid
	if !hasReminder(s, "/api/v1/reminders", id) {
		t.Fatalf("待办页没有派生的联系提醒")
	}
	if !hasReminder(s, "/api/v1/reminders/upcoming?days=7", id) {
		t.Fatalf("今日/近期待办没有派生的联系提醒")
	}

	list := apiArray(s, "/api/v1/reminders")
	for _, it := range list {
		m := it.(map[string]any)
		if m["id"] != id {
			continue
		}
		if m["ref_type"] != "contact" || m["person_id"] != pid {
			t.Errorf("派生待办 = %+v", m)
		}
		if m["title"] != "该联系 老友 了" {
			t.Errorf("标题 = %v", m["title"])
		}
	}

	apiWantError(t, http.MethodPut, "/api/v1/reminders/"+id,
		s.do(http.MethodPut, "/api/v1/reminders/"+id, map[string]any{"title": "改掉", "due_at": apiDate(1) + "T09:00:00", "status": "pending"}),
		http.StatusBadRequest, "改不了")
	apiWantError(t, http.MethodDelete, "/api/v1/reminders/"+id,
		s.do(http.MethodDelete, "/api/v1/reminders/"+id, nil), http.StatusBadRequest, "删不掉")

	apiWantStatus(t, http.MethodPost, "/api/v1/reminders/"+id+"/done",
		s.do(http.MethodPost, "/api/v1/reminders/"+id+"/done", nil), http.StatusNoContent)
	if hasReminder(s, "/api/v1/reminders", id) {
		t.Errorf("勾掉之后还在待办里")
	}
	// 勾掉等于打了一次卡：锚点挪到今天，下一次到期是 90 天后
	drift := driftRows(t, s, "")
	for _, d := range drift {
		if d["person_id"] == pid && d["anchor"] != apiDate(0) {
			t.Errorf("打卡后锚点 = %v，应为今天", d["anchor"])
		}
	}
}

func apiArrayStr(t *testing.T, s *testServer, method, path string) string {
	t.Helper()
	return apiWantStatus(t, method, path, s.do(method, path, nil), http.StatusOK)
}

func driftRows(t *testing.T, s *testServer, query string) []map[string]any {
	t.Helper()
	raw := apiArrayStr(t, s, http.MethodGet, "/api/v1/contacts/drift"+query)
	var out []map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode drift %q: %v", raw, err)
	}
	return out
}

func hasName(list []map[string]any, name string) bool {
	for _, d := range list {
		if d["name"] == name {
			return true
		}
	}
	return false
}

func hasReminder(s *testServer, path, id string) bool {
	s.t.Helper()
	for _, it := range apiArray(s, path) {
		if m, ok := it.(map[string]any); ok && m["id"] == id {
			return true
		}
	}
	return false
}

// 总开关就是 settings 里的一个键：设置页用现成的 /settings/bulk 写它，
// 关掉之后待办页与今日页都不再出「该联系 X 了」，渐远名单照旧列人。
func TestAPIContactRhythmSwitch(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	pid := apiCreatePersonMap(t, s, map[string]any{"name": "被催的人", "grade": 5})["id"].(string)
	if err := s.Store.TransactionCreate(ctx, &store.Transaction{
		PersonID: pid, Kind: "gift", Direction: "out", AmountFen: 100, Title: "随礼", OccurredAt: apiDate(-30),
	}); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}
	id := "contact:" + pid
	for _, path := range []string{"/api/v1/reminders", "/api/v1/reminders/upcoming?days=7"} {
		if !hasReminder(s, path, id) {
			t.Fatalf("%s 里该有派生的联系待办", path)
		}
	}

	setSwitch := func(v string) {
		t.Helper()
		apiWantStatus(t, http.MethodPost, "/api/v1/settings/bulk",
			s.do(http.MethodPost, "/api/v1/settings/bulk", map[string]string{"contact_rhythm_enabled": v}), http.StatusOK)
	}
	setSwitch("0")

	raw := apiArrayStr(t, s, http.MethodGet, "/api/v1/settings")
	var all map[string]string
	if err := json.Unmarshal([]byte(raw), &all); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if all["contact_rhythm_enabled"] != "0" {
		t.Errorf("GET /settings 里的开关 = %q，应为 \"0\"", all["contact_rhythm_enabled"])
	}
	for _, path := range []string{"/api/v1/reminders", "/api/v1/reminders/upcoming?days=7"} {
		if hasReminder(s, path, id) {
			t.Errorf("关掉后 %s 仍在催：%v", path, id)
		}
	}
	if !hasName(driftRows(t, s, ""), "被催的人") {
		t.Errorf("关掉催不该把名单也清空")
	}

	// 再打开：同一个人又回到待办
	setSwitch("1")
	for _, path := range []string{"/api/v1/reminders", "/api/v1/reminders/upcoming?days=7"} {
		if !hasReminder(s, path, id) {
			t.Errorf("重新打开后 %s 没有待办", path)
		}
	}
}
