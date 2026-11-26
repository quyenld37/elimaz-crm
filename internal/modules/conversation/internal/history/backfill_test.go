package history_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/channel/channeltest"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/history"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// vn là giờ Việt Nam (UTC+7) cho đồng hồ của test.
func vn() *time.Location { return time.FixedZone("ICT", 7*3600) }

// pollCase là một ca của TestCommentPoll: chế độ (rỗng: không có dòng page_settings), feed cuối cách bao lâu (nil:
// chưa từng có), lúc chạy, trạng thái và kênh của Page, có lệnh hay không.
type pollCase struct {
	name   string
	mode   string
	feed   *time.Duration
	now    time.Time
	status string
	key    string
	want   bool
}

// Đọc comment định kỳ (Q4): lịch channel.comment_poll ghi lệnh command.fb.comment_poll cho Page active theo chế độ
// page_settings.comment_poll_mode: auto → chỉ khi feed cuối cũ hơn 10 phút (hay chưa từng có) trong giờ bán; on → luôn;
// off → không; chưa có dòng cài đặt → auto. Mốc đọc = lúc chạy − 2 phút; chạy lại trong cùng kỳ 30 s không thêm lệnh;
// Page degraded, Page của kênh không có adapter → không.
func TestCommentPoll(t *testing.T) {
	t.Parallel()
	noon := time.Date(2026, 11, 26, 12, 30, 0, 0, vn())
	night := time.Date(2026, 11, 26, 3, 0, 0, 0, vn())
	cases := []pollCase{
		{"auto, feed 11 phút, 12:30", "auto", new(11 * time.Minute), noon, channel.StatusActive, "facebook", true},
		{"auto, feed 2 phút", "auto", new(2 * time.Minute), noon, channel.StatusActive, "facebook", false},
		{"auto, chưa có feed", "auto", nil, noon, channel.StatusActive, "facebook", true},
		{"auto, feed 11 phút, 03:00", "auto", new(11 * time.Minute), night, channel.StatusActive, "facebook", false},
		{"không có cài đặt = auto", "", new(11 * time.Minute), noon, channel.StatusActive, "facebook", true},
		{"off", "off", new(11 * time.Minute), noon, channel.StatusActive, "facebook", false},
		{"on, feed 2 phút", "on", new(2 * time.Minute), noon, channel.StatusActive, "facebook", true},
		{"on, 03:00", "on", new(2 * time.Minute), night, channel.StatusActive, "facebook", true},
		{"on, degraded", "on", nil, noon, channel.StatusDegraded, "facebook", false},
		{"on, kênh không có adapter", "on", nil, noon, channel.StatusActive, "fakechat", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			runPollCase(t, c)
		})
	}
	if history.CommentPollSchedule != "channel.comment_poll" {
		t.Fatal("tên lịch")
	}
}

// runPollCase dựng một shop có một Page theo ca c, chạy lịch đọc comment hai lần lúc c.now và kiểm lệnh được ghi.
func runPollCase(t *testing.T, c pollCase) {
	t.Helper()
	s, tenant, id := seedPollCase(t, c)
	runPoll(t, s, tenant, c.now)
	checkPoll(t, s, tenant, id, c)
}

// seedPollCase dựng shop A có Page "1001" kênh c.key, trạng thái c.status, feed cuối theo c.feed, chế độ c.mode.
func seedPollCase(t *testing.T, c pollCase) (*testkit.Staff, uuid.UUID, uuid.UUID) {
	t.Helper()
	s := testkit.OpenStaff(t)
	tenant := s.Tenant(t, "A")
	id := s.ChannelAccount(t, tenant, c.key, "1001", "Shop Một")
	var feed *time.Time
	if c.feed != nil {
		feed = new(c.now.Add(-*c.feed))
	}
	s.Exec(t, tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `UPDATE channel.channel_accounts SET last_feed_at = $2, status = $3 WHERE id = $1`,
			id, feed, c.status); err != nil || c.mode == "" {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO conversation.page_settings (tenant_id, channel_account_id, comment_poll_mode)
			VALUES ($1, $2, $3)`, tenant, id, c.mode)
		return err //nolint:wrapcheck // test
	})
	return s, tenant, id
}

// runPoll chạy việc của lịch đọc comment hai lần lúc now (adapter giả khóa facebook).
func runPoll(t *testing.T, s *testkit.Staff, tenant uuid.UUID, now time.Time) {
	t.Helper()
	reg, err := channel.NewRegistry(channeltest.NewConnectorAdapter("facebook"))
	if err != nil {
		t.Fatal(err)
	}
	d := deps.Deps{DB: s.App, Queue: queue.NewQueue(), Registry: reg, Now: func() time.Time { return now }}
	var h queue.Handler
	for _, x := range history.Parts(d).Jobs {
		if x.Kind().Name == "task."+history.CommentPollSchedule {
			h = x
		}
	}
	if h == nil {
		t.Fatal("không có việc của lịch đọc comment")
	}
	for range 2 {
		if err := tenancy.WithTenant(t.Context(), s.App, tenant, func(tx pgx.Tx) error {
			_, err := h.Handle(t.Context(), queue.Job{ID: uuid.New(), TenantID: &tenant}, tx)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// checkPoll kiểm số lệnh đọc comment và payload (tài khoản id, mốc đọc = lúc chạy − 2 phút, không token).
func checkPoll(t *testing.T, s *testkit.Staff, tenant, id uuid.UUID, c pollCase) {
	t.Helper()
	rows, err := s.Owner.Pool.Query(t.Context(), `SELECT payload::text FROM platform.jobs WHERE tenant_id = $1 AND kind = $2`,
		tenant, channel.CommentPollKind)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	if c.want {
		want = 1
	}
	if len(ps) != want {
		t.Fatalf("lệnh đọc comment = %v, muốn %d", ps, want)
	}
	if !c.want {
		return
	}
	var p struct {
		AccountID uuid.UUID `json:"accountId"`
		Since     time.Time `json:"since"`
	}
	if err := json.Unmarshal([]byte(ps[0]), &p); err != nil || p.AccountID != id || !p.Since.Equal(c.now.Add(-2*time.Minute)) ||
		strings.Contains(ps[0], "EAAB") {
		t.Fatalf("payload = %s", ps[0])
	}
}
