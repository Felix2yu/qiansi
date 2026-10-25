package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/qiansi/app/internal/lunar"
	"github.com/qiansi/app/internal/store"
)

// ===== format.go =====

func TestFormatFenToYuan(t *testing.T) {
	cases := []struct {
		fen  int
		want string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{5, "0.05"},
		{10, "0.10"},
		{50, "0.50"},
		{99, "0.99"},
		{100, "1.00"},
		{105, "1.05"},
		{110, "1.10"},
		{12345, "123.45"},
		{1000000, "10000.00"},
		{-1, "-0.01"},
		{-100, "-1.00"},
		{-12345, "-123.45"},
	}
	for _, c := range cases {
		if got := fenToYuan(c.fen); got != c.want {
			t.Errorf("fenToYuan(%d) = %q, want %q", c.fen, got, c.want)
		}
	}
}

func TestFormatItoaAndTwoDigits(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{{0, "0"}, {7, "7"}, {42, "42"}, {1000, "1000"}} {
		if got := itoa(c.n); got != c.want {
			t.Errorf("itoa(%d) = %q, want %q", c.n, got, c.want)
		}
	}
	for _, c := range []struct {
		n    int
		want string
	}{{0, "00"}, {5, "05"}, {9, "09"}, {10, "10"}, {99, "99"}} {
		if got := twoDigits(c.n); got != c.want {
			t.Errorf("twoDigits(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// ===== suggest.go: truncate =====

func TestFormatTruncate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"shorter", "abc", 5, "abc"},
		{"exact", "abcde", 5, "abcde"},
		{"longer", "abcdef", 5, "abcde…"},
		{"empty", "", 3, ""},
		{"cjk within", "牵丝关系", 4, "牵丝关系"},
		{"cjk cut by rune", "一二三四五六七八九十", 4, "一二三四…"},
		{"cjk 30 default", strings.Repeat("记", 40), 30, strings.Repeat("记", 30) + "…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := truncate(c.in, c.n); got != c.want {
				t.Errorf("truncate(%q,%d) = %q, want %q", c.in, c.n, got, c.want)
			}
		})
	}
}

// ===== suggest.go: a.suggest(context.Background()) =====

func hasSuggestion(list []Suggestion, typ, msgPart string) (*Suggestion, bool) {
	for i := range list {
		if list[i].Type == typ && strings.Contains(list[i].Message, msgPart) {
			return &list[i], true
		}
	}
	return nil, false
}

func countType(list []Suggestion, typ string) int {
	n := 0
	for _, s := range list {
		if s.Type == typ {
			n++
		}
	}
	return n
}

func TestSuggestEmptyStore(t *testing.T) {
	ts := newTestServer(t)
	got := ts.suggest(context.Background())
	if len(got) != 0 {
		t.Fatalf("suggest on empty store = %+v, want none", got)
	}
	// HTTP 路由同样返回空数组
	rec := ts.do(http.MethodGet, "/api/v1/dashboard/suggestions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET suggestions => %d: %s", rec.Code, rec.Body.String())
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("body = %q, want []", body)
	}
}

func TestSuggestAllRules(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	st := ts.Store

	// 阿旧：updated_at 挪到很久以前，留给后面的规则挂纪念日与往来
	stale := &store.Person{Name: "阿旧", Grade: 5}
	if err := st.PersonCreate(ctx, stale); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	if _, err := st.CurrentDB().ExecContext(ctx,
		"UPDATE people SET updated_at=? WHERE id=?", "2020-01-01T00:00:00Z", stale.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	// 最近更新的不应出现
	fresh := &store.Person{Name: "常新", Grade: 4}
	if err := st.PersonCreate(ctx, fresh); err != nil {
		t.Fatalf("PersonCreate fresh: %v", err)
	}
	// 归档的不应出现
	arch := &store.Person{Name: "已归档", Grade: 5}
	if err := st.PersonCreate(ctx, arch); err != nil {
		t.Fatalf("PersonCreate arch: %v", err)
	}
	if _, err := st.CurrentDB().ExecContext(ctx, "UPDATE people SET archived=1, updated_at='2019-01-01T00:00:00Z' WHERE id=?", arch.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// 2a. 太阳纪念日：今天（锚点年份任意，按年循环）
	today := time.Now()
	solarAnniv := &store.Anniversary{
		PersonID: stale.ID, Title: "相识纪念", RepeatYearly: true,
		Date: fmt.Sprintf("2000-%02d-%02d", int(today.Month()), today.Day()),
	}
	if err := st.AnniversaryCreate(ctx, solarAnniv); err != nil {
		t.Fatalf("AnniversaryCreate solar: %v", err)
	}
	// 2b. 农历纪念日：在未来 7 天内找一个可稳定往返的日期
	var lunarAnniv *store.Anniversary
	now := lunar.NowLocal()
	for i := 0; i < 7; i++ {
		d := now.AddDays(i)
		lm, ld := lunar.SolarToLunar(d)
		back := lunar.LunarToSolar(lm, ld, now)
		if back.Year == d.Year && back.Month == d.Month && back.Day == d.Day {
			lunarAnniv = &store.Anniversary{
				PersonID: stale.ID, Title: "农历忌日", IsLunar: true, RepeatYearly: true,
				Date: fmt.Sprintf("2000-%02d-%02d", lm, ld),
			}
			break
		}
	}
	if lunarAnniv != nil {
		if err := st.AnniversaryCreate(ctx, lunarAnniv); err != nil {
			t.Fatalf("AnniversaryCreate lunar: %v", err)
		}
	}
	// 远期的不应出现（3 个月后）
	farDate := time.Now().AddDate(0, 3, 0)
	far := &store.Anniversary{PersonID: stale.ID, Title: "远期纪念", RepeatYearly: true,
		Date: fmt.Sprintf("2000-%02d-%02d", int(farDate.Month()), farDate.Day())}
	if err := st.AnniversaryCreate(ctx, far); err != nil {
		t.Fatalf("AnniversaryCreate far: %v", err)
	}

	// 3. 借款未还：借出 123.45 元、已还 3.45 → 待还 120.00
	borrower := &store.Person{Name: "老赖"}
	if err := st.PersonCreate(ctx, borrower); err != nil {
		t.Fatalf("PersonCreate borrower: %v", err)
	}
	loan := &store.Transaction{
		PersonID: borrower.ID, Kind: "loan", Direction: "out",
		AmountFen: 12345, OccurredAt: store.DaysAgoLocal(30),
	}
	if err := st.TransactionCreate(ctx, loan); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}
	if err := st.RepaymentCreate(ctx, &store.Repayment{
		TransactionID: loan.ID, AmountFen: 345, OccurredAt: store.DaysAgoLocal(1),
	}); err != nil {
		t.Fatalf("RepaymentCreate: %v", err)
	}
	// 全额已还 → unpaid==0，被过滤
	paid := &store.Person{Name: "清爽"}
	if err := st.PersonCreate(ctx, paid); err != nil {
		t.Fatalf("PersonCreate paid: %v", err)
	}
	loan2 := &store.Transaction{
		PersonID: paid.ID, Kind: "loan", Direction: "out",
		AmountFen: 1000, OccurredAt: store.DaysAgoLocal(10),
	}
	if err := st.TransactionCreate(ctx, loan2); err != nil {
		t.Fatalf("TransactionCreate paid: %v", err)
	}
	if err := st.RepaymentCreate(ctx, &store.Repayment{
		TransactionID: loan2.ID, AmountFen: 1000, OccurredAt: store.DaysAgoLocal(1),
	}); err != nil {
		t.Fatalf("RepaymentCreate paid: %v", err)
	}

	// 4. 承诺逾期未兑现（含超长内容截断、无关联人物、未到期三种）
	long := strings.Repeat("承诺", 20) // 40 rune
	m1 := &store.Memo{PersonID: stale.ID, Content: long, SaidAt: store.DaysAgoLocal(20),
		IsPromise: true, DueDate: store.DaysAgoLocal(1), Status: "open"}
	if err := st.MemoCreate(ctx, m1); err != nil {
		t.Fatalf("MemoCreate overdue: %v", err)
	}
	m2 := &store.Memo{Content: "无主承诺", SaidAt: store.DaysAgoLocal(20),
		IsPromise: true, DueDate: store.DaysAgoLocal(2), Status: "open"}
	if err := st.MemoCreate(ctx, m2); err != nil {
		t.Fatalf("MemoCreate no person: %v", err)
	}
	m3 := &store.Memo{Content: "明天到期", SaidAt: store.TodayLocal(),
		IsPromise: true, DueDate: time.Now().AddDate(0, 0, 1).Format("2006-01-02"), Status: "open"}
	if err := st.MemoCreate(ctx, m3); err != nil {
		t.Fatalf("MemoCreate future: %v", err)
	}

	list := ts.suggest(context.Background())

	// 规则 1「久未联系」已由联系节奏引擎接管（N5），建议里不该再有这一类
	if _, ok := hasSuggestion(list, "久未联系", "打个招呼"); ok {
		t.Error("久未联系 建议应已下线，改由 /api/v1/contacts/drift 与派生待办承接")
	}
	if countType(list, "久未联系") != 0 {
		t.Errorf("久未联系 count = %d, want 0", countType(list, "久未联系"))
	}

	// 规则 2
	solar, ok := hasSuggestion(list, "纪念日临近", "相识纪念")
	if !ok {
		t.Errorf("missing solar anniversary suggestion: %+v", list)
	} else if solar.PersonID != stale.ID {
		t.Errorf("solar anniversary person = %+v", solar)
	}
	if lunarAnniv != nil {
		if _, ok := hasSuggestion(list, "纪念日临近", "农历忌日"); !ok {
			t.Errorf("missing lunar anniversary suggestion: %+v", list)
		}
	}
	if _, ok := hasSuggestion(list, "纪念日临近", "远期纪念"); ok {
		t.Error("far-away anniversary should not be suggested")
	}

	// 规则 3
	s, ok := hasSuggestion(list, "有借款未还", "待还 ¥120.00")
	if !ok {
		t.Errorf("missing 借款 suggestion: %+v", list)
	} else if s.PersonName != "老赖" {
		t.Errorf("loan suggestion = %+v", s)
	}
	if _, ok := hasSuggestion(list, "有借款未还", "清爽"); ok {
		t.Error("fully repaid person should not appear")
	}

	// 规则 4
	s, ok = hasSuggestion(list, "承诺到期未兑现", store.DaysAgoLocal(1))
	if !ok {
		t.Errorf("missing promise suggestion: %+v", list)
	} else {
		if !strings.HasPrefix(s.Message, strings.Repeat("承诺", 15)+"…") {
			t.Errorf("promise message not truncated: %q", s.Message)
		}
	}
	noPerson, ok := hasSuggestion(list, "承诺到期未兑现", "无主承诺")
	if !ok {
		t.Error("missing no-person promise suggestion")
	} else if noPerson.PersonID != "" || noPerson.PersonName != "" {
		t.Errorf("no-person promise = %+v", noPerson)
	}
	if _, ok := hasSuggestion(list, "承诺到期未兑现", "明天到期"); ok {
		t.Error("future-dated promise should not appear")
	}

	// HTTP 路由与直接调用一致
	rec := ts.do(http.MethodGet, "/api/v1/dashboard/suggestions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET suggestions => %d", rec.Code)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != len(list) {
		t.Errorf("route len = %d, suggest len = %d", len(decoded), len(list))
	}
}
