package live

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Nhận biết phiên live bằng hỏi định kỳ (Q5): lịch live.detect mỗi 30 s theo tenant ghi lệnh command.fb.live_detect
// (chỉ đọc): Execute đọc Page và token trong một transaction riêng, gọi kênh ngoài transaction, kết quả ghi ở OnDone.
const (
	DetectSchedule    = "live.detect"
	DetectCommandName = "command.fb.live_detect"
	detectEvery       = 30 * time.Second
	detectExpires     = time.Minute
	detectTimeout     = 20 * time.Second
	// metricDetectFailed đếm Page hỏi kênh lỗi trong một lượt (nhãn: khóa kênh; read_accounts khi không đọc được Page
	// từ database); Page đó chờ lượt sau.
	metricDetectFailed = "live_detect_failed"
)

// detector là phần nhận biết của channel.LiveSource mà lệnh cần (adapter có đủ năm hàm từ Task 171).
type detector interface {
	DetectLive(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials) ([]channel.LiveVideo, error)
	LiveStatus(ctx context.Context, acc channel.ChannelAccount, cred channel.Credentials, videoID string) (string, error)
}

// DetectCommand là lệnh command.fb.live_detect.
type DetectCommand struct{ d deps.Deps }

// NewDetectCommand dựng lệnh nhận biết live.
func NewDetectCommand(d deps.Deps) *DetectCommand { return &DetectCommand{d: d} }

// DetectScheduleFor là lịch live.detect chạy lệnh c mỗi 30 s cho từng tenant.
func DetectScheduleFor(c *DetectCommand) schedules.Schedule {
	return schedules.Schedule{Name: DetectSchedule, Every: detectEvery, PerTenant: true, Kind: c.Kind()}
}

// Kind là loại lệnh (hết hạn sau 60 s: lượt sau đã hỏi lại).
func (*DetectCommand) Kind() queue.Kind {
	return commands.CommandKind(DetectCommandName, detectExpires, detectTimeout)
}

// AutoRetryNotSent trả true: lệnh chỉ đọc, làm lại không hại.
func (*DetectCommand) AutoRetryNotSent() bool { return true }

// Lookup luôn trả not_sent (lệnh chỉ đọc).
func (*DetectCommand) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// detectResult là kết quả một lượt: mỗi Page các phiên kênh báo đang live và các phiên đã biết nay đã dừng.
type detectResult struct {
	Accounts []detectAccount `json:"accounts"`
}

type detectAccount struct {
	AccountID uuid.UUID           `json:"accountId"`
	Live      []channel.LiveVideo `json:"live"`
	Ended     []string            `json:"ended"`
	At        time.Time           `json:"at"`
}

type detectTarget struct {
	acc   channel.ChannelAccount
	cred  channel.Credentials
	det   detector
	known []string
}

// Execute hỏi kênh từng Page còn kết nối có token: danh sách video đang LIVE, và trạng thái của phiên live đã biết mà
// không còn trong danh sách. Lỗi ở một Page (token, Meta tạm lỗi) chỉ bỏ Page đó lượt này; lệnh luôn xong.
func (c *DetectCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	if j.TenantID == nil {
		return commands.Outcome{}, fmt.Errorf("lệnh %s thiếu tenant: %w", j.ID, queue.ErrPermanent)
	}
	targets, err := c.targets(ctx, *j.TenantID)
	if err != nil {
		c.failed("read_accounts") // database lỗi: chưa gọi kênh, lệnh tự chạy lại (AutoRetryNotSent)
		return commands.Outcome{Status: commands.StatusNotSent, Reason: "read_accounts"}, nil
	}
	var res detectResult
	for _, t := range targets {
		vids, err := t.det.DetectLive(ctx, t.acc, t.cred)
		if err != nil {
			c.failed(t.acc.Channel)
			continue
		}
		a := detectAccount{AccountID: t.acc.ID, Live: vids, At: clock(c.d)}
		for _, id := range t.known {
			if slices.ContainsFunc(vids, func(v channel.LiveVideo) bool { return v.LiveVideoID == id }) {
				continue
			}
			st, err := t.det.LiveStatus(ctx, t.acc, t.cred, id)
			if err != nil {
				c.failed(t.acc.Channel)
				continue
			}
			if state, ok := StateOf(st); ok && state == StatusEnded {
				a.Ended = append(a.Ended, id)
			}
		}
		res.Accounts = append(res.Accounts, a)
	}
	body, err := json.Marshal(res)
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả nhận biết live: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: body}, nil
}

// targets đọc Page của tenant mà kênh nhận biết được live, còn kết nối và có token, cùng phiên đang live của Page.
func (c *DetectCommand) targets(ctx context.Context, tenant uuid.UUID) ([]detectTarget, error) {
	var out []detectTarget
	err := tenancy.WithTenant(ctx, c.d.DB, tenant, func(tx pgx.Tx) error {
		out = nil
		accs, err := channel.ListAccounts(ctx, tx, nil)
		if err != nil {
			return err //nolint:wrapcheck // channel đã bọc việc đang làm
		}
		for _, acc := range accs {
			det := detectorOf(c.d.Registry, acc.Channel)
			if det == nil || acc.Status == "disconnected" || acc.TokenKind == "" {
				continue
			}
			cred, err := channel.CredentialsFor(ctx, tx, c.d.Keys, acc.ID)
			if err != nil {
				c.failed(acc.Channel)
				continue
			}
			lives, err := store.New(tx).ListLiveSessionsOfAccount(ctx, acc.ID)
			if err != nil {
				return fmt.Errorf("đọc phiên live của Page: %w", err)
			}
			t := detectTarget{acc: acc, cred: cred, det: det}
			for _, s := range lives {
				t.known = append(t.known, s.LiveVideoID)
			}
			out = append(out, t)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("đọc Page nhận biết live: %w", err)
	}
	return out, nil
}

// OnDone ghi kết quả trong transaction của Runner (ngữ cảnh tenant của lệnh): phiên LIVE → UpsertSession detected_by
// poll (trùng với webhook vẫn một phiên); phiên đã dừng → ended.
func (c *DetectCommand) OnDone(ctx context.Context, _ queue.Job, result json.RawMessage, tx pgx.Tx) error {
	var res detectResult
	if err := json.Unmarshal(result, &res); err != nil {
		return fmt.Errorf("giải kết quả nhận biết live: %w", queue.ErrPermanent)
	}
	for _, a := range res.Accounts {
		for _, v := range a.Live {
			if state, ok := StateOf(v.Status); !ok || state != StatusLive || v.LiveVideoID == "" {
				continue
			}
			started := v.StartedAt
			if started.IsZero() {
				started = a.At
			}
			if _, _, err := UpsertSession(ctx, tx, Observation{
				AccountID: a.AccountID, LiveVideoID: v.LiveVideoID, VideoID: v.VideoID, PostExternalID: v.PostID,
				Title: v.Title, State: StatusLive, DetectedBy: DetectedPoll, At: started,
			}); err != nil {
				return err
			}
		}
		for _, id := range a.Ended {
			if _, _, err := UpsertSession(ctx, tx, Observation{
				AccountID: a.AccountID, LiveVideoID: id, State: StatusEnded, DetectedBy: DetectedPoll, At: a.At,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *DetectCommand) failed(key string) {
	if c.d.Metrics != nil {
		c.d.Metrics.Inc(metricDetectFailed, key, 1)
	}
}

// detectorOf trả phần nhận biết live của adapter key trong r, nil khi không có.
func detectorOf(r *channel.Registry, key string) detector { //nolint:ireturn // phần nhận biết là interface của kênh
	if r == nil {
		return nil
	}
	a, ok := r.Get(key)
	if !ok {
		return nil
	}
	d, _ := a.(detector)
	return d
}

// canDetect: có adapter nào trong r nhận biết được live không.
func canDetect(r *channel.Registry) bool {
	if r == nil {
		return false
	}
	return slices.ContainsFunc(r.Keys(), func(k string) bool { return detectorOf(r, k) != nil })
}
