package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/qiansi/app/internal/store"
)

// 见面总览端点：口径（哪些类型算见面）与排序在 store 层已测，
// 这里盯 HTTP 这一层的形状与事件类型 is_meeting 的透传。

func TestAPIMeetingOverview(t *testing.T) {
	s := newTestServer(t)

	p := &store.Person{Name: "甲"}
	if err := s.Store.PersonCreate(t.Context(), p); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	e := &store.Event{Title: "茶叙", EventDate: "2026-01-01"}
	if err := s.Store.EventCreate(t.Context(), e, []string{p.ID}, nil); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}

	body := apiWantStatus(t, http.MethodGet, "/api/v1/meetings/overview",
		s.do(http.MethodGet, "/api/v1/meetings/overview", nil), http.StatusOK)
	var rows []store.MeetingOverviewRow
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if len(rows) != 1 || rows[0].PersonID != p.ID || rows[0].MeetingCount != 1 {
		t.Fatalf("overview = %+v，应只有甲的一行", rows)
	}
	if rows[0].LastMeeting != "2026-01-01" || rows[0].DaysSince <= 0 {
		t.Errorf("甲行 = %+v", rows[0])
	}
	// 只见过一次：平均间隔不带（omitempty），别给前端一个 0 天的假节奏
	if rows[0].AvgGapDays != 0 {
		t.Errorf("只见一次不该有平均间隔，得到 %d", rows[0].AvgGapDays)
	}
}

func TestAPIEventTypeIsMeeting(t *testing.T) {
	s := newTestServer(t)

	apiWantStatus(t, http.MethodPost, "/api/v1/event-types",
		s.do(http.MethodPost, "/api/v1/event-types", map[string]any{
			"name": "电话微信", "color": "#222222", "icon": "calendar",
			"is_default": false, "sort_order": 0, "is_meeting": false,
		}), http.StatusOK)

	list := apiArrayStr(t, s, http.MethodGet, "/api/v1/event-types")
	var types []store.EventType
	if err := json.Unmarshal([]byte(list), &types); err != nil {
		t.Fatalf("decode event-types: %v", err)
	}
	for _, et := range types {
		if et.Name == "电话微信" && et.IsMeeting {
			t.Fatalf("关掉算见面没生效: %+v", et)
		}
	}
}
