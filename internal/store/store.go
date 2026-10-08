// Package store provides data access layer.
package store

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

type Store struct {
	// DB 是当前连接句柄。请求处理里的读写直接用这个字段即可 —— 一个请求只用一个句柄。
	// 常驻 goroutine（自动备份调度器、通知调度器）必须走 CurrentDB()：
	// 从备份恢复会在 HTTP goroutine 里换掉整个句柄，缓存旧指针意味着恢复后拿着
	// 一个已 Close 的库继续跑，失败只落在 last_error 里，界面上看不出任何异常。
	DB *sql.DB

	swapMu sync.RWMutex
}

func New(db *sql.DB) *Store {
	return &Store{DB: db}
}

// CurrentDB 取当前句柄，供跨 goroutine 的常驻任务现取现用。
func (s *Store) CurrentDB() *sql.DB {
	s.swapMu.RLock()
	defer s.swapMu.RUnlock()
	return s.DB
}

// UseDB 换上新的句柄（从备份恢复后）。旧句柄由调用方负责关闭。
func (s *Store) UseDB(next *sql.DB) {
	s.swapMu.Lock()
	s.DB = next
	s.swapMu.Unlock()
}

// execOne 执行「目标行必须存在」的写语句，并把影响 0 行翻译成 sql.ErrNoRows。
//
// 不查 RowsAffected 是这一层最容易漏的坑：PUT 打到一个不存在的 id（比如另一个标签页
// 刚把它删掉）会静默成功，前端刷新后记录还在原样，用户以为改到了。
// 只用于按主键定位的单行写；清理类语句（解除关联、删中间表、批量置空）合法地可能
// 影响 0 行，走它们会把正常操作变成假 404，别用这个。
func execOne(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, query string, args ...any) error {
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// timeFormat 用于审计字段（created_at 等），保持 UTC 带时区，便于排查问题。
// 业务时间（due_at / occurred_at / 提醒窗口）一律使用下面的本地格式，
// 避免 SQLite 的 date('now')（UTC）与用户所在时区错位一天。
const timeFormat = "2006-01-02T15:04:05Z07:00"
const localTimeFormat = "2006-01-02T15:04:05"
const dateFormat = "2006-01-02"

func nowUTC() string {
	return time.Now().UTC().Format(timeFormat)
}

// nowLocal 返回本地时间的无时区写法，供 due_at 等业务字段使用。
func nowLocal() string {
	return time.Now().Format(localTimeFormat)
}

// todayLocal 返回今天的本地日期 YYYY-MM-DD。
func todayLocal() string {
	return time.Now().Format(dateFormat)
}

// daysFromTodayLocal 返回今天偏移 n 天后的本地日期 YYYY-MM-DD。
func daysFromTodayLocal(n int) string {
	return time.Now().AddDate(0, 0, n).Format(dateFormat)
}

// localCutoff 返回本地时间偏移 n 天后的时间戳字符串，用于 due_at 范围比较。
func localCutoff(days int) string {
	return time.Now().AddDate(0, 0, days).Format(localTimeFormat)
}

// TodayLocal / DaysAgoLocal 供 API 层构造 SQL 日期参数，
// 取代 SQLite 的 date('now')（该函数按 UTC 计算，与用户所在时区会差一天）。
func TodayLocal() string {
	return todayLocal()
}

func DaysAgoLocal(n int) string {
	return daysFromTodayLocal(-n)
}

// localDay 取一个时间串在本机时区里对应的那一天（YYYY-MM-DD）。
//
// created_at 之类是审计字段，按上面的约定存 UTC 带时区；把它当业务日期用时
// 直接 substr 前 10 个字符会在东八区每天 00:00–08:00 把「今天建档」读成昨天，
// 于是刚建的人第一天就被算成已经过了一档。没有时区后缀的老写法（纯本地时间）
// 解析会失败，按字面日期取，不会又因转换错一天。
func localDay(s string) string {
	if len(s) < 10 {
		return ""
	}
	if t, err := time.Parse(timeFormat, s); err == nil {
		return t.Local().Format(dateFormat)
	}
	return s[:10]
}
