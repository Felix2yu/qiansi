// Package store provides data access layer.
package store

import (
	"database/sql"
	"time"
)

type Store struct {
	DB *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{DB: db}
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
