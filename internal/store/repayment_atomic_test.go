package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// RepaymentCreate 的超付校验在一条 INSERT 里原子完成：
// 应用层的「先读 RepaidFen 再校验」在双击/双标签页并发下两趟都能过检，
// 这里的底线由 SQL 条件兜住。
func TestRepaymentCreateAtomicOverpay(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "老赵")
	loan := &Transaction{PersonID: p.ID, Kind: "loan", Direction: "out", AmountFen: 1000, OccurredAt: "2026-05-01"}
	if err := s.TransactionCreate(ctx, loan); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}

	rp1 := &Repayment{TransactionID: loan.ID, AmountFen: 600, OccurredAt: "2026-06-01"}
	if err := s.RepaymentCreate(ctx, rp1); err != nil {
		t.Fatalf("第一笔还款: %v", err)
	}
	// 600 + 500 > 1000：应用层若已读过旧的 RepaidFen，这条仍应被原子条件拦下
	rp2 := &Repayment{TransactionID: loan.ID, AmountFen: 500, OccurredAt: "2026-06-02"}
	if err := s.RepaymentCreate(ctx, rp2); !errors.Is(err, ErrRepayOverpay) {
		t.Fatalf("超付还款应 ErrRepayOverpay, got %v", err)
	}
	// 恰好还清放行
	rp3 := &Repayment{TransactionID: loan.ID, AmountFen: 400, OccurredAt: "2026-06-03"}
	if err := s.RepaymentCreate(ctx, rp3); err != nil {
		t.Fatalf("恰好还清应放行: %v", err)
	}
	// 交易不存在 → 404 语义，不是超付
	if err := s.RepaymentCreate(ctx, &Repayment{TransactionID: "ghost", AmountFen: 1, OccurredAt: "2026-06-04"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("幽灵交易应 ErrNoRows, got %v", err)
	}
	got, err := s.TransactionGet(ctx, loan.ID)
	if err != nil || got.RepaidFen != 1000 {
		t.Fatalf("RepaidFen 应为 1000, got %+v (%v)", got, err)
	}
}
