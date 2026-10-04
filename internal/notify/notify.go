// Package notify handles apprise-go multi-channel push and scheduler.
package notify

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	apprise "github.com/unraid/apprise-go"
	"github.com/qiansi/app/internal/store"
)

// tickInterval 是轮询间隔。到点判定是「今天做过没有」这种幂等检查，
// 轮得密一点只多两次 settings 读，却能把摘要的送达时间从「配置点后 30 分钟内」
// 收紧到 1 分钟 —— 用户填 09:00 就是 09:00。
const tickInterval = time.Minute

// 每日任务的生效时刻（本地时间，点之后当天任意一次 tick 都会补跑一次）。
const snapshotHour = 3

// 记账键：值为「最近一次做成这件事的本地日期」。
//
// 旧写法是 `Hour()==3 && Minute()<5` 这种窗口判定，配 30 分钟的 ticker，
// tick 相位由进程启动时刻决定 —— 多数部署永远撞不进那个 5 分钟窗口，
// 于是配了推送也一条都收不到。改成按日期记账后，只要今天还没做成、且已过时刻，就补跑。
const (
	keySnapshotDay = "notify_snapshot_day"
	keyDigestDay   = "notify_digest_day"
)

// RunScheduler is the long-running scheduler for daily digest and day-before reminders.
// If no apprise URLs are configured, it is effectively a no-op.
// 自动备份已独立成 internal/backup/scheduler（周期可配置），不再由这里驱动。
func RunScheduler(ctx context.Context, s *store.Store) {
	// 启动即评估：进程 09:30 起来、摘要配在 09:00，不该再等一整天。
	runTick(ctx, s, time.Now())
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			runTick(ctx, s, time.Now())
		}
	}
}

// runTick 是每日任务的判定核心。now 便于测试注入。
func runTick(ctx context.Context, s *store.Store, now time.Time) {
	now = now.Local()
	today := dayKey(now)

	// 1. 亲密快照：失败（比如库被换掉）不记账，下一次 tick 再来一次。
	if now.Hour() >= snapshotHour && notDone(ctx, s, keySnapshotDay, today) {
		if err := snapshotAll(ctx, s, now); err == nil {
			markDone(ctx, s, keySnapshotDay, today)
		} else {
			log.Printf("[notify] 亲密快照失败: %v", err)
		}
	}

	// 2. 每日摘要：推送失败同样不记账，靠下一轮 tick 重试；
	//    推送成功、未配置通道、近期无待办都算「今天这件事处理完了」。
	pushHour := pushHourOf(ctx, s)
	if now.Hour() >= pushHour && notDone(ctx, s, keyDigestDay, today) {
		if err := sendDigest(ctx, s); err != nil {
			log.Printf("[notify] 每日摘要推送失败，将在下一轮重试: %v", err)
		} else {
			markDone(ctx, s, keyDigestDay, today)
		}
	}
}

func dayKey(t time.Time) string { return t.Format("2006-01-02") }

// notDone 判断今天是否还没做成。读取失败按「没做成」处理 —— 宁可多跑一次
// （两个任务本身都幂等），也不要因为一次读抖动就整天不发。
func notDone(ctx context.Context, s *store.Store, key, today string) bool {
	v, err := s.SettingGet(ctx, key)
	return err != nil || v != today
}

func markDone(ctx context.Context, s *store.Store, key, today string) {
	if err := s.SettingSet(ctx, key, today); err != nil {
		log.Printf("[notify] 记录 %s 失败: %v", key, err)
	}
}

// pushHourOf 读取摘要推送时刻，非法或缺失回落到 09:00，并夹在 0-23。
func pushHourOf(ctx context.Context, s *store.Store) int {
	v, err := s.SettingGet(ctx, "push_time_hour")
	if err != nil || strings.TrimSpace(v) == "" {
		return 9
	}
	h, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || h < 0 || h > 23 {
		return 9
	}
	return h
}

// snapshotAll 为每个人写当天的亲密度快照。day 用本地日期：
// 亲密度趋势的比对基准（store.intimacy_snapshots）取的是本地日，
// 这里再用 UTC 会让东八区的快照整体早一天，曲线上出现断点。
func snapshotAll(ctx context.Context, s *store.Store, now time.Time) error {
	people, err := s.PersonList(ctx, "", 0, 0, false, 0, 500, 0)
	if err != nil {
		return err
	}
	day := dayKey(now.Local())
	for _, p := range people {
		res, err := s.PersonIntimacy(ctx, p.ID)
		if err != nil {
			continue
		}
		if _, err := s.DB.ExecContext(ctx,
			"INSERT OR REPLACE INTO intimacy_snapshots(person_id,day,score) VALUES(?,?,?)",
			p.ID, day, res.CurrentScore); err != nil {
			return err
		}
	}
	return nil
}

// sendDigest 推送近 7 天待办。返回 nil 表示今天不必再试（含未配置与无事可报）。
func sendDigest(ctx context.Context, s *store.Store) error {
	urlsStr, _ := s.SettingGet(ctx, "apprise_urls")
	if urlsStr == "" {
		return nil
	}
	upcoming, err := s.ReminderUpcoming(ctx, 7)
	if err != nil {
		return err
	}
	if len(upcoming) == 0 {
		return nil
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
		return fmt.Errorf("digest: %w", err)
	}
	return nil
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
