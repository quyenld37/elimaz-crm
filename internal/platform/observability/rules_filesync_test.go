package observability_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/observability"
)

// syncBuckets là ba bucket container filesync chép (crm-tmp không chép).
var syncBuckets = []string{"public", "originals", "private"} //nolint:gochecknoglobals // danh sách hằng của test

// syncRun ghi một dòng file_sync_runs bằng người quản trị (crm_app chỉ đọc bảng này); finished_at = at, started_at
// sớm hơn 2 phút.
func (e *env) syncRun(t *testing.T, kind, bucket string, ok bool, at time.Time) {
	t.Helper()
	c := connect(t, e.adminURL)
	_, err := c.Exec(t.Context(), `INSERT INTO platform.file_sync_runs (kind, bucket, ok, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5)`, kind, bucket, ok, at.Add(-2*time.Minute), at)
	if err != nil {
		t.Fatalf("chèn file_sync_runs: %v", err)
	}
}

// goodCopies ghi lần copy tốt của cả ba bucket lúc at.
func (e *env) goodCopies(t *testing.T, at time.Time) {
	t.Helper()
	for _, b := range syncBuckets {
		e.syncRun(t, "copy", b, true, at)
	}
}

// TP10: lần chép tốt cuối cách 89 phút → không file-sync-age; 91 phút → có, mức urgent; FILESYNC_EXPECTED=false →
// không bao giờ; lần reconcile ok = false → file-sync-mismatch.
func TestRule_FileSyncAge(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	now := time.Now()
	on := observability.RuleOptions{FileSyncExpected: true, Now: now}

	// Chưa có lần chép nào: đã bật thì báo gấp ngay (container chép chưa chạy là đúng việc phải biết).
	keys, f := check(t, e, on)
	wantKey(t, keys, "file-sync-age", true)
	if s, _ := f.find("file-sync-age"); s.severity != observability.SeverityUrgent {
		t.Fatalf("file-sync-age mức %q", s.severity)
	}

	e.goodCopies(t, now.Add(-89*time.Minute))
	keys, _ = check(t, e, on)
	wantKey(t, keys, "file-sync-age", false)
	wantKey(t, keys, "file-sync-failed", false)
	wantKey(t, keys, "file-sync-mismatch", false)

	keys, f = check(t, e, observability.RuleOptions{FileSyncExpected: true, Now: now.Add(2 * time.Minute)})
	wantKey(t, keys, "file-sync-age", true)
	s, _ := f.find("file-sync-age")
	if s.severity != observability.SeverityUrgent || !strings.Contains(s.text, "91 phút") {
		t.Fatalf("file-sync-age 91 phút: %+v", s)
	}

	// Tắt FILESYNC_EXPECTED: không luật nào của filesync, kể cả khi quá hạn, lỗi, lệch.
	e.syncRun(t, "copy", "private", false, now)
	e.syncRun(t, "reconcile", "public", false, now)
	keys, _ = check(t, e, observability.RuleOptions{FileSyncExpected: false, Now: now.Add(10 * time.Hour)})
	for _, k := range []string{"file-sync-age", "file-sync-failed", "file-sync-mismatch"} {
		wantKey(t, keys, k, false)
	}

	keys, f = check(t, e, on)
	wantKey(t, keys, "file-sync-mismatch", true)
	if s, _ := f.find("file-sync-mismatch"); s.severity != observability.SeverityNormal ||
		!strings.Contains(s.text, "public") {
		t.Fatalf("file-sync-mismatch: %+v", s)
	}
}

// Một bucket thiếu lần chép tốt cũng đủ báo: luật xét từng bucket, không lấy lần tốt nhất của cả ba.
func TestRule_FileSyncAgeAnyBucket(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	now := time.Now()
	e.syncRun(t, "copy", "public", true, now.Add(-5*time.Minute))
	e.syncRun(t, "copy", "originals", true, now.Add(-5*time.Minute))
	e.syncRun(t, "copy", "private", true, now.Add(-3*time.Hour))
	keys, f := check(t, e, observability.RuleOptions{FileSyncExpected: true, Now: now})
	wantKey(t, keys, "file-sync-age", true)
	if s, _ := f.find("file-sync-age"); !strings.Contains(s.text, "private") || strings.Contains(s.text, "public") {
		t.Fatalf("file-sync-age chỉ nêu bucket quá hạn: %q", s.text)
	}
}

// file-sync-failed theo lần copy GẦN NHẤT của mỗi bucket: lần lỗi rồi lần tốt sau đó thì hết báo.
func TestRule_FileSyncFailed(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	now := time.Now()
	e.goodCopies(t, now.Add(-70*time.Minute))
	e.syncRun(t, "copy", "originals", false, now.Add(-10*time.Minute))
	keys, f := check(t, e, observability.RuleOptions{FileSyncExpected: true, Now: now})
	wantKey(t, keys, "file-sync-failed", true)
	wantKey(t, keys, "file-sync-age", false)
	if s, _ := f.find("file-sync-failed"); s.severity != observability.SeverityNormal || !strings.Contains(s.text, "originals") {
		t.Fatalf("file-sync-failed: %+v", s)
	}
	e.syncRun(t, "copy", "originals", true, now.Add(-time.Minute))
	keys, _ = check(t, e, observability.RuleOptions{FileSyncExpected: true, Now: now})
	wantKey(t, keys, "file-sync-failed", false)
	// reconcile tốt sau reconcile lệch: hết báo lệch.
	e.syncRun(t, "reconcile", "private", false, now.Add(-30*time.Hour))
	e.syncRun(t, "reconcile", "private", true, now.Add(-6*time.Hour))
	keys, _ = check(t, e, observability.RuleOptions{FileSyncExpected: true, Now: now})
	wantKey(t, keys, "file-sync-mismatch", false)
}

// /health/deep: lastFileSyncAgeM là tuổi (phút) lần chép tốt cũ nhất trong ba bucket; nil khi còn bucket chưa có.
func TestDeepHealthFileSync(t *testing.T) {
	e := newEnv(t)
	if r := deep(t, e); r.LastFileSyncAgeM != nil {
		t.Fatalf("chưa có lần chép: muốn nil, %v", *r.LastFileSyncAgeM)
	}
	now := time.Now()
	e.syncRun(t, "copy", "public", true, now.Add(-10*time.Minute))
	e.syncRun(t, "copy", "originals", true, now.Add(-40*time.Minute))
	if r := deep(t, e); r.LastFileSyncAgeM != nil {
		t.Fatalf("private chưa có lần chép tốt: muốn nil, %v", *r.LastFileSyncAgeM)
	}
	e.syncRun(t, "copy", "private", true, now.Add(-25*time.Minute))
	e.syncRun(t, "copy", "originals", false, now.Add(-time.Minute))
	r := deep(t, e)
	if r.LastFileSyncAgeM == nil || *r.LastFileSyncAgeM != 40 {
		t.Fatalf("lastFileSyncAgeM: muốn 40 (lần tốt cũ nhất), %+v", r.LastFileSyncAgeM)
	}
}

// filesyncMember tạo role đăng nhập tạm (tên, mật khẩu ngẫu nhiên) là thành viên của crm_filesync, xóa khi test xong,
// và trả chuỗi kết nối của nó tới database thử. Role tạm có đúng quyền của crm_filesync (kế thừa) mà không chiếm kết
// nối duy nhất của crm_filesync (CONNECTION LIMIT 1 tính trên cả máy chủ PostgreSQL chung của mọi phiên test).
func filesyncMember(t *testing.T, e *env) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	role, pass := "crm_fs_test_"+hex.EncodeToString(b[:4]), hex.EncodeToString(b)
	admin := connect(t, e.adminURL)
	if _, err := admin.Exec(t.Context(), "CREATE ROLE "+role+" LOGIN NOBYPASSRLS INHERIT CONNECTION LIMIT 1 PASSWORD '"+
		pass+"' IN ROLE crm_filesync"); err != nil {
		t.Fatal(err)
	}
	// Đăng ký trước kết nối của role tạm: Cleanup chạy ngược thứ tự, kết nối đóng rồi mới xóa role.
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), e.adminURL)
		if err != nil {
			t.Errorf("xóa role %s: %v", role, err)
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		if _, err := c.Exec(context.Background(), "DROP ROLE "+role); err != nil {
			t.Errorf("xóa role %s: %v", role, err)
		}
	})
	u, err := url.Parse(e.adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, pass)
	return u.String()
}

// Role crm_filesync: chỉ INSERT vào platform.file_sync_runs. SELECT bảng bất kỳ (kể cả chính bảng đó), UPDATE,
// DELETE → lỗi quyền (42501). Thử bằng role tạm thành viên của crm_filesync (filesyncMember).
func TestFileSyncRole(t *testing.T) {
	e := newEnv(t)
	c := connect(t, filesyncMember(t, e))
	_, err := c.Exec(t.Context(), `INSERT INTO platform.file_sync_runs (kind, bucket, ok, started_at, finished_at, objects, bytes, detail)
		VALUES ('copy', 'public', true, now() - interval '1 minute', now(), 3, 30, 'chép 3')`)
	if err != nil {
		t.Fatalf("crm_filesync INSERT: %v", err)
	}
	denied := []string{
		"SELECT count(*) FROM platform.file_sync_runs",
		"SELECT count(*) FROM platform.jobs",
		"SELECT count(*) FROM platform.backup_runs",
		"SELECT count(*) FROM platform.alert_events",
		"SELECT count(*) FROM identity.users",
		"UPDATE platform.file_sync_runs SET ok = false",
		"DELETE FROM platform.file_sync_runs",
		"INSERT INTO platform.backup_runs (kind, ok) VALUES ('base', true)",
		"INSERT INTO platform.alert_events (key, severity, text) VALUES ('x', 'normal', 'x')",
	}
	for _, q := range denied {
		_, err := c.Exec(t.Context(), q)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "42501" {
			t.Errorf("%s: muốn lỗi quyền 42501, có %v", q, err)
		}
	}
	// Không tạo bảng trong schema nào (public của PostgreSQL 15+ đã đóng CREATE; platform không cấp).
	for _, q := range []string{"CREATE TABLE platform.x (a int)", "CREATE TABLE public.x (a int)"} {
		if _, err := c.Exec(t.Context(), q); err == nil {
			t.Errorf("%s: crm_filesync tạo được bảng", q)
		}
	}
	if n := e.count(t, "SELECT count(*) FROM platform.file_sync_runs WHERE ok"); n != 1 {
		t.Fatalf("dòng crm_filesync ghi: %d", n)
	}
	var limit int
	var bypass, login bool
	if err := connect(t, e.adminURL).QueryRow(t.Context(),
		"SELECT rolconnlimit, rolbypassrls, rolcanlogin FROM pg_roles WHERE rolname = 'crm_filesync'").
		Scan(&limit, &bypass, &login); err != nil {
		t.Fatal(err)
	}
	if limit != 1 || bypass || !login {
		t.Fatalf("crm_filesync: connlimit %d (muốn 1), bypassrls %v, login %v (muốn true)", limit, bypass, login)
	}
}
