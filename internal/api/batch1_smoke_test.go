package api

// 批次①（口径与无人值守）的端到端冒烟：按用户真实操作顺序走一遍，四条缺陷同时上锁。
//
// 这一批的共性是「编译得过、类型检查过、界面上看不出任何异常」—— 上一轮审查里的
// 「新建按钮根本不存在」就是这么漏过去的。后端有覆盖率门槛，但覆盖率不等于口径正确，
// 所以这里锁的是操作序列（建对话 → 记礼物 → 看首页数字 → 恢复后备份），不是单个函数。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/qiansi/app/internal/db"
	bsched "github.com/qiansi/app/internal/backup/scheduler"
)

// M1 + M4：记一笔随礼不该变成欠款；到期承诺该出现在待办里，勾掉即兑现。
func TestSmokeBatch1_GiftIsNotADebtAndPromiseReachesTodos(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	zhang := apiCreatePersonMap(t, s, map[string]any{"name": "张三"})["id"].(string)

	// 1) 新建对话：答应帮张三搬家，约定今天办
	memo := decodeMap(t, s.do(http.MethodPost, "/api/v1/memos", map[string]any{
		"person_id": zhang, "speaker": "me", "content": "帮张三搬家",
		"said_at": apiDate(0), "is_promise": true, "due_date": apiDate(0),
	}))
	memoID, _ := memo["id"].(string)
	if memoID == "" {
		t.Fatalf("新建对话没返回 id: %v", memo)
	}

	// 2) 金钱：随礼 600（礼物）+ 借出 500（借款），都不勾任何「结清」
	giftID := decodeMap(t, s.do(http.MethodPost, "/api/v1/transactions", map[string]any{
		"person_id": zhang, "kind": "gift", "direction": "out",
		"amount_fen": 60000, "title": "乔迁随礼", "occurred_at": apiDate(0),
	}))["id"].(string)
	s.do(http.MethodPost, "/api/v1/transactions", map[string]any{
		"person_id": zhang, "kind": "loan", "direction": "out",
		"amount_fen": 50000, "title": "周转", "occurred_at": apiDate(0),
	})

	// 礼物提交即了结，界面拿到的就是这个值（前端不再给勾选）
	if tx := s.get("/api/v1/transactions/" + giftID); tx["settled"] != true {
		t.Errorf("礼物应提交即结清: %v", tx)
	}

	// 3) 首页「我借出（未还）」只该是那 500
	stats := s.get("/api/v1/dashboard/stats")
	if got := int(stats["lend_fen"].(float64)); got != 50000 {
		t.Errorf("lend_fen = %d 分, 期望 50000（600 的随礼不该变成借款）", got)
	}

	// 新建入口已经会自动了结非借还行，但库里可能躺着修复前的存量数据：
	// 口径必须自己站得住，不能只靠写入时的兜底（迁移 010 之外的第二道防线）
	if _, err := s.Store.DB.ExecContext(ctx,
		`INSERT INTO transactions(id,person_id,kind,direction,amount_fen,title,occurred_at,settled,created_at)
		 VALUES('legacy-gift',?,'gift','out',60000,'修复前留下的随礼',?,0,'t')`, zhang, apiDate(0)); err != nil {
		t.Fatalf("seed legacy gift: %v", err)
	}
	if got := int(s.get("/api/v1/dashboard/stats")["lend_fen"].(float64)); got != 50000 {
		t.Errorf("存量的未结清礼物仍被算进欠账: lend_fen = %d, 期望 50000", got)
	}
	s.do(http.MethodDelete, "/api/v1/transactions/legacy-gift", nil)

	// 建议里的「待还」也只能是 500，否则用户会去催一个没借钱的人
	sugg := decodeMapList(t, s.do(http.MethodGet, "/api/v1/dashboard/suggestions", nil))
	var owed []string
	for _, x := range sugg {
		if x["type"] == "有借款未还" && x["person_name"] == "张三" {
			owed = append(owed, x["message"].(string))
		}
	}
	if len(owed) != 1 {
		t.Fatalf("应有且只有一条张三的待还建议: %+v", sugg)
	}
	if !strings.Contains(owed[0], "500") || strings.Contains(owed[0], "600") {
		t.Errorf("待还文案 = %q, 只该出现 500", owed[0])
	}

	// 4) 到期承诺进待办页（M4 之前只在今日页的建议文案里提一句）
	pending := decodeMapList(t, s.do(http.MethodGet, "/api/v1/reminders?status=pending&limit=200", nil))
	if !hasID(pending, "promise:"+memoID) {
		t.Fatalf("到期承诺没进待办列表: %+v", pending)
	}
	// 推送正文取的就是 upcoming，两边必须同源
	up := decodeMapList(t, s.do(http.MethodGet, "/api/v1/reminders/upcoming?days=7", nil))
	if !hasID(up, "promise:"+memoID) {
		t.Fatalf("到期承诺没进每日摘要数据源: %+v", up)
	}

	// 勾掉 = 兑现承诺本身，随后从待办消失（不需要额外的 dismiss）
	if rec := s.do(http.MethodPost, "/api/v1/reminders/promise:"+memoID+"/done", nil); rec.Code != 204 {
		t.Fatalf("完成派生待办 => %d: %s", rec.Code, rec.Body.String())
	}
	after := decodeMapList(t, s.do(http.MethodGet, "/api/v1/reminders?status=pending&limit=200", nil))
	if hasID(after, "promise:"+memoID) {
		t.Errorf("已兑现的承诺仍在待办里: %+v", after)
	}
	var status string
	if err := s.Store.DB.QueryRowContext(ctx, "SELECT status FROM memos WHERE id=?", memoID).Scan(&status); err != nil {
		t.Fatalf("读承诺状态: %v", err)
	}
	if status != "fulfilled" {
		t.Errorf("承诺状态 = %s, 期望 fulfilled", status)
	}

	// 派生项不在表里：改/删必须明确拒绝，不能假装成功（旧行为是 0 行 + 204）
	if rec := s.do(http.MethodPut, "/api/v1/reminders/promise:"+memoID, map[string]any{"title": "改了"}); rec.Code != http.StatusBadRequest {
		t.Errorf("PUT 派生待办 => %d, 期望 400", rec.Code)
	}
	if rec := s.do(http.MethodDelete, "/api/v1/reminders/promise:"+memoID, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("DELETE 派生待办 => %d, 期望 400", rec.Code)
	}
}

// M2：从备份恢复之后，自动备份必须照常执行。
//
// 恢复会在 HTTP goroutine 里换掉 *sql.DB，而调度器是进程启动时起、常驻到退出。
// 旧实现把启动时那份句柄存进了 Runner，恢复之后每次自动备份都在对已关闭的旧库
// 执行 VACUUM，失败只落在 last_error，设置页照样显示「下次执行」—— 最需要备份的
// 时刻（刚恢复过）恰恰没有备份。
func TestSmokeBatch1_AutoBackupStillRunsAfterRestore(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	apiCreatePersonMap(t, s, map[string]any{"name": "恢复前的人"})

	if rec := s.do(http.MethodPost, "/api/v1/backup/snapshot", nil); rec.Code != 200 {
		t.Fatalf("snapshot => %d: %s", rec.Code, rec.Body.String())
	}
	expRec := s.do(http.MethodGet, "/api/v1/backup/export", nil)
	if expRec.Code != 200 {
		t.Fatalf("export => %d: %s", expRec.Code, expRec.Body.String())
	}
	snapshot := expRec.Body.Bytes()

	// 快照之后写进去的人，恢复后应当消失
	apiCreatePersonMap(t, s, map[string]any{"name": "将被回滚的人"})
	body, ct := vcardBackupMultipart(t, "backup.db", snapshot)
	if rec := s.raw(http.MethodPost, "/api/v1/backup/restore", body, ct); rec.Code != 200 {
		t.Fatalf("restore => %d: %s", rec.Code, rec.Body.String())
	}

	// 常驻 Runner 与 main 里那个一样只拿 store + cfg
	runner := bsched.New(s.Store, s.Cfg)
	path, err := runner.RunNow(ctx, time.Now())
	if err != nil {
		t.Fatalf("恢复后自动备份失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("自动备份没有落盘: %v", err)
	}

	// 归档内容必须是恢复后的那份库，而不是一个空的旧快照
	probe, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开归档失败: %v", err)
	}
	defer probe.Close()
	var names []string
	rows, err := probe.QueryContext(ctx, "SELECT name FROM people ORDER BY name")
	if err != nil {
		t.Fatalf("查归档: %v", err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			names = append(names, n)
		}
	}
	rows.Close()
	if len(names) != 1 || names[0] != "恢复前的人" {
		t.Fatalf("归档内容不符: %v", names)
	}

	// 状态接口：成功路径写回、错误清空、下次执行时间往后推
	st := s.get("/api/v1/backup/auto")
	if st["last_path"] != path {
		t.Errorf("last_path = %v, 期望 %s", st["last_path"], path)
	}
	if st["last_error"] != "" {
		t.Errorf("last_error 未清空: %v", st["last_error"])
	}
	if st["last_run"] == "" || st["next_run"] == "" {
		t.Errorf("执行状态没推进: %v", st)
	}

	// 手动触发与定时走同一个 execute()：恢复后再点一次「立即执行」也应成功
	if rec := s.do(http.MethodPost, "/api/v1/backup/auto/run", nil); rec.Code != 200 {
		t.Fatalf("恢复后背 auto/run => %d: %s", rec.Code, rec.Body.String())
	}
}

// decodeMapList 断言 200 并解出数组响应
func decodeMapList(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("请求 => %d: %s", rec.Code, rec.Body.String())
	}
	return vcardBackupDecodeList(t, rec)
}

func hasID(list []map[string]any, id string) bool {
	for _, x := range list {
		if x["id"] == id {
			return true
		}
	}
	return false
}
