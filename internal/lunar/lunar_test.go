package lunar

import (
	"testing"
	"time"
)

func TestLunarParseDate(t *testing.T) {
	cases := []struct {
		in   string
		want YMD
	}{
		{"2024-02-10", YMD{2024, 2, 10}},
		{" 2024-02-10 ", YMD{2024, 2, 10}}, // 两侧空白
		{"1990-1-2", YMD{1990, 1, 2}},      // 非零填充也可解析
		{"", YMD{}},
		{"2024", YMD{}},
		{"2024-02", YMD{}},
		{"not-a-date", YMD{0, 0, 0}},     // 三段但全非数字 → 零值
		{"2024--01-02", YMD{2024, 0, 0}}, // 第三段非纯数字 → day=0
	}
	for _, c := range cases {
		if got := ParseDate(c.in); got != c.want {
			t.Errorf("ParseDate(%q)=%+v want %+v", c.in, got, c.want)
		}
	}
}

func TestLunarYMDString(t *testing.T) {
	if got := (YMD{}).String(); got != "" {
		t.Errorf("零值应返回空串, got %q", got)
	}
	if got := (YMD{2024, 2, 10}).String(); got != "2024-02-10" {
		t.Errorf("got %q", got)
	}
	if got := (YMD{7, 1, 2}).String(); got != "0007-01-02" {
		t.Errorf("年份应零填充到 4 位, got %q", got)
	}
}

func TestLunarNowLocal(t *testing.T) {
	now := YMDFromTime(time.Now())
	got := NowLocal()
	if got != now {
		t.Errorf("NowLocal()=%+v want %+v", got, now)
	}
}

// YMDFromTime 测试辅助：time.Time → YMD
func YMDFromTime(t time.Time) YMD {
	return YMD{t.Year(), int(t.Month()), t.Day()}
}

func TestLunarAddDays(t *testing.T) {
	cases := []struct {
		base YMD
		n    int
		want YMD
	}{
		{YMD{2024, 2, 28}, 1, YMD{2024, 2, 29}},  // 闰年
		{YMD{2024, 2, 29}, 1, YMD{2024, 3, 1}},   // 月溢出
		{YMD{2024, 12, 31}, 1, YMD{2025, 1, 1}},  // 年溢出
		{YMD{2024, 1, 1}, -1, YMD{2023, 12, 31}}, // 负数向前
		{YMD{2024, 6, 15}, 0, YMD{2024, 6, 15}},
		{YMD{2024, 1, 1}, 365, YMD{2024, 12, 31}}, // 闰年 366 天
	}
	for _, c := range cases {
		if got := c.base.AddDays(c.n); got != c.want {
			t.Errorf("%+v.AddDays(%d)=%+v want %+v", c.base, c.n, got, c.want)
		}
	}
}

func TestLunarBefore(t *testing.T) {
	cases := []struct {
		a, b YMD
		want bool
	}{
		{YMD{2024, 1, 1}, YMD{2025, 12, 31}, true}, // 年比较
		{YMD{2024, 2, 1}, YMD{2024, 3, 1}, true},   // 月比较
		{YMD{2024, 3, 1}, YMD{2024, 3, 2}, true},   // 日比较
		{YMD{2024, 3, 2}, YMD{2024, 3, 2}, false},  // 相等
		{YMD{2025, 1, 1}, YMD{2024, 12, 31}, false},
		{YMD{2024, 4, 1}, YMD{2024, 3, 31}, false},
		{YMD{2024, 3, 3}, YMD{2024, 3, 2}, false},
	}
	for _, c := range cases {
		if got := c.a.Before(c.b); got != c.want {
			t.Errorf("%+v.Before(%+v)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestLunarDaysBetween(t *testing.T) {
	cases := []struct {
		a, b YMD
		want int
	}{
		{YMD{2024, 1, 1}, YMD{2024, 1, 31}, 30},
		{YMD{2024, 1, 31}, YMD{2024, 1, 1}, 30}, // 绝对值
		{YMD{2024, 1, 1}, YMD{2024, 1, 1}, 0},
		{YMD{2023, 12, 31}, YMD{2024, 1, 1}, 1},
		{YMD{2024, 2, 28}, YMD{2024, 3, 1}, 2}, // 跨闰日
		{YMD{2020, 1, 1}, YMD{2024, 1, 1}, 1461},
	}
	for _, c := range cases {
		if got := DaysBetween(c.a, c.b); got != c.want {
			t.Errorf("DaysBetween(%+v,%+v)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

// 公历日期与农历互转：取若干已知锚点（春节、中秋等）
func TestLunarLunarToSolar(t *testing.T) {
	cases := []struct {
		lm, ld int
		today  YMD
		want   YMD
	}{
		{1, 1, YMD{2024, 1, 1}, YMD{2024, 2, 10}},  // 2024 春节
		{1, 1, YMD{2025, 1, 1}, YMD{2025, 1, 29}},  // 2025 春节
		{1, 1, YMD{2026, 1, 1}, YMD{2026, 2, 17}},  // 2026 春节
		{8, 15, YMD{2024, 1, 1}, YMD{2024, 9, 17}}, // 2024 中秋
		{5, 1, YMD{2024, 1, 1}, YMD{2024, 6, 6}},   // 2024 五月初一
		{1, 1, YMD{2024, 2, 10}, YMD{2024, 2, 10}}, // today 恰为发生日 → 返回当天
		{1, 1, YMD{2024, 2, 11}, YMD{2025, 1, 29}}, // 已过 → 滚到下一年
		{12, 29, YMD{2025, 1, 1}, YMD{2026, 2, 16}},
		{1, 30, YMD{2025, 1, 1}, YMD{2025, 2, 27}}, // 2025 正月为大月，有三十
		{1, 1, YMD{1984, 1, 1}, YMD{1984, 2, 2}},   // 1984 春节
	}
	for _, c := range cases {
		if got := LunarToSolar(c.lm, c.ld, c.today); got != c.want {
			t.Errorf("LunarToSolar(%d,%d,%+v)=%+v want %+v", c.lm, c.ld, c.today, got, c.want)
		}
	}
}

// 「腊月三十」逢小月、闰月（负数月）这类换算不了的输入不再 panic：
// 日超出的落到该月最后一天，月不存在的返回零值（调用方按「没有下一次」处理）。
// lunar-go v1.4.6 的 NewLunar 对这些输入直接 panic，包装层必须自己兜住。
func TestLunarLunarToSolarShortMonthAndBadMonth(t *testing.T) {
	if days := daysInLunarMonth(2024, 12); days != 29 {
		t.Fatalf("前提不成立：2024 腊月应有 29 天，实际 %d", days)
	}
	// 三十不存在 → 折到廿九；期望值用同一年表独立换算，验证折算而非只验证不崩
	want, ok := lunarYMD(2024, 12, 29)
	if !ok {
		t.Fatal("2024 腊月廿九应可换算")
	}
	if got := LunarToSolar(12, 30, YMD{2024, 1, 1}); got != want {
		t.Errorf("小月三十应折到廿九: got %+v want %+v", got, want)
	}
	if got := LunarToSolar(-5, 1, YMD{2024, 1, 1}); got != (YMD{}) {
		t.Errorf("闰月不存在应返回零值: got %+v", got)
	}
	if got := LunarToSolar(13, 1, YMD{2024, 1, 1}); got != (YMD{}) {
		t.Errorf("月越界应返回零值: got %+v", got)
	}
	// 零值 YMD.String() 为空串，调用方（anniversaryOccurrence）据此跳过该行
	if got := LunarToSolar(13, 1, YMD{2024, 1, 1}).String(); got != "" {
		t.Errorf("零值应渲染为空串: got %q", got)
	}
}

func TestLunarSolarToLunar(t *testing.T) {
	cases := []struct {
		solar YMD
		m, d  int
	}{
		{YMD{2024, 2, 10}, 1, 1},   // 2024 春节
		{YMD{2025, 1, 29}, 1, 1},   // 2025 春节
		{YMD{2025, 1, 28}, 12, 29}, // 2025 除夕（2024 腊月只有廿九）
		{YMD{2023, 1, 22}, 1, 1},
		{YMD{2024, 1, 1}, 11, 20},
		{YMD{2024, 9, 17}, 8, 15}, // 2024 中秋
		{YMD{2024, 10, 1}, 8, 29},
		{YMD{2023, 3, 21}, 2, 30},
		{YMD{2026, 2, 17}, 1, 1},
	}
	for _, c := range cases {
		m, d := SolarToLunar(c.solar)
		if m != c.m || d != c.d {
			t.Errorf("SolarToLunar(%+v)=(%d,%d) want (%d,%d)", c.solar, m, d, c.m, c.d)
		}
	}
	// 互转往返：lunar -> solar -> lunar
	for _, lm := range []struct{ m, d int }{{1, 1}, {5, 5}, {8, 15}, {9, 9}, {12, 8}} {
		s := LunarToSolar(lm.m, lm.d, YMD{2025, 1, 1})
		m2, d2 := SolarToLunar(s)
		if m2 != lm.m || d2 != lm.d {
			t.Errorf("往返不一致: (%d,%d) -> %s -> (%d,%d)", lm.m, lm.d, s.String(), m2, d2)
		}
	}
}

func TestLunarNextOccurrenceSolar(t *testing.T) {
	cases := []struct {
		date, today YMD
		want        YMD
	}{
		{YMD{1990, 3, 5}, YMD{2024, 1, 1}, YMD{2024, 3, 5}}, // 今年未到
		{YMD{1990, 3, 5}, YMD{2024, 3, 5}, YMD{2024, 3, 5}}, // 恰为今天
		{YMD{1990, 3, 5}, YMD{2024, 3, 6}, YMD{2025, 3, 5}}, // 已过 → 明年
		{YMD{1990, 12, 31}, YMD{2024, 1, 1}, YMD{2024, 12, 31}},
		{YMD{1990, 2, 29}, YMD{2024, 3, 1}, YMD{2025, 2, 29}}, // 数值上滚到下一年的 2-29
	}
	for _, c := range cases {
		if got := NextOccurrence(c.date, false, c.today); got != c.want {
			t.Errorf("NextOccurrence(%+v,false,%+v)=%+v want %+v", c.date, c.today, got, c.want)
		}
	}
}

func TestLunarNextOccurrenceLunar(t *testing.T) {
	cases := []struct {
		date, today YMD
		want        YMD
	}{
		// 农历正月初一（anchor 年份被忽略，只看月/日）
		{YMD{1990, 1, 1}, YMD{2025, 2, 1}, YMD{2026, 2, 17}},
		{YMD{1990, 1, 1}, YMD{2025, 1, 1}, YMD{2025, 1, 29}},
		{YMD{2023, 1, 1}, YMD{2024, 2, 10}, YMD{2024, 2, 10}},
		{YMD{2000, 8, 15}, YMD{2024, 1, 1}, YMD{2024, 9, 17}},
	}
	for _, c := range cases {
		if got := NextOccurrence(c.date, true, c.today); got != c.want {
			t.Errorf("NextOccurrence(%+v,true,%+v)=%+v want %+v", c.date, c.today, got, c.want)
		}
	}
}

func TestLunarRemindDates(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{"7,3,1,0", []int{0, 1, 3, 7}},
		{"", []int{}},
		{"5", []int{5}},
		{" 10 , 2 ,, x, -1 ", []int{-1, 2, 10}}, // 非法项跳过，负数保留
		{"3,3,1", []int{1, 3, 3}},               // 重复项保留
		{"abc", []int{}},
	}
	for _, c := range cases {
		got := RemindDates(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("RemindDates(%q)=%v want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("RemindDates(%q)=%v want %v", c.in, got, c.want)
			}
		}
	}
}

func TestLunarSortInts(t *testing.T) {
	cases := [][]int{
		{},
		{1},
		{3, 1, 2},
		{5, 4, 3, 2, 1},
		{2, 2, 1},
		{-1, 0, -5},
	}
	for _, a := range cases {
		sortInts(a)
		for i := 1; i < len(a); i++ {
			if a[i-1] > a[i] {
				t.Errorf("sortInts 结果未升序: %v", a)
			}
		}
	}
}
