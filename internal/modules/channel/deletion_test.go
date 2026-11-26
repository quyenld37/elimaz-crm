package channel_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/datadeletion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// saveSession lưu một phiên Page của người dùng app appUserID cho nhân viên mới của tenant (trả id phiên).
func (e *chEnv) saveSession(tenant uuid.UUID, email, appUserID string, now time.Time) uuid.UUID {
	e.t.Helper()
	ctx := e.t.Context()
	user := e.staff.User(e.t, tenant, email, "active")
	kr := testKeyring(e.t)
	var id uuid.UUID
	if err := tenancy.WithTenant(ctx, e.staff.App, tenant, func(tx pgx.Tx) error {
		state, err := channel.StartOAuth(ctx, tx, user, now)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		startID, err := channel.CheckOAuthState(ctx, tx, user, state, now)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		id, err = channel.SaveOAuthPages(ctx, tx, kr, startID, user,
			channel.OAuthLogin{Token: "EAAB-" + logging.Secret(email), Pages: testPages(), AppUserID: appUserID}, now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// Phiên Page lưu băm mã người dùng app (datadeletion.UserRefHash), không lưu mã gốc.
func TestSaveOAuthPages_StoresUserRefHash(t *testing.T) {
	t.Parallel()
	e, _, now := newOAuthEnv(t)
	id := e.saveSession(e.tenant, "nv1@elimaz.test", "218471", now)
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM channel.oauth_sessions WHERE id = $1 AND fb_user_ref_hash = $2",
		id, datadeletion.UserRefHash("218471")); n != 1 {
		t.Errorf("phiên Page không có băm đúng")
	}
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM channel.oauth_sessions WHERE fb_user_ref_hash IS NULL AND user_token_enc IS NULL"); n != 1 {
		t.Errorf("dòng start phải không có băm, có %d", n)
	}
}

// Hai tenant có phiên cùng băm: việc platform.data_deletion xóa cả hai (phiên người khác giữ nguyên), completed_at có
// giá trị.
func TestDeleteOAuthDataFor_AllTenants(t *testing.T) {
	t.Parallel()
	e, _, now := newOAuthEnv(t)
	b := e.staff.Tenant(t, "B")
	sa := e.saveSession(e.tenant, "nv1@elimaz.test", "218471", now)
	sb := e.saveSession(b, "nv2@elimaz.test", "218471", now)
	keep := e.saveSession(b, "nv3@elimaz.test", "999", now)
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM channel.oauth_sessions WHERE id = $1", sa) +
		e.staff.CountIn(t, b, "SELECT count(*) FROM channel.oauth_sessions WHERE id IN ($1, $2)", sb, keep); n != 3 {
		t.Fatalf("trước khi xóa thấy %d phiên; muốn 3", n)
	}
	code := "0123456789abcdef0123456789abcdef"
	if _, err := e.staff.Owner.Pool.Exec(t.Context(),
		"INSERT INTO platform.data_deletion_requests (code, source, user_ref_hash) VALUES ($1, 'facebook', $2)",
		code, datadeletion.UserRefHash("218471")); err != nil {
		t.Fatal(err)
	}
	p := datadeletion.NewProcessor(channel.DeleteOAuthDataFor)
	if err := e.staff.App.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		_, err := p.Handle(t.Context(), queue.Job{ID: uuid.New()}, tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	// Bảng có RLS: đếm trong từng tenant (kiểm cả trước khi chạy để chắc câu đếm thấy dòng).
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM channel.oauth_sessions WHERE id = $1", sa) +
		e.staff.CountIn(t, b, "SELECT count(*) FROM channel.oauth_sessions WHERE id = $1", sb); n != 0 {
		t.Errorf("còn %d phiên của người dùng đã yêu cầu xóa", n)
	}
	if n := e.staff.CountIn(t, b, "SELECT count(*) FROM channel.oauth_sessions WHERE id = $1", keep); n != 1 {
		t.Error("phiên của người khác bị xóa")
	}
	if n := e.staff.Count(t, "SELECT count(*) FROM platform.data_deletion_requests WHERE code = $1 AND completed_at IS NOT NULL", code); n != 1 {
		t.Error("completed_at chưa có giá trị")
	}
}

// checkupAt chạy lệnh nhắc Data Use Checkup với hạn due lúc now, trả kết quả.
func checkupAt(t *testing.T, e *chEnv, a observability.Alerter, due, now time.Time) commands.Outcome {
	t.Helper()
	h := channel.NewDataUseCheckupCommand(e.staff.App, a, due, func() time.Time { return now }, slog.New(slog.DiscardHandler))
	if h.Kind().Name != "task."+channel.DataUseCheckupSchedule || h.Kind().Policy.Mode != queue.ModeCommand {
		t.Errorf("Kind = %+v", h.Kind())
	}
	out, err := h.Execute(t.Context(), queue.Job{ID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *chEnv) checkupAlerts(t *testing.T) int {
	t.Helper()
	return e.staff.Count(t, "SELECT count(*) FROM platform.alert_events WHERE key LIKE 'data-use-checkup:%'")
}

// Còn 29 ngày → 1 cảnh báo; 31 ngày → 0; đúng 30 ngày, quá hạn → 1; chưa đặt hạn → 0.
func TestDataUseCheckup_Window(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		due  time.Time
		want int
	}{
		{"29 ngày", now.AddDate(0, 0, 29), 1},
		{"31 ngày", now.AddDate(0, 0, 31), 0},
		{"đúng 30 ngày", now.AddDate(0, 0, 30), 1},
		{"quá hạn", now.AddDate(0, 0, -3), 1},
		{"chưa đặt hạn", time.Time{}, 0},
	} {
		e := newChEnv(t)
		a := observability.NewTelegramAlerter(e.staff.App, config.Config{}, nil, slog.New(slog.DiscardHandler))
		due := time.Time{}
		if !c.due.IsZero() {
			due = time.Date(c.due.Year(), c.due.Month(), c.due.Day(), 0, 0, 0, 0, time.UTC)
		}
		if out := checkupAt(t, e, a, due, now); out.Status != commands.StatusDone {
			t.Errorf("%s: %+v", c.name, out)
		}
		if got := e.checkupAlerts(t); got != c.want {
			t.Errorf("%s: %d cảnh báo; muốn %d", c.name, got, c.want)
		}
	}
}

// Chống lặp 7 ngày theo đồng hồ database (một đồng hồ: mốc gửi trước chèn bằng now() của database, ngoài cửa sổ 10
// phút của Alerter): đã gửi 1 ngày trước → không gửi; 6 ngày 23 giờ trước → không gửi; 7 ngày 1 giờ trước → gửi lại.
func TestDataUseCheckup_RepeatAfterSevenDays(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)
	due := time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ago  string
		want int // số dòng alert_events sau lượt chạy (gồm dòng chèn sẵn)
	}{
		{"1 day", 1},
		{"6 days 23 hours", 1},
		{"7 days 1 hour", 2},
	} {
		e := newChEnv(t)
		if _, err := e.staff.Owner.Pool.Exec(t.Context(),
			"INSERT INTO platform.alert_events (key, severity, text, sent_at) VALUES ($1, 'normal', 'nhắc trước', now() - $2::interval)",
			"data-use-checkup:2026-12-10", c.ago); err != nil {
			t.Fatal(err)
		}
		a := observability.NewTelegramAlerter(e.staff.App, config.Config{}, nil, slog.New(slog.DiscardHandler))
		if out := checkupAt(t, e, a, due, now); out.Status != commands.StatusDone {
			t.Errorf("%s: %+v", c.ago, out)
		}
		if got := e.checkupAlerts(t); got != c.want {
			t.Errorf("gửi trước %s: %d dòng; muốn %d", c.ago, got, c.want)
		}
	}
}

// Không có Alerter (cmd/crm luôn truyền) → not_sent, không panic, không ghi alert_events.
func TestDataUseCheckup_NoAlerter(t *testing.T) {
	t.Parallel()
	e := newChEnv(t)
	now := time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)
	if out := checkupAt(t, e, nil, now.AddDate(0, 0, 10), now); out.Status != commands.StatusNotSent || e.checkupAlerts(t) != 0 {
		t.Errorf("Alerter nil: %+v; muốn not_sent", out)
	}
}
