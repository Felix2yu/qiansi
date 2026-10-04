package api

import (
	"net/http"
	"testing"
)

// ===== 事件自带礼金（N2）=====

// egAPICount 直查库里的行数。HTTP 侧的列表都带分页和过滤，
// 「有没有多写一条」这种断言只能绕过处理器看表本身。
func egAPICount(t *testing.T, s *testServer, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.Store.DB.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("查询 %s 失败: %v", query, err)
	}
	return n
}

// egAPIEventTx 在事件详情的 expenses 里按 id 找那笔礼金。
func egAPIEventTx(t *testing.T, ev map[string]any, id string) map[string]any {
	t.Helper()
	list, _ := ev["expenses"].([]any)
	for _, it := range list {
		m, _ := it.(map[string]any)
		if m["id"] == id {
			return m
		}
	}
	t.Fatalf("事件下找不到账目 %s: %v", id, list)
	return nil
}

func TestAPIEventGift(t *testing.T) {
	s := newTestServer(t)
	host := apiCreatePersonMap(t, s, map[string]any{"name": "老王"})

	rec := s.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "老王儿子婚礼", "event_date": "2026-10-01",
		"participant_ids": []any{host["id"]},
		"gift_amount_fen": 80000, "gift_direction": "out",
	})
	apiWantStatus(t, http.MethodPost, "/api/v1/events/", rec, http.StatusOK)
	created := decodeMap(t, rec)
	id, _ := created["id"].(string)
	giftID, _ := created["gift_transaction_id"].(string)
	if giftID == "" {
		t.Fatalf("新建事件的响应没带上礼金账目 id: %v", created)
	}

	detail := s.get("/api/v1/events/" + id)
	if detail["gift_amount_fen"].(float64) != 80000 || detail["gift_direction"].(string) != "out" {
		t.Fatalf("事件详情礼金投影 = %v/%v", detail["gift_amount_fen"], detail["gift_direction"])
	}
	// 随出去的钱不再重复计入「花费」。expense_fen 是 omitempty，0 时整个键都不出现。
	if v, present := detail["expense_fen"]; present && v != float64(0) {
		t.Fatalf("expense_fen = %v, want 0", v)
	}
	tx := egAPIEventTx(t, detail, giftID)
	if tx["kind"] != "gift" || tx["person_id"] != host["id"] || tx["amount_fen"].(float64) != 80000 {
		t.Fatalf("礼金账目 = %v", tx)
	}
	if tx["settled"] != true {
		t.Fatalf("礼金应当场结清: %v", tx)
	}
	// 记在往来当天，且自动挂回了这个事件
	if tx["occurred_at"] != "2026-10-01" || tx["event_id"] != id {
		t.Fatalf("礼金账目的日期/关联 = %v / %v", tx["occurred_at"], tx["event_id"])
	}

	// 人物卡上的「随出去」跟着涨起来（N1 与 N2 接力的地方）
	after := apiStats(t, apiPersonOf(s, host["id"].(string)))
	if apiNum(t, after, "gift_out_fen") != 80000 {
		t.Fatalf("按人小结没接上事件礼金: %v", after)
	}

	// 改金额：PUT 是整行覆盖，同一笔账被改写而不是再补一条
	apiWantStatus(t, http.MethodPut, "/api/v1/events/"+id,
		s.do(http.MethodPut, "/api/v1/events/"+id, map[string]any{
			"title": "老王儿子婚礼", "event_date": "2026-10-01",
			"participant_ids": []any{host["id"]},
			"gift_amount_fen": 100000, "gift_direction": "in", "gift_person_id": host["id"],
		}), http.StatusOK)
	second := s.get("/api/v1/events/" + id)
	if second["gift_transaction_id"].(string) != giftID {
		t.Fatalf("改礼金换了一条账: %v -> %v", giftID, second["gift_transaction_id"])
	}
	if second["gift_amount_fen"].(float64) != 100000 || second["gift_direction"].(string) != "in" {
		t.Fatalf("改后投影 = %v/%v", second["gift_amount_fen"], second["gift_direction"])
	}

	// 清空金额：那笔账一起走
	apiWantStatus(t, http.MethodPut, "/api/v1/events/"+id,
		s.do(http.MethodPut, "/api/v1/events/"+id, map[string]any{
			"title": "老王儿子婚礼", "event_date": "2026-10-01",
			"participant_ids": []any{host["id"]}, "gift_amount_fen": 0,
		}), http.StatusOK)
	third := s.get("/api/v1/events/" + id)
	if third["gift_transaction_id"] != nil {
		t.Fatalf("清空后仍认领礼金: %v", third["gift_transaction_id"])
	}
	if n := egAPICount(t, s, "SELECT COUNT(*) FROM transactions WHERE event_id=?", id); n != 0 {
		t.Fatalf("清空后事件下还有 %d 笔账", n)
	}
	if st := apiStats(t, apiPersonOf(s, host["id"].(string))); apiNum(t, st, "gift_out_fen") != 0 {
		t.Fatalf("清空后按人小结没归零: %v", st)
	}
}

// 入参收口：负数、乱方向、没人可挂，都不该凭空生成一条账
func TestAPIEventGiftValidation(t *testing.T) {
	s := newTestServer(t)
	host := apiCreatePersonMap(t, s, map[string]any{"name": "老李"})

	cases := []struct {
		name    string
		body    map[string]any
		want    int
		wantMsg string
	}{
		{"负数", map[string]any{"title": "A", "event_date": "2026-10-01",
			"participant_ids": []any{host["id"]}, "gift_amount_fen": -100}, 400, "gift_amount_fen 不能为负"},
		{"乱方向", map[string]any{"title": "A", "event_date": "2026-10-01",
			"participant_ids": []any{host["id"]}, "gift_amount_fen": 100, "gift_direction": "上"},
			400, "gift_direction 只能是"},
		{"没人可挂", map[string]any{"title": "A", "event_date": "2026-10-01", "gift_amount_fen": 100},
			400, "记录礼金需要至少一位参与人"},
		{"归属人不存在", map[string]any{"title": "A", "event_date": "2026-10-01",
			"participant_ids": []any{host["id"]}, "gift_amount_fen": 100, "gift_person_id": "ghost"},
			400, "关联的记录不存在"},
	}
	for _, c := range cases {
		rec := s.do(http.MethodPost, "/api/v1/events/", c.body)
		apiWantError(t, http.MethodPost, "/api/v1/events/", rec, c.want, c.wantMsg)
	}
	if n := egAPICount(t, s, "SELECT COUNT(*) FROM events"); n != 0 {
		t.Fatalf("被拒的礼金请求仍建了 %d 个事件", n)
	}
	if n := egAPICount(t, s, "SELECT COUNT(*) FROM transactions"); n != 0 {
		t.Fatalf("被拒的礼金请求仍记了 %d 笔账", n)
	}
}
