package api

import (
	"net/http"
	"testing"
)

// 编辑账目时结清标记跟随「本金 − 已还」的算术口径校正（resyncSettledAfterEdit）：
//   - 已还清后把本金改大：未还差额必须重新出现（settled 翻回 false）；
//   - 手勾「不用还了」的未结清账：保存后不被翻回去；
//   - 已还清却标未结清：保存即校正为结清。
func TestTxEditResyncsSettled(t *testing.T) {
	ts := newTestServer(t)
	pid := apiMustCreate(t, ts, "/api/v1/people/", map[string]any{"name": "老周"})

	mkLoan := func(amount int) string {
		rec := ts.do(http.MethodPost, "/api/v1/transactions/", map[string]any{
			"person_id": pid, "kind": "loan", "direction": "out", "amount_fen": amount,
			"title": "借款", "occurred_at": "2026-05-01",
		})
		if rec.Code != 200 {
			t.Fatalf("建借据失败: %d %s", rec.Code, rec.Body.String())
		}
		return evAPIStr(decodeMap(t, rec), "id")
	}
	repay := func(txID string, amount int) {
		if rec := ts.do(http.MethodPost, "/api/v1/transactions/"+txID+"/repayments", map[string]any{"amount_fen": amount}); rec.Code != 200 {
			t.Fatalf("还款失败: %d %s", rec.Code, rec.Body.String())
		}
	}
	edit := func(txID string, body map[string]any) map[string]any {
		rec := ts.do(http.MethodPut, "/api/v1/transactions/"+txID, body)
		if rec.Code != 200 {
			t.Fatalf("编辑失败: %d %s", rec.Code, rec.Body.String())
		}
		return decodeMap(t, rec)
	}

	t.Run("还清后改大本金：未还差额重新出现", func(t *testing.T) {
		txID := mkLoan(1000)
		repay(txID, 1000)
		after := edit(txID, map[string]any{
			"person_id": pid, "kind": "loan", "direction": "out", "amount_fen": 2000,
			"title": "借款", "occurred_at": "2026-05-01", "settled": true,
		})
		if after["settled"].(bool) {
			t.Fatal("本金改成 2000 后仍标记已结清，欠款会从统计里消失")
		}
	})

	t.Run("手勾「不用还了」的差额保留", func(t *testing.T) {
		txID := mkLoan(1000)
		repay(txID, 400)
		after := edit(txID, map[string]any{
			"person_id": pid, "kind": "loan", "direction": "out", "amount_fen": 1000,
			"title": "借款", "occurred_at": "2026-05-01", "settled": true,
		})
		if !after["settled"].(bool) {
			t.Fatal("用户手勾的结清（这笔不用还了）不应被翻回去")
		}
	})

	t.Run("已还清却标未结清：校正为结清", func(t *testing.T) {
		txID := mkLoan(1000)
		repay(txID, 1000)
		after := edit(txID, map[string]any{
			"person_id": pid, "kind": "loan", "direction": "out", "amount_fen": 1000,
			"title": "借款", "occurred_at": "2026-05-01", "settled": false,
		})
		if !after["settled"].(bool) {
			t.Fatal("已还清的账不该停留在未结清")
		}
		if evAPIStr(after, "settled_at") == "" {
			t.Fatal("自动校正为结清时应补上结清时间")
		}
	})
}
