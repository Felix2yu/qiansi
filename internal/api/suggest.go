package api

import (
	"context"
	"database/sql"

	"github.com/qiansi/app/internal/store"
)

type Suggestion struct {
	Type       string `json:"type"`
	PersonID   string `json:"person_id,omitempty"`
	PersonName string `json:"person_name,omitempty"`
	Message    string `json:"message"`
}

func (a *API) suggest() []Suggestion {
	out := []Suggestion{}
	ctx := context.Background()
	st := a.Store.DB

	// 1. People not updated recently
	rows, err := st.QueryContext(ctx, `SELECT p.id,p.name FROM live_people p
WHERE p.archived=0 AND substr(p.updated_at,1,10) < ?
ORDER BY p.grade DESC, p.updated_at ASC LIMIT 10`, store.DaysAgoLocal(14))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var personID, name string
			if err := rows.Scan(&personID, &name); err == nil {
				out = append(out, Suggestion{
					Type: "久未联系", PersonID: personID, PersonName: name,
					Message: "已有一段时间没有往来了，打个招呼吧",
				})
			}
		}
	}

	// 2. Upcoming anniversaries within 7 days
	//
	// 必须走 NextOccurrence 计算「下一次发生」：直接比较 date(a.date) 只在
	// 建档当年命中一次，之后每年重复的纪念日再也不会出现在建议里。
	if upcoming, err := a.Store.AnniversaryUpcoming(ctx, 7); err == nil {
		for _, r := range upcoming {
			name := r.PersonName
			out = append(out, Suggestion{
				Type: "纪念日临近", PersonID: r.PersonID, PersonName: name,
				Message: r.Title,
			})
		}
	}

	// 3. Unsettled lent money
	//
	// 先按每笔交易扣掉各自还款，再按人汇总。直接在聚合里写 t.id 会让 SQLite
	// 取未定义的某一行，多笔借款时数字是错的。
	// kind='loan' 与首页欠账口径一致：礼物/花销不是借款，不该出现在「待还」里。
	rows, err = st.QueryContext(ctx, `SELECT p.id,p.name,COALESCE(SUM(x.remaining),0) AS unpaid
FROM (
  SELECT t.person_id AS pid,
         t.amount_fen - COALESCE((SELECT SUM(r.amount_fen) FROM repayments r WHERE r.transaction_id=t.id),0) AS remaining
  FROM live_transactions t WHERE t.direction='out' AND t.kind='loan' AND t.settled=0
) x JOIN live_people p ON p.id=x.pid
GROUP BY x.pid ORDER BY unpaid DESC LIMIT 5`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var personID, name string
			var unpaid int
			if err := rows.Scan(&personID, &name, &unpaid); err == nil && unpaid > 0 {
				out = append(out, Suggestion{
					Type: "有借款未还", PersonID: personID, PersonName: name,
					Message: "待还 ¥" + fenToYuan(unpaid),
				})
			}
		}
	}

	// 4. Open promises past due
	rows, err = st.QueryContext(ctx, `SELECT COALESCE(p.id,''), COALESCE(p.name,''), m.content, COALESCE(m.due_date,'')
FROM live_memos m LEFT JOIN live_people p ON p.id=m.person_id
WHERE m.is_promise=1 AND m.status='open' AND m.due_date<>'' AND date(m.due_date) < ?`, store.TodayLocal())
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var personID, name, content, due string
			if err := rows.Scan(&personID, &name, &content, &due); err == nil {
				out = append(out, Suggestion{
					Type: "承诺到期未兑现", PersonID: personID, PersonName: name,
					Message: truncate(content, 30) + "（" + due + "）",
				})
			}
		}
	}

	_ = sql.ErrNoRows
	return out
}

// truncate 按字符（rune）截断，避免把中文按字节切开产生乱码。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
