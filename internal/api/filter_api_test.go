package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// O6 的筛选参数走 HTTP 契约：给错值必须 400 说清楚，
// 不能悄悄当成「不限」——前端拼错参数时会看到全量数据，还以为筛选生效了。

func apiEventTypeID(t *testing.T, s *testServer, name string) int {
	t.Helper()
	rec := s.do(http.MethodPost, "/api/v1/event-types/", map[string]any{"name": name, "color": "#112233"})
	id, ok := decodeMap(t, rec)["id"].(float64)
	if !ok {
		t.Fatalf("POST /event-types 未返回数字 id: %s", rec.Body.String())
	}
	return int(id)
}

// apiEventTitles 拉往来列表，返回命中的标题集合。
func apiEventTitles(t *testing.T, s *testServer, query string) map[string]bool {
	t.Helper()
	path := "/api/v1/events/" + query
	rec := s.do(http.MethodGet, path, nil)
	body := apiWantStatus(t, http.MethodGet, path, rec, http.StatusOK)
	var items []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode events %s: %v (%s)", query, err, body)
	}
	got := map[string]bool{}
	for _, it := range items {
		got[it.Title] = true
	}
	return got
}

func apiTxTitles(t *testing.T, s *testServer, query string) map[string]bool {
	t.Helper()
	path := "/api/v1/transactions/" + query
	rec := s.do(http.MethodGet, path, nil)
	body := apiWantStatus(t, http.MethodGet, path, rec, http.StatusOK)
	var items []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode transactions %s: %v (%s)", query, err, body)
	}
	got := map[string]bool{}
	for _, it := range items {
		got[it.Title] = true
	}
	return got
}

func apiWantTitles(t *testing.T, label string, got map[string]bool, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s 命中 %d 条，期望 %d（%v）", label, len(got), len(want), want)
	}
	for _, w := range want {
		if !got[w] {
			t.Fatalf("%s 少了「%s」：%+v", label, w, got)
		}
	}
}

func TestAPIEventListFilterParams(t *testing.T) {
	s := newTestServer(t)
	party := apiEventTypeID(t, s, "聚会")
	sport := apiEventTypeID(t, s, "运动")
	pid := apiMustCreate(t, s, "/api/v1/people/", map[string]any{"name": "老王"})
	mk := func(title, date string, typeID int) {
		apiMustCreate(t, s, "/api/v1/events/", map[string]any{
			"title": title, "event_date": date, "type_id": typeID,
			"participant_ids": []string{pid},
		})
	}
	mk("春节聚餐", "2026-02-10", party)
	mk("春日爬山", "2026-04-05", sport)
	mk("夏日饭局", "2026-07-01", party)

	apiWantTitles(t, "全量", apiEventTitles(t, s, ""), "春节聚餐", "春日爬山", "夏日饭局")
	apiWantTitles(t, "按类型", apiEventTitles(t, s, "?type_id="+strconv.Itoa(party)), "春节聚餐", "夏日饭局")
	apiWantTitles(t, "起始", apiEventTitles(t, s, "?from=2026-03-01"), "春日爬山", "夏日饭局")
	apiWantTitles(t, "截止", apiEventTitles(t, s, "?to=2026-03-01"), "春节聚餐")
	apiWantTitles(t, "区间", apiEventTitles(t, s, "?from=2026-03-01&to=2026-06-30"), "春日爬山")
	apiWantTitles(t, "类型+区间", apiEventTitles(t, s, "?type_id="+strconv.Itoa(party)+"&from=2026-06-01"), "夏日饭局")
	// 2026-02-10 这天两头都含
	apiWantTitles(t, "端点重合", apiEventTitles(t, s, "?from=2026-02-10&to=2026-02-10"), "春节聚餐")
	// 与既有的 person_id / q 共存
	apiWantTitles(t, "参与人+类型", apiEventTitles(t, s, "?person_id="+pid+"&type_id="+strconv.Itoa(sport)), "春日爬山")

	// 坏值一律 400，且给出这一项的名字
	for _, q := range []string{"?type_id=abc", "?type_id=0", "?type_id=-1"} {
		apiWantError(t, http.MethodGet, "/api/v1/events/"+q, s.do(http.MethodGet, "/api/v1/events/"+q, nil),
			http.StatusBadRequest, "type_id")
	}
	apiWantError(t, http.MethodGet, "/api/v1/events/?from=2026-02-30",
		s.do(http.MethodGet, "/api/v1/events/?from=2026-02-30", nil), http.StatusBadRequest, "from")
	apiWantError(t, http.MethodGet, "/api/v1/events/?to=2026-13-01",
		s.do(http.MethodGet, "/api/v1/events/?to=2026-13-01", nil), http.StatusBadRequest, "to")
	// 起止填反：返回空列表会和「这段时间真没有记录」长得一样，必须报错
	apiWantError(t, http.MethodGet, "/api/v1/events/?from=2026-07-01&to=2026-02-01",
		s.do(http.MethodGet, "/api/v1/events/?from=2026-07-01&to=2026-02-01", nil),
		http.StatusBadRequest, "起始日期不能晚于截止日期")
}

func TestAPITxListFilterParams(t *testing.T) {
	s := newTestServer(t)
	pid := apiMustCreate(t, s, "/api/v1/people/", map[string]any{"name": "老李"})
	must := func(title, kind, date string, settled bool) {
		apiMustCreate(t, s, "/api/v1/transactions/", map[string]any{
			"person_id": pid, "kind": kind, "direction": "out",
			"amount_fen": 5000, "title": title, "occurred_at": date, "settled": settled,
		})
	}
	must("开春借款", "loan", "2026-01-05", false)
	must("生日礼物", "gift", "2026-03-05", true)
	must("年中还款", "loan", "2026-05-05", true)

	apiWantTitles(t, "全量", apiTxTitles(t, s, ""), "开春借款", "生日礼物", "年中还款")
	apiWantTitles(t, "按类别", apiTxTitles(t, s, "?kind=loan"), "开春借款", "年中还款")
	apiWantTitles(t, "未结清", apiTxTitles(t, s, "?settled=0"), "开春借款")
	apiWantTitles(t, "已结清", apiTxTitles(t, s, "?settled=1"), "生日礼物", "年中还款")
	apiWantTitles(t, "类别+结清", apiTxTitles(t, s, "?kind=loan&settled=1"), "年中还款")
	apiWantTitles(t, "起始", apiTxTitles(t, s, "?from=2026-02-01"), "生日礼物", "年中还款")
	apiWantTitles(t, "区间", apiTxTitles(t, s, "?from=2026-02-01&to=2026-04-01"), "生日礼物")
	apiWantTitles(t, "按人+类别", apiTxTitles(t, s, "?person_id="+pid+"&kind=gift"), "生日礼物")
	apiWantTitles(t, "查无此人", apiTxTitles(t, s, "?person_id=ghost"))

	for _, q := range []string{"?kind=borrow", "?settled=yes", "?settled=2"} {
		apiWantError(t, http.MethodGet, "/api/v1/transactions/"+q, s.do(http.MethodGet, "/api/v1/transactions/"+q, nil),
			http.StatusBadRequest, strings.Split(q, "=")[0][1:])
	}
	apiWantError(t, http.MethodGet, "/api/v1/transactions/?from=2026-2-30",
		s.do(http.MethodGet, "/api/v1/transactions/?from=2026-2-30", nil), http.StatusBadRequest, "from")
	// 不补零的写法照样收，且归一化后与库里的字面量比得上下
	apiWantTitles(t, "少写零的起始", apiTxTitles(t, s, "?from=2026-2-1"), "生日礼物", "年中还款")
}
