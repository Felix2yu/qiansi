package api

import (
	"context"
	"database/sql"
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
	rows, err := st.QueryContext(ctx, `SELECT p.id,p.name FROM people p
WHERE p.archived=0 AND p.updated_at < datetime('now','-14 day')
ORDER BY p.grade DESC, p.updated_at ASC LIMIT 10`)
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
	rows, err = st.QueryContext(ctx, `SELECT COALESCE(p.id,''), COALESCE(p.name,''), a.title, a.date, a.is_lunar
FROM anniversaries a LEFT JOIN people p ON p.id=a.person_id
WHERE date(a.date) BETWEEN date('now') AND date('now','+7 day')
ORDER BY date(a.date)`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var personID, name, title, date string
			var isLunar bool
			if err := rows.Scan(&personID, &name, &title, &date, &isLunar); err == nil {
				out = append(out, Suggestion{
					Type: "纪念日临近", PersonID: personID, PersonName: name,
					Message: title + "（" + date + "）",
				})
			}
		}
	}

	// 3. Unsettled lent money
	rows, err = st.QueryContext(ctx, `SELECT p.id,p.name,
COALESCE(SUM(t.amount_fen)-COALESCE((SELECT SUM(r.amount_fen) FROM repayments r WHERE r.transaction_id=t.id),0),0) AS unpaid
FROM transactions t JOIN people p ON p.id=t.person_id
WHERE t.direction='out' AND t.settled=0
GROUP BY t.person_id ORDER BY unpaid DESC LIMIT 5`)
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
FROM memos m LEFT JOIN people p ON p.id=m.person_id
WHERE m.is_promise=1 AND m.status='open' AND date(m.due_date) < date('now')`)
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
