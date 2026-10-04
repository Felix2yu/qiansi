package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

// registerExport 提供明细 CSV 与全量 JSON 的导出/导入（N3）。
//
// vCard 只能搬通讯录，备份只能整库换 .db 文件，两者都覆盖不了
// 「把某场宴席的礼单导出去对账」和「换机时把数据交给别的程序看一眼」。
func (a *API) registerExport(r chi.Router) {
	r.Route("/api/v1/export", func(r chi.Router) {
		r.Get("/csv", a.exportCSV)
		r.Get("/json", a.exportJSON)
	})
	r.Post("/api/v1/import/json", a.importJSON)
}

// exportCSV 按类型导出一张明细表。三个过滤维度都是可选的：
// year（自然年）、person_id、event_id（只有金钱明细认后者）。
func (a *API) exportCSV(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	what := q.Get("what")
	if what == "" {
		writeErr(w, 400, "what required（events/transactions/memos）")
		return
	}
	year := 0
	if s := q.Get("year"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1900 || n > 2200 {
			writeErr(w, 400, "year 需要是四位年份")
			return
		}
		year = n
	}
	f := store.ExportFilter{Year: year, PersonID: q.Get("person_id"), EventID: q.Get("event_id")}
	head, rows, err := a.Store.ExportDetail(r.Context(), what, f)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	name := "qiansi-" + what
	if year > 0 {
		name += "-" + strconv.Itoa(year)
	}
	name += ".csv"
	// BOM 不是可选项：没有它 Excel 会把 UTF-8 中文按本地代码页解，打开就是一屏乱码。
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if _, err := w.Write([]byte("\ufeff")); err != nil {
		return
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(head); err != nil {
		return
	}
	for _, row := range rows {
		if err := cw.Write(row); err != nil {
			return
		}
	}
	cw.Flush()
}

func (a *API) exportJSON(w http.ResponseWriter, r *http.Request) {
	d, err := a.Store.DumpAll(r.Context())
	if err != nil {
		writeErr(w, 500, "导出失败: "+err.Error())
		return
	}
	name := "qiansi-full-" + time.Now().Format("20060102-150405") + ".json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	enc := json.NewEncoder(w)
	// 关掉 HTML 转义：备注里一个 & 变成 \u0026 之后，这份文件就没法拿去看第二眼了。
	enc.SetEscapeHTML(false)
	// 写到这里头已经发出去了，再出错只能记一行日志：
	// 浏览器取消下载、连接被掐断都会走到这里，返回体已经不可能改。
	if err := enc.Encode(d); err != nil {
		log.Printf("[export] 全量 JSON 写出中断: %v", err)
	}
}

// dumpMaxBody 与整库恢复用同一个上限：全量 JSON 比 .db 更啰嗦，几十 MB 是正常的。
const dumpMaxBody = 64 << 20

// importJSON 用导出文件整库覆盖。破坏性最强的一个入口，所以先归档再动手，
// 导入失败或导错了还能从 backups/ 里把导入前的库捞回来。
func (a *API) importJSON(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, dumpMaxBody)
	dec := json.NewDecoder(r.Body)
	// 金额、亲密度都是整数。默认解码成 float64 再写回去，SQLite 会存成 REAL，
	// 读回来时 driver 就未必肯把它塞进 int 了。
	dec.UseNumber()
	var d store.Dump
	if err := dec.Decode(&d); err != nil {
		writeErr(w, 400, "JSON 解析失败: "+err.Error())
		return
	}
	if d.App != store.DumpApp || d.Version != store.DumpVersion {
		writeErr(w, 400, fmt.Sprintf("不是牵丝 v%d 的导出文件", store.DumpVersion))
		return
	}
	snapshot, err := a.archiveSnapshot(r.Context())
	if err != nil {
		writeErr(w, 500, "导入前归档当前数据失败: "+err.Error())
		return
	}
	written, err := a.Store.RestoreAll(r.Context(), &d)
	if err != nil {
		if errors.Is(err, store.ErrDumpInvalid) {
			writeErr(w, 400, err.Error())
			return
		}
		writeStoreErr(w, err)
		return
	}
	rows := 0
	for _, n := range written {
		rows += n
	}
	writeJSON(w, 200, map[string]any{
		"tables":   len(written),
		"rows":     rows,
		"snapshot": snapshot,
		"note":     "附件的图片文件不在 JSON 里，换机时需要另外拷贝 uploads 目录",
	})
}
