package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qiansi/app/internal/store"
)

// ===== helper（前缀 evAPI，避免与并行同事的 helper 冲突） =====

func evAPIPerson(t *testing.T, ts *testServer, name string) string {
	t.Helper()
	p := &store.Person{Name: name}
	if err := ts.Store.PersonCreate(context.Background(), p); err != nil {
		t.Fatalf("PersonCreate(%s): %v", name, err)
	}
	return p.ID
}

func evAPIList(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200: %s", rec.Code, rec.Body.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解码数组失败: %v / %s", err, rec.Body.String())
	}
	return out
}

func evAPINum(m map[string]any, key string) float64 {
	v, _ := m[key].(float64)
	return v
}

func evAPIStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// evAPICount 直接跑一条计数查询：验证失败的那次写入有没有整体回滚。
func evAPICount(t *testing.T, ts *testServer, query string) int {
	t.Helper()
	var n int
	if err := ts.Store.DB.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("COUNT %s: %v", query, err)
	}
	return n
}

func evAPIDate(offsetDays int) string {
	return time.Now().AddDate(0, 0, offsetDays).Format("2006-01-02")
}

// ===== Events =====

func TestAPIRecords_EventFullLifecycle(t *testing.T) {
	ts := newTestServer(t)
	p1 := evAPIPerson(t, ts, "甲")
	p2 := evAPIPerson(t, ts, "乙")

	// 缺少 event_date → 400
	rec := ts.do(http.MethodPost, "/api/v1/events/", map[string]any{"title": "无日期"})
	if rec.Code != 400 {
		t.Fatalf("缺 event_date 应 400, got %d: %s", rec.Code, rec.Body.String())
	}
	// 坏 JSON → 400
	rec = ts.raw(http.MethodPost, "/api/v1/events/", []byte("{坏json"), "application/json")
	if rec.Code != 400 {
		t.Fatalf("坏 JSON 应 400, got %d", rec.Code)
	}
	// 有开销但无参与人/归属人 → 400
	rec = ts.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "无主开销", "event_date": "2026-05-01", "expense_fen": 100,
	})
	if rec.Code != 400 {
		t.Fatalf("开销无归属应 400, got %d: %s", rec.Code, rec.Body.String())
	}
	// 非法参与人 → store FK 错误 → 400（入参问题，不是服务器故障）
	rec = ts.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "坏参与人", "event_date": "2026-05-01", "participant_ids": []string{"ghost"},
	})
	if rec.Code != 400 {
		t.Fatalf("非法参与人应 400, got %d: %s", rec.Code, rec.Body.String())
	}
	// 开销归属人不存在：事件与开销同事务，两者都不落库，而不是「事件已保存、开销失败」
	before := evAPICount(t, ts, "SELECT COUNT(*) FROM events")
	rec = ts.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "归属人不存在", "event_date": "2026-05-01",
		"expense_fen": 100, "expense_person_id": "ghost",
	})
	if rec.Code != 400 {
		t.Fatalf("开销归属人不存在应 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := evAPICount(t, ts, "SELECT COUNT(*) FROM events"); got != before {
		t.Fatalf("开销失败不应留下事件: %d -> %d", before, got)
	}
	if got := evAPICount(t, ts, "SELECT COUNT(*) FROM transactions WHERE title='归属人不存在'"); got != 0 {
		t.Fatalf("开销失败不应留下往来: %d 条", got)
	}

	// 正常创建：多地点去重清洗 + 开销自动关联交易
	rec = ts.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "生日聚会", "event_date": "2026-05-01",
		"locations":       []string{" 咖啡馆 ", "公园", "咖啡馆", " "},
		"participant_ids": []string{p1, p2},
		"expense_fen":     5000,
		"summary":         "周年", "has_gift": true, "gift": "一本书",
	})
	if rec.Code != 200 {
		t.Fatalf("创建失败: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeMap(t, rec)
	id := evAPIStr(created, "id")
	if id == "" {
		t.Fatalf("响应缺少 id")
	}

	got := ts.get("/api/v1/events/" + id)
	if evAPIStr(got, "location") != "咖啡馆 · 公园" {
		t.Fatalf("location 应拼接清洗后的 locations, got %q", evAPIStr(got, "location"))
	}
	locs, _ := got["locations"].([]any)
	if len(locs) != 2 {
		t.Fatalf("locations = %v", locs)
	}
	if n := evAPINum(got, "expense_fen"); n != 5000 {
		t.Fatalf("expense_fen = %v, 期望 5000", got["expense_fen"])
	}
	ps, _ := got["participants"].([]any)
	if len(ps) != 2 {
		t.Fatalf("participants = %v", ps)
	}
	exps, _ := got["expenses"].([]any)
	if len(exps) != 1 {
		t.Fatalf("应自动生成一条关联开销: %v", exps)
	}
	exp0 := exps[0].(map[string]any)
	if evAPINum(exp0, "amount_fen") != 5000 || evAPIStr(exp0, "kind") != "expense" || !exp0["settled"].(bool) {
		t.Fatalf("自动开销字段不符: %v", exp0)
	}
	txID := evAPIStr(exp0, "id")

	// 列表：全量 / person_id / q
	// 「归属人不存在」那次写入整体回滚了，所以只剩下面正常创建的 1 条
	all := evAPIList(t, ts.do(http.MethodGet, "/api/v1/events/?limit=10", nil))
	if len(all) != 1 {
		t.Fatalf("events 列表 = %d, 期望 1", len(all))
	}
	byPerson := evAPIList(t, ts.do(http.MethodGet, "/api/v1/events/?person_id="+p1, nil))
	if len(byPerson) != 1 {
		t.Fatalf("person_id 过滤 = %d", len(byPerson))
	}
	byNone := evAPIList(t, ts.do(http.MethodGet, "/api/v1/events/?person_id=ghost", nil))
	if len(byNone) != 0 {
		t.Fatalf("未知人物应空")
	}
	byQ := evAPIList(t, ts.do(http.MethodGet, "/api/v1/events/?q=生日", nil))
	if len(byQ) != 1 {
		t.Fatalf("q 过滤 = %d", len(byQ))
	}
	byQ2 := evAPIList(t, ts.do(http.MethodGet, "/api/v1/events/?q=查无", nil))
	if len(byQ2) != 0 {
		t.Fatalf("无匹配应空")
	}
	// limit 非法字符串 → 走默认值
	if l := evAPIList(t, ts.do(http.MethodGet, "/api/v1/events/?limit=abc&offset=xyz", nil)); len(l) != 1 {
		t.Fatalf("非法 limit 应回落默认值: %d 条", len(l))
	}

	// 未知 id → 404
	if rec := ts.do(http.MethodGet, "/api/v1/events/ghost", nil); rec.Code != 404 {
		t.Fatalf("未知事件应 404, got %d", rec.Code)
	}

	// 更新：改标题 + 金额翻倍 → syncEventExpense 走 UPDATE 分支
	rec = ts.do(http.MethodPut, "/api/v1/events/"+id, map[string]any{
		"title": "生日聚会2", "event_date": "2026-05-01",
		"locations": []string{"书店"}, "participant_ids": []string{p2}, "expense_fen": 8000,
	})
	if rec.Code != 200 {
		t.Fatalf("更新失败: %d %s", rec.Code, rec.Body.String())
	}
	got = ts.get("/api/v1/events/" + id)
	if evAPIStr(got, "title") != "生日聚会2" {
		t.Fatalf("标题未更新")
	}
	if n := evAPINum(got, "expense_fen"); n != 8000 {
		t.Fatalf("expense_fen = %v, 期望 8000", got["expense_fen"])
	}
	exps, _ = got["expenses"].([]any)
	if len(exps) != 1 {
		t.Fatalf("更新开销应保持一条: %v", exps)
	}
	// 关联交易标题随事件同步
	tg := ts.get("/api/v1/transactions/" + txID)
	if evAPIStr(tg, "title") != "生日聚会2" || evAPINum(tg, "amount_fen") != 8000 {
		t.Fatalf("syncEventExpense 未更新交易: %v", tg)
	}

	// 更新为 0 开销 → 删除自动创建的 expense
	rec = ts.do(http.MethodPut, "/api/v1/events/"+id, map[string]any{
		"title": "生日聚会2", "event_date": "2026-05-01", "expense_fen": 0,
	})
	if rec.Code != 200 {
		t.Fatalf("清零开销失败: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := ts.Store.TransactionGet(context.Background(), txID); err == nil {
		t.Fatalf("expense 交易应被删除")
	}

	// 更新：有金额但无参与人/归属人 → syncEventExpense 报 400
	rec = ts.do(http.MethodPut, "/api/v1/events/"+id, map[string]any{
		"title": "生日聚会2", "event_date": "2026-05-01", "expense_fen": 700,
	})
	if rec.Code != 400 {
		t.Fatalf("无归属开销更新应 400, got %d: %s", rec.Code, rec.Body.String())
	}
	// 坏 JSON → 400
	if rec := ts.raw(http.MethodPut, "/api/v1/events/"+id, []byte("["), "application/json"); rec.Code != 400 {
		t.Fatalf("更新坏 JSON 应 400, got %d", rec.Code)
	}

	// 删除 → 204，再 GET 404
	if rec := ts.do(http.MethodDelete, "/api/v1/events/"+id, nil); rec.Code != 204 {
		t.Fatalf("删除应 204, got %d", rec.Code)
	}
	if rec := ts.do(http.MethodGet, "/api/v1/events/"+id, nil); rec.Code != 404 {
		t.Fatalf("删除后应 404, got %d", rec.Code)
	}
	// 删除未知事件 → 404
	if rec := ts.do(http.MethodDelete, "/api/v1/events/ghost", nil); rec.Code != 404 {
		t.Fatalf("删除未知事件应 404, got %d", rec.Code)
	}
}

func TestAPIRecords_EventUpdateCreatesMissingExpense(t *testing.T) {
	ts := newTestServer(t)
	p := evAPIPerson(t, ts, "丙")

	// 先建一个无开销事件
	rec := ts.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "无开销事件", "event_date": "2026-06-01", "participant_ids": []string{p},
	})
	id := evAPIStr(decodeMap(t, rec), "id")

	// 更新时指定 expense_person_id + 金额 → 走 create 分支
	rec = ts.do(http.MethodPut, "/api/v1/events/"+id, map[string]any{
		"title": "无开销事件", "event_date": "2026-06-01",
		"expense_fen": 3000, "expense_person_id": p,
	})
	if rec.Code != 200 {
		t.Fatalf("更新应 200: %d %s", rec.Code, rec.Body.String())
	}
	got := ts.get("/api/v1/events/" + id)
	exps, _ := got["expenses"].([]any)
	if len(exps) != 1 || evAPINum(exps[0].(map[string]any), "amount_fen") != 3000 {
		t.Fatalf("应新增一条关联开销: %v", exps)
	}
}

// ===== Memos =====

func TestAPIRecords_Memos(t *testing.T) {
	ts := newTestServer(t)
	p := evAPIPerson(t, ts, "小美")

	// 缺 said_at → 400
	if rec := ts.do(http.MethodPost, "/api/v1/memos/", map[string]any{"content": "x"}); rec.Code != 400 {
		t.Fatalf("缺 said_at 应 400, got %d", rec.Code)
	}
	// 坏 JSON → 400
	if rec := ts.raw(http.MethodPost, "/api/v1/memos/", []byte("nope"), "application/json"); rec.Code != 400 {
		t.Fatalf("坏 JSON 应 400, got %d", rec.Code)
	}
	// 非法人物 → 外键失败 400
	if rec := ts.do(http.MethodPost, "/api/v1/memos/", map[string]any{
		"content": "x", "said_at": "2026-05-01T10:00:00", "person_id": "ghost",
	}); rec.Code != 400 {
		t.Fatalf("非法 person 应 400, got %d: %s", rec.Code, rec.Body.String())
	}
	// 正常
	rec := ts.do(http.MethodPost, "/api/v1/memos/", map[string]any{
		"content": "答应请客", "said_at": "2026-05-01T10:00:00",
		"person_id": p, "is_promise": true, "due_date": "2026-06-01",
	})
	if rec.Code != 200 {
		t.Fatalf("创建备忘失败: %d %s", rec.Code, rec.Body.String())
	}
	m1 := decodeMap(t, rec)
	if evAPIStr(m1, "speaker") != "other" || evAPIStr(m1, "status") != "open" {
		t.Fatalf("默认值未生效: %v", m1)
	}
	rec = ts.do(http.MethodPost, "/api/v1/memos/", map[string]any{
		"content": "普通记录", "said_at": "2026-04-01T10:00:00", "speaker": "me",
	})
	m2 := decodeMap(t, rec)

	list := evAPIList(t, ts.do(http.MethodGet, "/api/v1/memos/", nil))
	if len(list) != 2 {
		t.Fatalf("备忘列表 = %d", len(list))
	}
	promises := evAPIList(t, ts.do(http.MethodGet, "/api/v1/memos/?promises_only=1&person_id="+p, nil))
	if len(promises) != 1 || evAPIStr(promises[0], "id") != evAPIStr(m1, "id") {
		t.Fatalf("promises_only 过滤不符: %v", promises)
	}
	byPerson := evAPIList(t, ts.do(http.MethodGet, "/api/v1/memos/?person_id=ghost", nil))
	if len(byPerson) != 0 {
		t.Fatalf("未知人物应空")
	}

	// 更新
	id := evAPIStr(m1, "id")
	rec = ts.do(http.MethodPut, "/api/v1/memos/"+id, map[string]any{
		"content": "已请客", "said_at": "2026-05-01T10:00:00",
		"person_id": p, "is_promise": true, "status": "fulfilled",
	})
	if rec.Code != 200 {
		t.Fatalf("更新备忘失败: %d", rec.Code)
	}
	list = evAPIList(t, ts.do(http.MethodGet, "/api/v1/memos/", nil))
	for _, m := range list {
		if evAPIStr(m, "id") == id && evAPIStr(m, "status") != "fulfilled" {
			t.Fatalf("更新未持久化: %v", m)
		}
	}
	// 坏 JSON 更新 → 400
	if rec := ts.raw(http.MethodPut, "/api/v1/memos/"+id, []byte("x"), "application/json"); rec.Code != 400 {
		t.Fatalf("更新坏 JSON 应 400, got %d", rec.Code)
	}

	// 删除两条
	if rec := ts.do(http.MethodDelete, "/api/v1/memos/"+id, nil); rec.Code != 204 {
		t.Fatalf("删除应 204, got %d", rec.Code)
	}
	if rec := ts.do(http.MethodDelete, "/api/v1/memos/"+evAPIStr(m2, "id"), nil); rec.Code != 204 {
		t.Fatalf("删除第二条应 204")
	}
	if list := evAPIList(t, ts.do(http.MethodGet, "/api/v1/memos/", nil)); len(list) != 0 {
		t.Fatalf("删除后应空: %v", list)
	}
}

// ===== Transactions & Repayments =====

func TestAPIRecords_TransactionsAndRepayments(t *testing.T) {
	ts := newTestServer(t)
	p := evAPIPerson(t, ts, "老王")

	// 缺 occurred_at → 400
	if rec := ts.do(http.MethodPost, "/api/v1/transactions/", map[string]any{"person_id": p, "amount_fen": 1}); rec.Code != 400 {
		t.Fatalf("缺 occurred_at 应 400, got %d", rec.Code)
	}
	// 坏 JSON → 400
	if rec := ts.raw(http.MethodPost, "/api/v1/transactions/", []byte("{"), "application/json"); rec.Code != 400 {
		t.Fatalf("坏 JSON 应 400, got %d", rec.Code)
	}
	// 非法人物 → 外键失败 400
	if rec := ts.do(http.MethodPost, "/api/v1/transactions/", map[string]any{
		"person_id": "ghost", "direction": "out", "amount_fen": 100, "occurred_at": "2026-05-01",
	}); rec.Code != 400 {
		t.Fatalf("非法 person 交易应 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// 借款 1000（未结），带 due_date 与事件关联
	evRec := ts.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "旅行", "event_date": "2026-05-01", "participant_ids": []string{p},
	})
	evID := evAPIStr(decodeMap(t, evRec), "id")
	rec := ts.do(http.MethodPost, "/api/v1/transactions/", map[string]any{
		"person_id": p, "kind": "loan", "direction": "out", "amount_fen": 1000,
		"title": "借款", "occurred_at": "2026-05-01", "due_date": "2026-08-01", "event_id": evID,
	})
	if rec.Code != 200 {
		t.Fatalf("创建交易失败: %d %s", rec.Code, rec.Body.String())
	}
	tx := decodeMap(t, rec)
	txID := evAPIStr(tx, "id")
	if evAPIStr(tx, "event_title") != "旅行" {
		t.Fatalf("响应应回填 event_title: %v", tx)
	}

	// GET / 未知 → 404
	if rec := ts.do(http.MethodGet, "/api/v1/transactions/ghost", nil); rec.Code != 404 {
		t.Fatalf("未知交易应 404")
	}
	list := evAPIList(t, ts.do(http.MethodGet, "/api/v1/transactions/?person_id="+p, nil))
	if len(list) != 1 {
		t.Fatalf("交易列表 = %d", len(list))
	}
	if len(evAPIList(t, ts.do(http.MethodGet, "/api/v1/transactions/?person_id=ghost", nil))) != 0 {
		t.Fatalf("未知人物交易应空")
	}

	// 部分还款 400 → 仍未结清
	rec = ts.do(http.MethodPost, "/api/v1/transactions/"+txID+"/repayments", map[string]any{
		"amount_fen": 400, "occurred_at": "2026-06-01", "note": "微信",
	})
	if rec.Code != 200 {
		t.Fatalf("还款失败: %d %s", rec.Code, rec.Body.String())
	}
	rp1 := decodeMap(t, rec)
	rp1ID := evAPIStr(rp1, "id")
	tx = ts.get("/api/v1/transactions/" + txID)
	if tx["settled"].(bool) {
		t.Fatalf("部分还款后不应结清")
	}
	if evAPINum(tx, "repaid_fen") != 400 {
		t.Fatalf("repaid_fen = %v", tx["repaid_fen"])
	}

	// 省略 occurred_at → 默认今天
	rec = ts.do(http.MethodPost, "/api/v1/transactions/"+txID+"/repayments", map[string]any{"amount_fen": 100})
	if rec.Code != 200 {
		t.Fatalf("默认日期还款失败: %d %s", rec.Code, rec.Body.String())
	}
	rp2 := decodeMap(t, rec)
	if !strings.HasPrefix(evAPIStr(rp2, "occurred_at"), evAPIDate(0)) && evAPIStr(rp2, "occurred_at") != evAPIDate(0) {
		t.Fatalf("occurred_at 应默认今天, got %q", rp2["occurred_at"])
	}

	// 校验分支：金额 <= 0 / 坏 JSON / 未知交易
	if rec := ts.do(http.MethodPost, "/api/v1/transactions/"+txID+"/repayments", map[string]any{"amount_fen": 0}); rec.Code != 400 {
		t.Fatalf("金额 0 应 400, got %d", rec.Code)
	}
	if rec := ts.raw(http.MethodPost, "/api/v1/transactions/"+txID+"/repayments", []byte("bad"), "application/json"); rec.Code != 400 {
		t.Fatalf("坏 JSON 还款应 400")
	}
	if rec := ts.do(http.MethodPost, "/api/v1/transactions/ghost/repayments", map[string]any{"amount_fen": 1}); rec.Code != 400 {
		t.Fatalf("未知交易还款应 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// 还款列表
	rps := evAPIList(t, ts.do(http.MethodGet, "/api/v1/transactions/"+txID+"/repayments", nil))
	if len(rps) != 2 || evAPIStr(rps[0], "note") != "" {
		t.Fatalf("还款列表不符: %v", rps)
	}

	// 还完 500 → resyncSettled 自动结清
	rec = ts.do(http.MethodPost, "/api/v1/transactions/"+txID+"/repayments", map[string]any{"amount_fen": 500})
	if rec.Code != 200 {
		t.Fatalf("结清还款失败")
	}
	rp3ID := evAPIStr(decodeMap(t, rec), "id")
	tx = ts.get("/api/v1/transactions/" + txID)
	if !tx["settled"].(bool) || evAPIStr(tx, "settled_at") == "" {
		t.Fatalf("还完应自动结清: %v", tx)
	}
	// 再还一笔 → 状态不变（settled==t.Settled 早退分支）
	overRec := ts.do(http.MethodPost, "/api/v1/transactions/"+txID+"/repayments", map[string]any{"amount_fen": 10})
	if overRec.Code != 200 {
		t.Fatalf("超额还款应 200")
	}
	extraID := evAPIStr(decodeMap(t, overRec), "id")

	// 删除还款 → 回到未结清
	for _, rid := range []string{rp1ID, evAPIStr(rp2, "id"), rp3ID, extraID} {
		if rec := ts.do(http.MethodDelete, "/api/v1/transactions/"+txID+"/repayments/"+rid, nil); rec.Code != 204 {
			t.Fatalf("删除还款 %s 应 204, got %d", rid, rec.Code)
		}
	}
	tx = ts.get("/api/v1/transactions/" + txID)
	if tx["settled"].(bool) || evAPIStr(tx, "settled_at") != "" {
		t.Fatalf("删光还款应回到未结清: %v", tx)
	}
	if len(evAPIList(t, ts.do(http.MethodGet, "/api/v1/transactions/"+txID+"/repayments", nil))) != 0 {
		t.Fatalf("还款列表应空")
	}
	// 删除未知还款 → 404；未知交易下的还款本就不存在
	if rec := ts.do(http.MethodDelete, "/api/v1/transactions/ghost/repayments/ghost", nil); rec.Code != 404 {
		t.Fatalf("未知还款删除应 404, got %d", rec.Code)
	}

	// 更新交易金额 → 200，再验证持久化
	rec = ts.do(http.MethodPut, "/api/v1/transactions/"+txID, map[string]any{
		"person_id": p, "kind": "loan", "direction": "out", "amount_fen": 2000,
		"title": "借款(改)", "occurred_at": "2026-05-01",
	})
	if rec.Code != 200 {
		t.Fatalf("更新交易失败: %d %s", rec.Code, rec.Body.String())
	}
	tx = ts.get("/api/v1/transactions/" + txID)
	if evAPINum(tx, "amount_fen") != 2000 || evAPIStr(tx, "title") != "借款(改)" {
		t.Fatalf("更新未持久化: %v", tx)
	}
	// 坏 JSON 更新 → 400
	if rec := ts.raw(http.MethodPut, "/api/v1/transactions/"+txID, []byte("]"), "application/json"); rec.Code != 400 {
		t.Fatalf("坏 JSON 更新应 400")
	}

	// 删除交易 → 204
	if rec := ts.do(http.MethodDelete, "/api/v1/transactions/"+txID, nil); rec.Code != 204 {
		t.Fatalf("删除交易应 204")
	}
	if len(evAPIList(t, ts.do(http.MethodGet, "/api/v1/transactions/", nil))) != 0 {
		t.Fatalf("交易应清空")
	}
	// 事件也不应受影响（GET 200）
	if rec := ts.do(http.MethodGet, "/api/v1/events/"+evID, nil); rec.Code != 200 {
		t.Fatalf("事件应仍存在")
	}
}

// ===== Anniversaries =====

func TestAPIRecords_Anniversaries(t *testing.T) {
	ts := newTestServer(t)
	p := evAPIPerson(t, ts, "阿珍")

	// 缺 title/date → 400
	if rec := ts.do(http.MethodPost, "/api/v1/anniversaries/", map[string]any{"title": "只有标题"}); rec.Code != 400 {
		t.Fatalf("缺 date 应 400, got %d", rec.Code)
	}
	if rec := ts.do(http.MethodPost, "/api/v1/anniversaries/", map[string]any{"date": "2026-05-20"}); rec.Code != 400 {
		t.Fatalf("缺 title 应 400, got %d", rec.Code)
	}
	if rec := ts.raw(http.MethodPost, "/api/v1/anniversaries/", []byte("{"), "application/json"); rec.Code != 400 {
		t.Fatalf("坏 JSON 应 400")
	}
	// 非法人物 → 外键失败 400
	if rec := ts.do(http.MethodPost, "/api/v1/anniversaries/", map[string]any{
		"title": "x", "date": "2026-05-20", "person_id": "ghost",
	}); rec.Code != 400 {
		t.Fatalf("非法 person 纪念日应 400, got %d: %s", rec.Code, rec.Body.String())
	}

	rec := ts.do(http.MethodPost, "/api/v1/anniversaries/", map[string]any{
		"title": "纪念日", "date": "2020-05-20", "person_id": p, "repeat_yearly": true,
	})
	if rec.Code != 200 {
		t.Fatalf("创建纪念日失败: %d %s", rec.Code, rec.Body.String())
	}
	a := decodeMap(t, rec)
	id := evAPIStr(a, "id")
	if evAPIStr(a, "remind_days") != "7,3,1,0" {
		t.Fatalf("remind_days 默认值不符: %v", a["remind_days"])
	}
	// 无归属人
	rec = ts.do(http.MethodPost, "/api/v1/anniversaries/", map[string]any{
		"title": "独立日", "date": "2021-01-01", "remind_days": "0",
	})
	id2 := evAPIStr(decodeMap(t, rec), "id")

	list := evAPIList(t, ts.do(http.MethodGet, "/api/v1/anniversaries/", nil))
	if len(list) != 2 {
		t.Fatalf("纪念日列表 = %d", len(list))
	}

	rec = ts.do(http.MethodPut, "/api/v1/anniversaries/"+id, map[string]any{
		"title": "纪念日2", "date": "2020-05-21", "is_lunar": true, "remind_days": "1,0",
	})
	if rec.Code != 200 {
		t.Fatalf("更新失败: %d", rec.Code)
	}
	list = evAPIList(t, ts.do(http.MethodGet, "/api/v1/anniversaries/", nil))
	for _, x := range list {
		if evAPIStr(x, "id") == id {
			if evAPIStr(x, "title") != "纪念日2" || !x["is_lunar"].(bool) || evAPIStr(x, "remind_days") != "1,0" {
				t.Fatalf("更新未持久化: %v", x)
			}
		}
	}
	if rec := ts.raw(http.MethodPut, "/api/v1/anniversaries/"+id, []byte("z"), "application/json"); rec.Code != 400 {
		t.Fatalf("坏 JSON 更新应 400")
	}

	if rec := ts.do(http.MethodDelete, "/api/v1/anniversaries/"+id, nil); rec.Code != 204 {
		t.Fatalf("删除应 204")
	}
	if rec := ts.do(http.MethodDelete, "/api/v1/anniversaries/"+id2, nil); rec.Code != 204 {
		t.Fatalf("删除第二条应 204")
	}
	if list := evAPIList(t, ts.do(http.MethodGet, "/api/v1/anniversaries/", nil)); len(list) != 0 {
		t.Fatalf("删除后应空: %v", list)
	}
}

// ===== Reminders =====

func TestAPIRecords_Reminders(t *testing.T) {
	ts := newTestServer(t)
	p := evAPIPerson(t, ts, "阿强")

	// 校验分支
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/", map[string]any{"due_at": "2026-05-01T09:00:00"}); rec.Code != 400 {
		t.Fatalf("缺 title 应 400")
	}
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/", map[string]any{"title": "  ", "due_at": "2026-05-01T09:00:00"}); rec.Code != 400 {
		t.Fatalf("空白 title 应 400")
	}
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/", map[string]any{"title": "有标题"}); rec.Code != 400 {
		t.Fatalf("缺 due_at 应 400")
	}
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/", map[string]any{"title": "空白due", "due_at": "  "}); rec.Code != 400 {
		t.Fatalf("空白 due_at 应 400")
	}
	if rec := ts.raw(http.MethodPost, "/api/v1/reminders/", []byte(".."), "application/json"); rec.Code != 400 {
		t.Fatalf("坏 JSON 应 400")
	}
	// 非法人物 → 外键失败 400
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/", map[string]any{
		"title": "x", "due_at": "2026-05-01T09:00:00", "person_id": "ghost",
	}); rec.Code != 400 {
		t.Fatalf("非法 person 提醒应 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// 近期待办（+2 天）与远期（+40 天）
	rec := ts.do(http.MethodPost, "/api/v1/reminders/", map[string]any{
		"title": "还书", "due_at": evAPIDate(2) + "T09:00:00", "person_id": p, "ref_type": "custom", "ref_id": "r-1",
	})
	near := decodeMap(t, rec)
	if evAPIStr(near, "status") != "pending" {
		t.Fatalf("默认 status 应为 pending")
	}
	// 手工建两条，精确控制 due_at 供 horizon 断言
	mustCreate := func(title, dueAt string) string {
		r := ts.do(http.MethodPost, "/api/v1/reminders/", map[string]any{"title": title, "due_at": dueAt})
		if r.Code != 200 {
			t.Fatalf("创建提醒 %s: %d %s", title, r.Code, r.Body.String())
		}
		return evAPIStr(decodeMap(t, r), "id")
	}
	nearID := mustCreate("近的", evAPIDate(2)+"T23:00:00")
	farID := mustCreate("远的", evAPIDate(40)+"T09:00:00")

	// upcoming：days=7 → 只有 7 天内的（这里两条 +2 的）
	up := evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/upcoming?days=7", nil))
	if len(up) != 2 {
		t.Fatalf("days=7 upcoming = %d, 期望 2: %v", len(up), up)
	}
	for _, r := range up {
		if r["ref_type"] == nil || evAPIStr(r, "ref_type") != "custom" {
			t.Fatalf("upcoming 字段不符: %v", r)
		}
	}
	// days=0 → 不限
	upAll := evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/upcoming?days=0", nil))
	if len(upAll) != 3 {
		t.Fatalf("days=0 应含全部 3 条: %v", upAll)
	}
	if upAll[0]["due_at"].(string) > upAll[len(upAll)-1]["due_at"].(string) {
		t.Fatalf("upcoming 应按 due_at 升序")
	}
	// days 非法 → 默认 30（远的 40 天不进）
	if up := evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/upcoming?days=abc", nil)); len(up) != 2 {
		t.Fatalf("非法 days 应回落 30: %d 条", len(up))
	}

	// 列表 + 状态过滤
	lst := evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/", nil))
	if len(lst) != 3 {
		t.Fatalf("提醒列表 = %d", len(lst))
	}
	if evAPIStr(lst[0], "person_name") != "阿强" || evAPIStr(lst[0], "ref_id") != "r-1" {
		t.Fatalf("列表关联字段不符: %v", lst[0])
	}
	if done := evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/?status=done", nil)); len(done) != 0 {
		t.Fatalf("done 过滤应空")
	}

	// 更新
	rec = ts.do(http.MethodPut, "/api/v1/reminders/"+nearID, map[string]any{
		"title": "近的(改)", "due_at": evAPIDate(3) + "T09:00:00", "status": "pending",
	})
	if rec.Code != 200 {
		t.Fatalf("更新提醒失败: %d %s", rec.Code, rec.Body.String())
	}
	lst = evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/?status=pending", nil))
	for _, r := range lst {
		if evAPIStr(r, "id") == nearID && evAPIStr(r, "title") != "近的(改)" {
			t.Fatalf("更新未持久化: %v", r)
		}
	}
	if rec := ts.raw(http.MethodPut, "/api/v1/reminders/"+nearID, []byte("("), "application/json"); rec.Code != 400 {
		t.Fatalf("坏 JSON 更新应 400")
	}

	// done 普通提醒
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/"+farID+"/done", nil); rec.Code != 204 {
		t.Fatalf("done 应 204, got %d", rec.Code)
	}
	doneList := evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/?status=done", nil))
	if len(doneList) != 1 || evAPIStr(doneList[0], "id") != farID || evAPIStr(doneList[0], "completed_at") == "" {
		t.Fatalf("done 后状态不符: %v", doneList)
	}
	up = evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/upcoming?days=0", nil))
	for _, r := range up {
		if evAPIStr(r, "id") == farID {
			t.Fatalf("done 后不应出现在 upcoming")
		}
	}
	// 未知 id done → 404，写 0 行不能当成「已标记完成」
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/ghost/done", nil); rec.Code != 404 {
		t.Fatalf("未知提醒 done 应 404, got %d", rec.Code)
	}

	// 删除
	for _, id := range []string{evAPIStr(near, "id"), nearID, farID} {
		if rec := ts.do(http.MethodDelete, "/api/v1/reminders/"+id, nil); rec.Code != 204 {
			t.Fatalf("删除 %s 应 204, got %d", id, rec.Code)
		}
	}
	if lst := evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/", nil)); len(lst) != 0 {
		t.Fatalf("删除后应空: %v", lst)
	}
}

func TestAPIRecords_AnniversaryReminderDismissFlow(t *testing.T) {
	ts := newTestServer(t)
	p := evAPIPerson(t, ts, "小林")

	// 明天到期的纪念日（remind_days=1,0 → 1 天档今天触发），horizon=7 应出现在 upcoming
	rec := ts.do(http.MethodPost, "/api/v1/anniversaries/", map[string]any{
		"title": "相识纪念日", "date": evAPIDate(1), "person_id": p, "remind_days": "1,0",
	})
	if rec.Code != 200 {
		t.Fatalf("创建纪念日失败: %d %s", rec.Code, rec.Body.String())
	}
	annID := evAPIStr(decodeMap(t, rec), "id")

	up := evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/upcoming?days=7", nil))
	var annRemID string
	for _, r := range up {
		if strings.HasPrefix(evAPIStr(r, "ref_type"), "anniversary") {
			annRemID = evAPIStr(r, "id")
		}
	}
	if annRemID == "" {
		t.Fatalf("upcoming 缺少纪念日衍生提醒: %v", up)
	}
	if !strings.HasPrefix(annRemID, "anniv:"+annID+":") {
		t.Fatalf("衍生提醒 id 结构不符: %s", annRemID)
	}

	// 畸形 anniv id → 400
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/anniv:只有一段/done", nil); rec.Code != 400 {
		t.Fatalf("畸形衍生 id 应 400, got %d", rec.Code)
	}
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/anniv::/done", nil); rec.Code != 400 {
		t.Fatalf("空 annivID 应 400, got %d", rec.Code)
	}

	// done 衍生提醒 → 204，且刷新后不再出现
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/"+annRemID+"/done", nil); rec.Code != 204 {
		t.Fatalf("衍生提醒 done 应 204, got %d", rec.Code)
	}
	up = evAPIList(t, ts.do(http.MethodGet, "/api/v1/reminders/upcoming?days=7", nil))
	for _, r := range up {
		if evAPIStr(r, "id") == annRemID {
			t.Fatalf("dismiss 后不应再出现: %v", up)
		}
	}
	// 重复 done 也应 204（INSERT OR IGNORE）
	if rec := ts.do(http.MethodPost, "/api/v1/reminders/"+annRemID+"/done", nil); rec.Code != 204 {
		t.Fatalf("重复 dismiss 应 204, got %d", rec.Code)
	}
}

// ===== 关闭 DB 的 500 分支 =====

func TestAPIRecords_ClosedDB500s(t *testing.T) {
	ts := newTestServer(t)
	if err := ts.Store.DB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	wantStatus := func(name string, rec *httptest.ResponseRecorder, code int) {
		if rec.Code != code {
			t.Errorf("%s: 状态码 = %d, 期望 %d: %s", name, rec.Code, code, rec.Body.String())
		}
	}

	wantStatus("eventList", ts.do(http.MethodGet, "/api/v1/events/", nil), 500)
	wantStatus("eventGet", ts.do(http.MethodGet, "/api/v1/events/1", nil), 500)
	wantStatus("eventCreate", ts.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "t", "event_date": "2026-01-01",
	}), 500)
	wantStatus("eventUpdate", ts.do(http.MethodPut, "/api/v1/events/1", map[string]any{
		"title": "t", "event_date": "2026-01-01",
	}), 500)
	wantStatus("eventDelete", ts.do(http.MethodDelete, "/api/v1/events/1", nil), 500)

	wantStatus("memoList", ts.do(http.MethodGet, "/api/v1/memos/", nil), 500)
	wantStatus("memoCreate", ts.do(http.MethodPost, "/api/v1/memos/", map[string]any{
		"content": "c", "said_at": "2026-01-01T00:00:00",
	}), 500)
	wantStatus("memoUpdate", ts.do(http.MethodPut, "/api/v1/memos/1", map[string]any{
		"content": "c", "said_at": "2026-01-01T00:00:00",
	}), 500)
	wantStatus("memoDelete", ts.do(http.MethodDelete, "/api/v1/memos/1", nil), 500)

	wantStatus("txList", ts.do(http.MethodGet, "/api/v1/transactions/", nil), 500)
	wantStatus("txGet(404)", ts.do(http.MethodGet, "/api/v1/transactions/1", nil), 404)
	wantStatus("txCreate", ts.do(http.MethodPost, "/api/v1/transactions/", map[string]any{
		"person_id": "p", "direction": "out", "amount_fen": 1, "occurred_at": "2026-01-01",
	}), 500)
	wantStatus("txUpdate", ts.do(http.MethodPut, "/api/v1/transactions/1", map[string]any{
		"person_id": "p", "direction": "out", "amount_fen": 1, "occurred_at": "2026-01-01",
	}), 500)
	wantStatus("txDelete", ts.do(http.MethodDelete, "/api/v1/transactions/1", nil), 500)
	wantStatus("repaymentCreate", ts.do(http.MethodPost, "/api/v1/transactions/1/repayments", map[string]any{
		"amount_fen": 1,
	}), 500)
	wantStatus("repaymentsList", ts.do(http.MethodGet, "/api/v1/transactions/1/repayments", nil), 500)
	wantStatus("repaymentDelete", ts.do(http.MethodDelete, "/api/v1/transactions/1/repayments/2", nil), 500)

	wantStatus("annivList", ts.do(http.MethodGet, "/api/v1/anniversaries/", nil), 500)
	wantStatus("annivCreate", ts.do(http.MethodPost, "/api/v1/anniversaries/", map[string]any{
		"title": "t", "date": "2026-01-01",
	}), 500)
	wantStatus("annivUpdate", ts.do(http.MethodPut, "/api/v1/anniversaries/1", map[string]any{
		"title": "t", "date": "2026-01-01",
	}), 500)
	wantStatus("annivDelete", ts.do(http.MethodDelete, "/api/v1/anniversaries/1", nil), 500)

	wantStatus("reminderList", ts.do(http.MethodGet, "/api/v1/reminders/", nil), 500)
	wantStatus("upcoming", ts.do(http.MethodGet, "/api/v1/reminders/upcoming", nil), 500)
	wantStatus("reminderCreate", ts.do(http.MethodPost, "/api/v1/reminders/", map[string]any{
		"title": "t", "due_at": "2026-01-01T09:00:00",
	}), 500)
	wantStatus("reminderUpdate", ts.do(http.MethodPut, "/api/v1/reminders/1", map[string]any{
		"title": "t", "due_at": "2026-01-01T09:00:00",
	}), 500)
	wantStatus("reminderDelete", ts.do(http.MethodDelete, "/api/v1/reminders/1", nil), 500)
	wantStatus("reminderDone", ts.do(http.MethodPost, "/api/v1/reminders/1/done", nil), 500)
	wantStatus("annivDismiss", ts.do(http.MethodPost, "/api/v1/reminders/anniv:a:2026-01-01:0/done", nil), 500)
}
