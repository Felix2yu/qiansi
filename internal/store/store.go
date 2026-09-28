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

const timeFormat = "2006-01-02T15:04:05Z07:00"

func nowUTC() string {
	return time.Now().UTC().Format(timeFormat)
}
