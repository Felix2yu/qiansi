// Package notify handles apprise-go multi-channel push and scheduler.
package notify

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	apprise "github.com/unraid/apprise-go"
	"github.com/qiansi/app/internal/store"
)

// RunScheduler is the long-running scheduler for daily digest and day-before reminders.
// If no apprise URLs are configured, it is effectively a no-op.
func RunScheduler(ctx context.Context, s *store.Store) {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			runTick(ctx, s)
		}
	}
}

func runTick(ctx context.Context, s *store.Store) {
	// 1. Intimacy snapshot daily (03:00)
	if time.Now().Hour() == 3 && time.Now().Minute() < 5 {
		snapshotAll(ctx, s)
	}
	// 2. Digest at configured push time (default 09:00)
	pushHour := 9
	if v, err := s.SettingGet(ctx, "push_time_hour"); err == nil && v != "" {
		fmt.Sscanf(v, "%d", &pushHour)
	}
	if time.Now().Hour() == pushHour && time.Now().Minute() < 5 {
		sendDigest(ctx, s)
	}
}

func snapshotAll(ctx context.Context, s *store.Store) {
	people, err := s.PersonList(ctx, "", 0, 0, false, 0, 500, 0)
	if err != nil {
		return
	}
	for _, p := range people {
		res, err := s.PersonIntimacy(ctx, p.ID)
		if err != nil {
			continue
		}
		day := time.Now().UTC().Format("2006-01-02")
		_, _ = s.DB.ExecContext(ctx,
			"INSERT OR REPLACE INTO intimacy_snapshots(person_id,day,score) VALUES(?,?,?)",
			p.ID, day, res.CurrentScore)
	}
}

func sendDigest(ctx context.Context, s *store.Store) {
	urlsStr, _ := s.SettingGet(ctx, "apprise_urls")
	if urlsStr == "" {
		return
	}
	upcoming, _ := s.ReminderUpcoming(ctx, 7)
	if len(upcoming) == 0 {
		return
	}
	var body strings.Builder
	body.WriteString("以下是近 7 天待办：\n")
	for _, r := range upcoming {
		body.WriteString("· ")
		body.WriteString(r.Title)
		if r.PersonName != "" {
			body.WriteString(" (")
			body.WriteString(r.PersonName)
			body.WriteString(")")
		}
		body.WriteString(" — ")
		body.WriteString(r.DueAt)
		body.WriteString("\n")
	}
	urls := strings.FieldsFunc(urlsStr, func(r rune) bool { return r == ',' || r == '\n' })
	if err := Push(ctx, urls, "牵丝 · 每日摘要", body.String()); err != nil {
		log.Printf("[notify] digest send failed: %v", err)
	}
}

// Push sends to all given apprise URLs. Failures are logged but not retried.
func Push(ctx context.Context, urls []string, title, body string) error {
	if len(urls) == 0 {
		return nil
	}
	client := apprise.New()
	for _, u := range urls {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		if err := client.Add(u); err != nil {
			log.Printf("[notify] add url %s: %v", u, err)
			continue
		}
	}
	if err := client.Send(body, apprise.WithTitle(title)); err != nil {
		return err
	}
	return nil
}
