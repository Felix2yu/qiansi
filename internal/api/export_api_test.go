package api

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/qiansi/app/internal/store"
)

// ===== 结构化导入导出（N3）=====

// exCSV 请求明细导出并解析成行。表头也在返回里：列序本身就是给 Excel 用户的契约。
func exCSV(t *testing.T, s *testServer, query string) [][]string {
	t.Helper()
	rec := s.do(http.MethodGet, "/api/v1/export/csv?"+query, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("导出 %s => %d: %s", query, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\ufeff") {
		t.Fatal("CSV 没有 BOM，Excel 打开中文会乱码")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatalf("CSV 解析失败: %v", err)
	}
	return rows
}

// exEventWithGift 记一场带 800 元礼金的往来，返回事件 id。
func exEventWithGift(t *testing.T, s *testServer, hostID string) string {
	t.Helper()
	rec := s.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "老王儿子婚礼", "event_date": "2026-10-01",
		"participant_ids": []any{hostID},
		"gift_amount_fen": 80000, "gift_direction": "out",
	})
	apiWantStatus(t, http.MethodPost, "/api/v1/events/", rec, http.StatusOK)
	id, _ := decodeMap(t, rec)["id"].(string)
	if id == "" {
		t.Fatalf("新建往来没返回 id: %v", decodeMap(t, rec))
	}
	return id
}

func TestAPIExportCSV(t *testing.T) {
	s := newTestServer(t)
	host := apiCreatePersonMap(t, s, map[string]any{"name": "老王"})
	eventID := exEventWithGift(t, s, host["id"].(string))

	rows := exCSV(t, s, "what=events")
	if rows[0][6] != "礼金（元）" {
		t.Fatalf("表头 = %v", rows[0])
	}
	if len(rows) != 2 || rows[1][1] != "老王儿子婚礼" || rows[1][6] != "800.00" {
		t.Fatalf("往来明细 = %v", rows)
	}
	// 礼金不是「花费」，那一列得空着
	if rows[1][7] != "" {
		t.Fatalf("礼金被重复计成开销: %v", rows[1])
	}

	// 单场礼单：只导这一场宴席的账
	if rows := exCSV(t, s, "what=transactions&event_id="+eventID); len(rows) != 2 {
		t.Fatalf("按事件过滤 = %v", rows)
	}
	if rows := exCSV(t, s, "what=events&year=1999"); len(rows) != 1 {
		t.Fatalf("1999 年不该有往来: %v", rows)
	}
	cd := s.do(http.MethodGet, "/api/v1/export/csv?what=events&year=2026", nil).Header().Get("Content-Disposition")
	if !strings.Contains(cd, `filename="qiansi-events-2026.csv"`) {
		t.Fatalf("下载文件名 = %q", cd)
	}
}

func TestAPIExportCSVRejectsBadQuery(t *testing.T) {
	s := newTestServer(t)
	apiWantError(t, http.MethodGet, "/api/v1/export/csv", s.do(http.MethodGet, "/api/v1/export/csv", nil),
		http.StatusBadRequest, "what")
	apiWantError(t, http.MethodGet, "/api/v1/export/csv?what=events&year=abc",
		s.do(http.MethodGet, "/api/v1/export/csv?what=events&year=abc", nil),
		http.StatusBadRequest, "四位年份")
	apiWantError(t, http.MethodGet, "/api/v1/export/csv?what=events&year=1899",
		s.do(http.MethodGet, "/api/v1/export/csv?what=events&year=1899", nil),
		http.StatusBadRequest, "四位年份")
	apiWantError(t, http.MethodGet, "/api/v1/export/csv?what=people",
		s.do(http.MethodGet, "/api/v1/export/csv?what=people", nil),
		http.StatusBadRequest, "不支持导出")
}

// 全量 JSON 出去、整库回来：换机时靠这一条路，不用再拷 .db。
func TestAPIJSONDumpRoundTrip(t *testing.T) {
	s := newTestServer(t)
	host := apiCreatePersonMap(t, s, map[string]any{"name": "老王"})
	exEventWithGift(t, s, host["id"].(string))

	rec := s.do(http.MethodGet, "/api/v1/export/json", nil)
	apiWantStatus(t, http.MethodGet, "/api/v1/export/json", rec, http.StatusOK)
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="qiansi-full-`) {
		t.Fatalf("下载文件名 = %q", cd)
	}
	dec := json.NewDecoder(strings.NewReader(rec.Body.String()))
	dec.UseNumber()
	var d store.Dump
	if err := dec.Decode(&d); err != nil {
		t.Fatalf("导出文件读不回来: %v", err)
	}
	if d.App != "qiansi" || d.Version != store.DumpVersion {
		t.Fatalf("文件头 = %s/%d", d.App, d.Version)
	}
	// 金额是整数列。float64 会写成 REAL，回来就不是同一个值。
	for _, tab := range d.Tables {
		if tab.Name != "transactions" {
			continue
		}
		amount := -1
		for i, c := range tab.Columns {
			if c == "amount_fen" {
				amount = i
			}
		}
		if len(tab.Rows) == 0 {
			t.Fatal("事件自带的礼金该在导出里")
		}
		if n, ok := tab.Rows[0][amount].(json.Number); !ok || n.String() != "80000" {
			t.Fatalf("amount_fen = %#v, want 整数 80000", tab.Rows[0][amount])
		}
	}

	// 导出之后又多记了一个人，导入必须把它覆盖掉
	apiCreatePersonMap(t, s, map[string]any{"name": "后来加的人"})

	rec = s.do(http.MethodPost, "/api/v1/import/json", d)
	apiWantStatus(t, http.MethodPost, "/api/v1/import/json", rec, http.StatusOK)
	got := decodeMap(t, rec)
	if got["rows"].(float64) < 1 || got["tables"].(float64) < 10 {
		t.Fatalf("导入统计 = %v", got)
	}
	// 覆盖全库前必须先归档，否则导错了就再也捞不回来
	snapshot, _ := got["snapshot"].(string)
	if snapshot == "" {
		t.Fatalf("导入没留归档: %v", got)
	}
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("归档文件不在: %v", err)
	}

	names := apiNames(t, apiArray(s, "/api/v1/people/"))
	if !names["老王"] || names["后来加的人"] {
		t.Fatalf("导入后的名单 = %v", names)
	}
}

func TestAPIImportJSONRejects(t *testing.T) {
	s := newTestServer(t)
	apiCreatePersonMap(t, s, map[string]any{"name": "老王"})

	// 别家程序的文件
	rec := s.do(http.MethodPost, "/api/v1/import/json", map[string]any{
		"app": "weixin", "dump_version": float64(store.DumpVersion), "tables": []any{},
	})
	apiWantError(t, http.MethodPost, "/api/v1/import/json", rec, http.StatusBadRequest, "不是牵丝")

	// 半截 JSON
	rec = s.raw(http.MethodPost, "/api/v1/import/json", []byte(`{"app":"qiansi",`), "application/json")
	apiWantError(t, http.MethodPost, "/api/v1/import/json", rec, http.StatusBadRequest, "JSON")

	// 不认识的表
	rec = s.do(http.MethodPost, "/api/v1/import/json", map[string]any{
		"app": "qiansi", "dump_version": float64(store.DumpVersion),
		"tables": []any{map[string]any{"table": "sqlite_master",
			"columns": []any{"name"}, "rows": []any{[]any{"x"}}}},
	})
	apiWantError(t, http.MethodPost, "/api/v1/import/json", rec, http.StatusBadRequest, "不认识的表")

	if n := egAPICount(t, s, "SELECT COUNT(*) FROM people"); n != 1 {
		t.Fatalf("被拒的导入动了库：人数 = %d, want 1", n)
	}
}
