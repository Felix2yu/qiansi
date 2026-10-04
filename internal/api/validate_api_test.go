package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/qiansi/app/internal/store"
)

// ===== M10 入参校验 =====
//
// 这里盯的不是「格式好看」，而是脏日期进库之后的后果：
// "2026-02-30" 推不出下一次发生，挂在它上面的纪念日待办既到不了点也清不掉；
// "1990-1-1" 在字符串比较里永远大于 "1990-01-01" 之后的任何真实日期，提醒窗口就此错位。

func valAPICount(t *testing.T, ts *testServer, query string) int {
	t.Helper()
	var n int
	if err := ts.Store.DB.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("COUNT %s: %v", query, err)
	}
	return n
}

func TestAPIValidate_DatesMustExist(t *testing.T) {
	ts := newTestServer(t)
	p := valAPIPerson(t, ts, "校验收件人")

	// 格式对但日子不存在：闰年外的 2-30
	rec := ts.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "不存在的日子", "event_date": "2026-02-30", "participant_ids": []string{p},
	})
	apiWantError(t, "event bad date", "", rec, http.StatusBadRequest, "event_date 不是有效日期")
	if n := valAPICount(t, ts, "SELECT COUNT(*) FROM events"); n != 0 {
		t.Fatalf("被拒的事件不应落库, got %d", n)
	}

	// 少写补零要归一，不能原样存下
	rec = ts.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "少写零", "event_date": "2026-1-5", "participant_ids": []string{p},
	})
	body := apiWantStatus(t, "event pad date", "", rec, http.StatusOK)
	if got := decodeMap(t, rec)["event_date"]; got != "2026-01-05" {
		t.Fatalf("event_date 应归一成 2026-01-05, got %v / %s", got, body)
	}

	// 月/日越界
	rec = ts.do(http.MethodPost, "/api/v1/anniversaries/", map[string]any{
		"title": "越界纪念日", "date": "2026-13-01", "person_id": p,
	})
	apiWantError(t, "anniv bad date", "", rec, http.StatusBadRequest, "date 不是有效日期")
	rec = ts.do(http.MethodPost, "/api/v1/memos/", map[string]any{
		"content": "乱日期", "person_id": p, "said_at": "明天吧",
	})
	apiWantError(t, "memo bad said_at", "", rec, http.StatusBadRequest, "said_at 不是有效时间")
	rec = ts.do(http.MethodPost, "/api/v1/reminders/", map[string]any{
		"title": "乱时间", "due_at": "2026-05-01 09:00",
	})
	apiWantError(t, "reminder bad due_at", "", rec, http.StatusBadRequest, "due_at 不是有效时间")
	rec = ts.do(http.MethodPost, "/api/v1/transactions/", map[string]any{
		"person_id": p, "kind": "loan", "direction": "out", "amount_fen": 100,
		"occurred_at": "2026-05-01", "due_date": "2026-02-30",
	})
	apiWantError(t, "tx bad due_date", "", rec, http.StatusBadRequest, "due_date 不是有效日期")

	// 提醒支持「只到分钟」的字面量，不能因为少了秒就拒掉
	rec = ts.do(http.MethodPost, "/api/v1/reminders/", map[string]any{
		"title": "到分钟", "due_at": "2026-05-01T09:30",
	})
	apiWantStatus(t, "reminder minute", "", rec, http.StatusOK)
}

func TestAPIValidate_BirthdayFeedsAnniversary(t *testing.T) {
	ts := newTestServer(t)

	rec := ts.do(http.MethodPost, "/api/v1/people/", map[string]any{"name": "生日脏", "birthday": "1990-02-30"})
	apiWantError(t, "person bad birthday", "", rec, http.StatusBadRequest, "birthday 不是有效日期")
	if n := valAPICount(t, ts, "SELECT COUNT(*) FROM people"); n != 0 {
		t.Fatalf("被拒的人物不应落库, got %d", n)
	}

	// 生日少写零：落库归一，并且由它派生的纪念日拿到的也是归一后的值
	rec = ts.do(http.MethodPost, "/api/v1/people/", map[string]any{"name": "生日补零", "birthday": "1990-1-1"})
	apiWantStatus(t, "person pad birthday", "", rec, http.StatusOK)
	created := decodeMap(t, rec)
	if got := created["birthday"]; got != "1990-01-01" {
		t.Fatalf("birthday 应归一, got %v", got)
	}
	var annivDate string
	if err := ts.Store.DB.QueryRow(
		"SELECT date FROM anniversaries WHERE person_id=? AND source='birthday'", created["id"]).Scan(&annivDate); err != nil {
		t.Fatalf("birthday 纪念日应已生成: %v", err)
	}
	if annivDate != "1990-01-01" {
		t.Fatalf("纪念日 date = %q, 期望 1990-01-01", annivDate)
	}
}

func TestAPIValidate_TransactionEnumsAndAmount(t *testing.T) {
	ts := newTestServer(t)
	p := valAPIPerson(t, ts, "账本对手")

	good := map[string]any{
		"person_id": p, "kind": "loan", "direction": "out",
		"amount_fen": 5000, "occurred_at": "2026-05-01", "title": "借款",
	}
	rec := ts.do(http.MethodPost, "/api/v1/transactions/", good)
	apiWantStatus(t, "tx create", "", rec, http.StatusOK)
	txID, _ := decodeMap(t, rec)["id"].(string)

	// 金额为 0 或负数：不是「免费的往来」，而是会把欠账口径算成 0 或反向的脏值
	for _, amount := range []int{0, -100} {
		bad := map[string]any{}
		for k, v := range good {
			bad[k] = v
		}
		bad["amount_fen"] = amount
		rec = ts.do(http.MethodPost, "/api/v1/transactions/", bad)
		apiWantError(t, "tx bad amount", "", rec, http.StatusBadRequest, "amount_fen 必须大于 0")
	}

	// 枚举：写错 kind 的记录既不进借还统计，也不进礼物统计，属于谁也认不出来的死角数据
	rec = ts.do(http.MethodPost, "/api/v1/transactions/", map[string]any{
		"person_id": p, "kind": "borrow", "direction": "out", "amount_fen": 100, "occurred_at": "2026-05-01",
	})
	apiWantError(t, "tx bad kind", "", rec, http.StatusBadRequest, "kind 只能是 loan / gift / expense / other")
	rec = ts.do(http.MethodPost, "/api/v1/transactions/", map[string]any{
		"person_id": p, "kind": "loan", "direction": "sideways", "amount_fen": 100, "occurred_at": "2026-05-01",
	})
	apiWantError(t, "tx bad direction", "", rec, http.StatusBadRequest, "direction 只能是 in / out")

	// 缺 person_id 要有中文说明，而不是让 SQLite 的 NOT NULL/FK 原文冲到界面上
	rec = ts.do(http.MethodPost, "/api/v1/transactions/", map[string]any{
		"kind": "gift", "direction": "out", "amount_fen": 100, "occurred_at": "2026-05-01",
	})
	apiWantError(t, "tx no person", "", rec, http.StatusBadRequest, "person_id 必填")

	// PUT 是全量覆盖：金额给 0 会把已有那笔账清零，同样得挡下来
	rec = ts.do(http.MethodPut, "/api/v1/transactions/"+txID, map[string]any{
		"person_id": p, "kind": "loan", "direction": "out", "amount_fen": 0, "occurred_at": "2026-05-01",
	})
	apiWantError(t, "tx update zero", "", rec, http.StatusBadRequest, "amount_fen 必须大于 0")

	// 挡下来之后原值还在
	if got := valAPICount(t, ts, "SELECT COUNT(*) FROM transactions WHERE amount_fen=5000"); got != 1 {
		t.Fatalf("失败的 PUT 不应改动原记录, 命中 %d 条", got)
	}
}

func TestAPIValidate_MemoEnums(t *testing.T) {
	ts := newTestServer(t)
	p := valAPIPerson(t, ts, "对话对象")

	rec := ts.do(http.MethodPost, "/api/v1/memos/", map[string]any{
		"content": "x", "said_at": "2026-05-01", "person_id": p, "speaker": "third-party",
	})
	apiWantError(t, "memo bad speaker", "", rec, http.StatusBadRequest, "speaker 只能是 me / other")
	rec = ts.do(http.MethodPost, "/api/v1/memos/", map[string]any{
		"content": "x", "said_at": "2026-05-01", "person_id": p, "status": "cancelled",
	})
	apiWantError(t, "memo bad status", "", rec, http.StatusBadRequest, "status 只能是 open / fulfilled / broken")
}

// 关联记录不存在时，前端要看到能读的一句话，而不是 FOREIGN KEY constraint failed (787)。
func TestAPIValidate_ForeignKeysReadInChinese(t *testing.T) {
	ts := newTestServer(t)

	cases := []struct {
		name, method, path string
		body               map[string]any
	}{
		{"tx ghost person", http.MethodPost, "/api/v1/transactions/", map[string]any{
			"person_id": "ghost", "kind": "loan", "direction": "out", "amount_fen": 100, "occurred_at": "2026-05-01"}},
		{"memo ghost person", http.MethodPost, "/api/v1/memos/", map[string]any{
			"person_id": "ghost", "content": "x", "said_at": "2026-05-01"}},
		{"anniv ghost person", http.MethodPost, "/api/v1/anniversaries/", map[string]any{
			"person_id": "ghost", "title": "x", "date": "2026-05-01"}},
		{"reminder ghost person", http.MethodPost, "/api/v1/reminders/", map[string]any{
			"person_id": "ghost", "title": "x", "due_at": "2026-05-01T09:00:00"}},
	}
	for _, c := range cases {
		rec := ts.do(c.method, c.path, c.body)
		body := apiWantStatus(t, c.name, c.path, rec, http.StatusBadRequest)
		if !strings.Contains(body, "关联的记录不存在") {
			t.Fatalf("%s: 期望中文外键提示, got %s", c.name, body)
		}
		for _, leak := range []string{"FOREIGN KEY", "constraint", "SQLITE", "no such"} {
			if strings.Contains(body, leak) {
				t.Fatalf("%s: 驱动错误原文泄漏到界面: %s", c.name, body)
			}
		}
	}
}

func valAPIPerson(t *testing.T, ts *testServer, name string) string {
	t.Helper()
	p := &store.Person{Name: name}
	if err := ts.Store.PersonCreate(context.Background(), p); err != nil {
		t.Fatalf("PersonCreate(%s): %v", name, err)
	}
	return p.ID
}
