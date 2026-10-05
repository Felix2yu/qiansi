package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

// 入参校验（M10）。
//
// 这一层的价值不只是「拦住坏数据」：脏日期一旦进库，提醒和统计会带着它一直算下去——
// "2026-02-30" 永远推不出下一次 occurrence，挂在它上面的纪念日待办既到不了点也清不掉；
// "2026-13-1" 会被 SQLite 当普通文本存下，比较时永远大于任何真实日期。

const (
	dateLayout  = "2006-01-02"
	stampLayout = "2006-01-02T15:04:05"
	minLayout   = "2006-01-02T15:04"
)

// normDate 校验并归一化日期。空串按「未填」原样返回，是否必填由调用方决定。
// 用 time.Parse 而不是正则，是为了把「格式对但日子不存在」（2026-02-30、2026-13-01）
// 一起拒掉。两种宽度都收（1990-1-1 常见于手写的 vcf 和从日历粘贴），
// 但一律按补零的形状输出，保证同一列里只有一种字面量。
func normDate(field, v string) (string, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", nil
	}
	for _, layout := range []string{dateLayout, "2006-1-2"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(dateLayout), nil
		}
	}
	return "", fmt.Errorf("%s 不是有效日期，应为 YYYY-MM-DD（如 2026-03-01）", field)
}

// normStamp 同 normDate，但允许带时间尾巴：前端日期框给的是 YYYY-MM-DD，提醒与对话
// 会补成 T09:00:00，旧数据里还有只到分钟的字面量。
// 按命中的那个形状原样回写（只补零、不改长度）：把纯日期强行升成带时间，
// 会让只到日的 due_at 比较跨不过当天，也会让 said_at 这类本存日期的列凭空变长。
func normStamp(field, v string) (string, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", nil
	}
	for _, shape := range []struct{ in, out string }{
		{"2006-01-02T15:04:05", stampLayout},
		{"2006-1-2T15:04:05", stampLayout},
		{"2006-01-02T15:04", minLayout},
		{"2006-1-2T15:04", minLayout},
		{dateLayout, dateLayout},
		{"2006-1-2", dateLayout},
	} {
		if t, err := time.Parse(shape.in, s); err == nil {
			return t.Format(shape.out), nil
		}
	}
	return "", fmt.Errorf("%s 不是有效时间，应为 YYYY-MM-DD 或 YYYY-MM-DDTHH:MM:SS", field)
}

// oneOf 枚举白名单。空串放过：可选字段由各自的必填检查负责。
func oneOf(field, v string, allowed ...string) error {
	if v == "" {
		return nil
	}
	for _, a := range allowed {
		if a == v {
			return nil
		}
	}
	return fmt.Errorf("%s 只能是 %s", field, strings.Join(allowed, " / "))
}

func positiveFen(field string, n int) error {
	if n <= 0 {
		return fmt.Errorf("%s 必须大于 0", field)
	}
	return nil
}

// pathIntID 取 URL 上的自增主键。
// 不能再写 `id, _ := strconv.Atoi(...)`：解析失败会得到 0，
// PUT 于是变成「静默新建一条」，DELETE 变成「删一个不存在的 0 号」。
func pathIntID(r *http.Request, name string) (int, error) {
	raw := chi.URLParam(r, name)
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("无效的 id：%s", raw)
	}
	return id, nil
}

// badRequestErr 把校验器的中文错误统一落成 400。
func badRequestErr(w http.ResponseWriter, err error) {
	writeErr(w, http.StatusBadRequest, err.Error())
}

// queryDate 取 URL 上的日期筛选参数并归一化，未填＝不限（空串）。
func queryDate(r *http.Request, name string) (string, error) {
	return normDate(name, r.URL.Query().Get(name))
}

// queryPositiveInt 取可选的正整数筛选参数（字典 id 这类）。
// 给了坏值一律 400，不能退回「不限」：前端拼错参数时会看到全量数据，
// 以为筛选生效了，比报错更难发现。
func queryPositiveInt(r *http.Request, name string) (int, error) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s 必须是正整数：%s", name, v)
	}
	return n, nil
}

// queryBool 取可选的布尔筛选参数（1/0、true/false 都收）。
// 返回指针：筛选条件里「没提这一项」和「要求它为假」是两件事。
func queryBool(r *http.Request, name string) (*bool, error) {
	v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name)))
	if v == "" {
		return nil, nil
	}
	switch v {
	case "1", "true":
		t := true
		return &t, nil
	case "0", "false":
		f := false
		return &f, nil
	}
	return nil, fmt.Errorf("%s 只能是 1 / 0：%s", name, v)
}

// queryEnum 取可选的枚举筛选参数；未填＝不限，由 handler 决定要不要再要求必填。
func queryEnum(r *http.Request, name string, allowed ...string) (string, error) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return "", nil
	}
	if err := oneOf(name, v, allowed...); err != nil {
		return "", err
	}
	return v, nil
}

// checkDateRange 校区间方向。归一化之后两边都是 YYYY-MM-DD，可以直接按字符串比。
// 起止填反了不该返回空列表：那和「这段时间真的没有记录」在页面上长得一模一样。
func checkDateRange(from, to string) error {
	if from != "" && to != "" && from > to {
		return errors.New("起始日期不能晚于截止日期")
	}
	return nil
}

// ===== 各实体的入参校验 =====
//
// 约定：校验器直接就地归一化字段（写回结构体），返回的第一个错误决定 400 的文案。
// 必填字段本身（标题、日期非空）仍由各 handler 负责，这里只管格式与取值范围，
// 免得「未填」和「填错」共用一句模糊的提示。

func validateMemo(m *store.Memo) error {
	said, err := normStamp("said_at", m.SaidAt)
	if err != nil {
		return err
	}
	m.SaidAt = said
	due, err := normDate("due_date", m.DueDate)
	if err != nil {
		return err
	}
	m.DueDate = due
	if err := oneOf("speaker", m.Speaker, "me", "other"); err != nil {
		return err
	}
	return oneOf("status", m.Status, "open", "fulfilled", "broken")
}

func validateTx(t *store.Transaction) error {
	if t.PersonID == "" {
		return errors.New("person_id 必填：往来必须挂在某个联系人身上")
	}
	if err := oneOf("kind", t.Kind, "loan", "gift", "expense", "other"); err != nil {
		return err
	}
	if err := oneOf("direction", t.Direction, "in", "out"); err != nil {
		return err
	}
	if err := positiveFen("amount_fen", t.AmountFen); err != nil {
		return err
	}
	occ, err := normDate("occurred_at", t.OccurredAt)
	if err != nil {
		return err
	}
	t.OccurredAt = occ
	due, err := normDate("due_date", t.DueDate)
	if err != nil {
		return err
	}
	t.DueDate = due
	settledAt, err := normStamp("settled_at", t.SettledAt)
	if err != nil {
		return err
	}
	t.SettledAt = settledAt
	return nil
}

func validateAnniv(a *store.Anniversary) error {
	d, err := normDate("date", a.Date)
	if err != nil {
		return err
	}
	a.Date = d
	return nil
}

// validateRhythm 校联系节奏表。六个等级一个都不许少：少一档就等于那一档悄悄退回默认值，
// 用户以为自己在改「常来往」的节奏，保存完那一档又变回 30 天。
func validateRhythm(tiers []store.RhythmTier) error {
	seen := map[int]bool{}
	for _, t := range tiers {
		if t.Grade < 0 || t.Grade > 5 {
			return fmt.Errorf("亲密度等级只能是 0–5，收到 %d", t.Grade)
		}
		if seen[t.Grade] {
			return fmt.Errorf("亲密度等级 %s 重复了", gradeLabel(t.Grade))
		}
		seen[t.Grade] = true
		if t.Days < 1 || t.Days > store.MaxRhythmDays {
			return fmt.Errorf("%s 的节奏要在 1–%d 天之间，收到 %d", gradeLabel(t.Grade), store.MaxRhythmDays, t.Days)
		}
	}
	for g := 0; g <= 5; g++ {
		if !seen[g] {
			return fmt.Errorf("缺少 %s 这一档，六个等级都要给天数", gradeLabel(g))
		}
	}
	return nil
}

// gradeLabel 与选人、统计页同一套说法：♥ 的个数就是亲密度，0 是「未设置」。
func gradeLabel(g int) string {
	if g <= 0 {
		return "未设置"
	}
	return "♥×" + strconv.Itoa(g)
}

func validateReminder(r *store.Reminder) error {
	due, err := normStamp("due_at", r.DueAt)
	if err != nil {
		return err
	}
	r.DueAt = due
	return oneOf("status", r.Status, "pending", "done")
}

// validatePerson 目前只校生日。生日是唯一一个会被下游当日期解析的字段：
// SyncBirthdayAnniversary 拿它建纪念日，纪念日再拿它算下一次发生与待办，
// "1990-1-1" 这种少写零的值会在字符串比较里躲过提醒窗口。
func validatePerson(p *store.Person) error {
	d, err := normDate("birthday", p.Birthday)
	if err != nil {
		return err
	}
	p.Birthday = d
	return nil
}