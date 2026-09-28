// Package lunar provides thin helpers on top of github.com/6tail/lunar-go
// for the 牵丝 app's anniversary/reminder flows.
//
// Time semantics (per ExperienceRecall guidance):
//   - solarInput is the single-source-of-truth for comparisons.
//   - lunar "date" strings are stored in DB as "YYYY-MM-DD" where YYYY can be any
//     anchor year (typically birth/establishment year) — the month/day are the
//     only meaningful fields for annual anniversaries.
//   - All returned "solar date" values are Gregorian dates, formatted as
//     "YYYY-MM-DD", suitable for SQLite DATE comparisons.
package lunar

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	cal "github.com/6tail/lunar-go/calendar"
)

// YMD is a small year/month/day triple.
type YMD struct {
	Year  int
	Month int
	Day   int
}

// ParseDate parses a "YYYY-MM-DD" string into YMD.
// Returns a zero YMD if the input is invalid.
func ParseDate(s string) YMD {
	parts := strings.SplitN(strings.TrimSpace(s), "-", 3)
	if len(parts) != 3 {
		return YMD{}
	}
	y, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	d, _ := strconv.Atoi(parts[2])
	return YMD{Year: y, Month: m, Day: d}
}

// String renders YMD as "YYYY-MM-DD".
func (y YMD) String() string {
	if y.Year == 0 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", y.Year, y.Month, y.Day)
}

// NowLocal returns today's local date as YMD.
func NowLocal() YMD {
	t := time.Now()
	return YMD{t.Year(), int(t.Month()), t.Day()}
}

// AddDays returns a new YMD shifted by `n` days (negative = past).
// Normalizes year/month/day overflow correctly.
func (y YMD) AddDays(n int) YMD {
	t := time.Date(y.Year, time.Month(y.Month), y.Day, 0, 0, 0, 0, time.UTC)
	t = t.AddDate(0, 0, n)
	return YMD{t.Year(), int(t.Month()), t.Day()}
}

// LunarToSolar converts a lunar (month, day) pair — repeated every year — into
// the **next** Gregorian occurrence on or after `today`.
//
// `anchorYear` is only used to seed the first call; the returned event is
// always annual (repeat_yearly=true semantics). The anchor date's year value
// is ignored (we iterate from current year up).
func LunarToSolar(lunarMonth, lunarDay int, today YMD) YMD {
	for offset := 0; offset < 3; offset++ {
		year := today.Year + offset
		l := cal.NewLunar(year, lunarMonth, lunarDay, 12, 0, 0)
		s := l.GetSolar()
		solar := YMD{s.GetYear(), s.GetMonth(), s.GetDay()}
		if !solar.Before(today) {
			return solar
		}
	}
	// Fallback: one year ahead (very edge case)
	l := cal.NewLunar(today.Year+1, lunarMonth, lunarDay, 12, 0, 0)
	s := l.GetSolar()
	return YMD{s.GetYear(), s.GetMonth(), s.GetDay()}
}

// SolarToLunar converts a Gregorian YMD to its lunar month and day.
// Returns (lunarMonth, lunarDay).
func SolarToLunar(solar YMD) (int, int) {
	s := cal.NewSolar(solar.Year, solar.Month, solar.Day, 12, 0, 0)
	l := s.GetLunar()
	return l.GetMonth(), l.GetDay()
}

// DaysBetween returns abs(a-b) days. Uses time.Time; timezone-independent.
func DaysBetween(a, b YMD) int {
	ta := time.Date(a.Year, time.Month(a.Month), a.Day, 0, 0, 0, 0, time.UTC)
	tb := time.Date(b.Year, time.Month(b.Month), b.Day, 0, 0, 0, 0, time.UTC)
	d := tb.Sub(ta).Hours() / 24
	if d < 0 {
		return int(-d)
	}
	return int(d)
}

// Before compares two YMDs lexicographically (year→month→day).
func (a YMD) Before(b YMD) bool {
	if a.Year != b.Year {
		return a.Year < b.Year
	}
	if a.Month != b.Month {
		return a.Month < b.Month
	}
	return a.Day < b.Day
}

// NextOccurrence returns the next Gregorian date for an anniversary,
// handling both solar and lunar forms.
//
//   - If `isLunar` is false, just compare month/day directly (handle year wrap).
//   - If `isLunar` is true, call LunarToSolar.
func NextOccurrence(date YMD, isLunar bool, today YMD) YMD {
	if isLunar {
		return LunarToSolar(date.Month, date.Day, today)
	}
	// Solar: same month/day annually
	for offset := 0; offset < 3; offset++ {
		candidate := YMD{today.Year + offset, date.Month, date.Day}
		if !candidate.Before(today) {
			return candidate
		}
	}
	return YMD{today.Year + 1, date.Month, date.Day}
}

// RemindDates parses a "7,3,1,0" style remind_days list and returns the
// reminder offsets in ascending order.
func RemindDates(s string) []int {
	out := []int{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	// Sort ascending so "0" (event day) comes last for most-compare flow
	sortInts(out)
	return out
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		x := a[i]
		j := i - 1
		for j >= 0 && a[j] > x {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = x
	}
}
