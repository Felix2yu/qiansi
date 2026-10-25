package store

import (
	"context"
	"testing"
)

// 见面口径（N6）：哪些往来算一次见面、间隔链怎么串、总览怎么排。
// 口径：无类型一律算见面；事件类型关掉「算见面」（电话微信等）后
// 既不显示间隔，也不把链打断。

func mtEvent(t *testing.T, s *Store, title, date string, typeID *int, personID string) *Event {
	t.Helper()
	e := &Event{Title: title, EventDate: date, TypeID: typeID}
	if err := s.EventCreate(context.Background(), e, []string{personID}, nil); err != nil {
		t.Fatalf("EventCreate(%s): %v", title, err)
	}
	return e
}

func mtTimelineItem(t *testing.T, s *Store, personID, title string) *TimelineItem {
	t.Helper()
	list, err := s.PersonTimeline(context.Background(), personID)
	if err != nil {
		t.Fatalf("PersonTimeline: %v", err)
	}
	for _, ti := range list {
		if ti.Title == title {
			return ti
		}
	}
	t.Fatalf("时间线里找不到 %q", title)
	return nil
}

func TestMeetingStatsAndTimeline(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// 迁移 017 的默认值：新类型不填 is_meeting 就是 1（算见面）
	if _, err := s.CurrentDB().Exec(`INSERT INTO event_types(id,name,color) VALUES(10,'见面聊天','#111111')`); err != nil {
		t.Fatalf("seed type 10: %v", err)
	}
	var def int
	if err := s.CurrentDB().QueryRow(`SELECT is_meeting FROM event_types WHERE id=10`).Scan(&def); err != nil || def != 1 {
		t.Fatalf("is_meeting 默认值 = %d(err=%v)，应为 1", def, err)
	}
	if _, err := s.CurrentDB().Exec(`INSERT INTO event_types(id,name,color,is_meeting) VALUES(20,'电话微信','#222222',0)`); err != nil {
		t.Fatalf("seed type 20: %v", err)
	}

	jia := evMustPerson(t, s, "甲")
	yi := evMustPerson(t, s, "乙")
	bing := evMustPerson(t, s, "丙")
	ding := evMustPerson(t, s, "丁")

	mt := 10
	mtEvent(t, s, "元旦茶叙", "2026-01-01", nil, jia.ID)           // 无类型 → 算
	mtEvent(t, s, "小聚", "2026-01-11", &mt, jia.ID)             // 算见面的类型
	mtEvent(t, s, "拜年电话", "2026-01-16", nil, jia.ID)           // 无类型的电话——口径算见面，先记着
	phone := mtEvent(t, s, "拜年电话2", "2026-01-17", nil, jia.ID) // 稍后改成不算见面的类型
	if _, err := s.CurrentDB().Exec(`UPDATE events SET type_id=20 WHERE id=?`, phone.ID); err != nil {
		t.Fatalf("把拜年电话2改成电话类型: %v", err)
	}
	// 同一天两场：created_at 决定先后，后一场间隔记 0
	early := mtEvent(t, s, "午饭", "2026-01-21", &mt, jia.ID)
	late := mtEvent(t, s, "晚饭", "2026-01-21", &mt, jia.ID)
	if _, err := s.CurrentDB().Exec(`UPDATE events SET created_at='2026-01-21T01:00:00Z' WHERE id=?`, early.ID); err != nil {
		t.Fatalf("固定午饭 created_at: %v", err)
	}
	if _, err := s.CurrentDB().Exec(`UPDATE events SET created_at='2026-01-21T02:00:00Z' WHERE id=?`, late.ID); err != nil {
		t.Fatalf("固定晚饭 created_at: %v", err)
	}

	mtEvent(t, s, "乙的初见", "2026-01-05", nil, yi.ID)
	mtEvent(t, s, "丁的见面", "2026-01-02", nil, ding.ID)
	if _, err := s.CurrentDB().Exec(`UPDATE people SET archived=1 WHERE id=?`, ding.ID); err != nil {
		t.Fatalf("归档丁: %v", err)
	}

	t.Run("间隔链", func(t *testing.T) {
		cases := []struct {
			title string
			want  *int // nil = 不带间隔
		}{
			{"元旦茶叙", nil},       // 最早一场，没有「上一次」
			{"小聚", intPtr(10)},  // 01-01 → 01-11
			{"拜年电话", intPtr(5)}, // 01-11 → 01-16（无类型，算见面）
			{"拜年电话2", nil},      // 不算见面的类型：不显示间隔
			{"午饭", intPtr(5)},   // 01-16 → 01-21（不算见面的拜年电话2 不打断链）
			{"晚饭", intPtr(0)},   // 同一天后一场
		}
		for _, c := range cases {
			item := mtTimelineItem(t, s, jia.ID, c.title)
			if c.want == nil {
				if item.GapDays != nil {
					t.Errorf("%s 不应带间隔，得到 %d", c.title, *item.GapDays)
				}
			} else if item.GapDays == nil {
				t.Errorf("%s 应带间隔 %d，得到 nil", c.title, *c.want)
			} else if *item.GapDays != *c.want {
				t.Errorf("%s 间隔 = %d，应为 %d", c.title, *item.GapDays, *c.want)
			}
		}
	})

	t.Run("按人统计", func(t *testing.T) {
		stats, err := s.PersonStatsFor(ctx, []string{jia.ID, yi.ID, bing.ID})
		if err != nil {
			t.Fatalf("PersonStatsFor: %v", err)
		}
		j := stats[jia.ID]
		if j == nil || j.MeetingCount != 5 {
			t.Fatalf("甲 MeetingCount = %+v，应为 5 场（拜年电话2 不算）", j)
		}
		if j.LastMeeting != "2026-01-21" {
			t.Errorf("甲 LastMeeting = %q，应为 2026-01-21", j.LastMeeting)
		}
		// 首见 01-01、末见 01-21 共 20 天 ÷ 4 段 = 5 天
		if j.AvgMeetingGapDays != 5 {
			t.Errorf("甲 AvgMeetingGapDays = %d，应为 5", j.AvgMeetingGapDays)
		}
		y := stats[yi.ID]
		if y == nil || y.MeetingCount != 1 || y.AvgMeetingGapDays != 0 {
			t.Errorf("乙统计 = %+v，应为 1 次且无平均间隔", y)
		}
		if b := stats[bing.ID]; b == nil || b.MeetingCount != 0 || b.LastMeeting != "" {
			t.Errorf("丙没有任何往来，见面统计应为零值，得到 %+v", b)
		}
	})

	t.Run("总览", func(t *testing.T) {
		rows, err := s.MeetingOverview(ctx)
		if err != nil {
			t.Fatalf("MeetingOverview: %v", err)
		}
		var names []string
		for _, r := range rows {
			names = append(names, r.Name)
		}
		// 丙没见过、丁已归档，都不该出现；乙末见（01-05）比甲（01-21）更久，排最前
		if len(rows) != 2 || names[0] != "乙" || names[1] != "甲" {
			t.Fatalf("总览 = %v，应为 [乙 甲]", names)
		}
		a := rows[1]
		if a.MeetingCount != 5 || a.LastMeeting != "2026-01-21" || a.AvgGapDays != 5 {
			t.Errorf("甲总览行 = %+v", a)
		}
		if a.DaysSince <= 0 {
			t.Errorf("甲 DaysSince = %d，末见在几个月前，应为正数", a.DaysSince)
		}
	})
}

func intPtr(n int) *int { return &n }

func TestMeetingGapAvgRounding(t *testing.T) {
	cases := []struct {
		first, last string
		count       int
		want        int
	}{
		{"2026-01-01", "2026-01-31", 2, 30}, // 整除
		{"2026-01-01", "2026-01-02", 1, 0},  // 只见一次没有间隔
		{"2026-01-01", "2026-01-21", 4, 7},  // 20/3 四舍五入
		{"", "2026-01-21", 3, 0},            // 日期读不懂宁可不算
		{"2026-01-21", "2026-01-01", 2, 0},  // 末见早于首见（理论不可能）不算
	}
	for _, c := range cases {
		if got := avgMeetingGapDays(c.first, c.last, c.count); got != c.want {
			t.Errorf("avgMeetingGapDays(%q,%q,%d) = %d，应为 %d", c.first, c.last, c.count, got, c.want)
		}
	}
}

func TestEventTypeIsMeetingPassthrough(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tel := &EventType{Name: "电话微信", IsMeeting: false}
	if err := s.EventTypeUpsert(ctx, tel); err != nil {
		t.Fatalf("新建类型: %v", err)
	}
	list, err := s.EventTypeList(ctx)
	if err != nil {
		t.Fatalf("EventTypeList: %v", err)
	}
	var got *EventType
	for _, et := range list {
		if et.Name == "电话微信" {
			got = et
		}
	}
	if got == nil || got.IsMeeting {
		t.Fatalf("新建时关掉算见面没生效: %+v", got)
	}
	got.IsMeeting = true
	if err := s.EventTypeUpsert(ctx, got); err != nil {
		t.Fatalf("改回算见面: %v", err)
	}
	list, _ = s.EventTypeList(ctx)
	for _, et := range list {
		if et.Name == "电话微信" && !et.IsMeeting {
			t.Fatalf("改回算见面没生效: %+v", et)
		}
	}
}
