package observability_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/observability"
)

func deep(t *testing.T, e *env) observability.DeepHealthReport {
	t.Helper()
	r, err := observability.DeepHealth(t.Context(), e.d, observability.Valkey{})
	if err != nil {
		t.Fatalf("DeepHealth: %v", err)
	}
	return r
}

func TestDeepHealth(t *testing.T) {
	e := newEnv(t)
	if r := deep(t, e); r.OK || r.WorkerHeartbeatAgeS != nil {
		t.Fatalf("chưa có nhịp sống: muốn OK false, %+v", r)
	}
	fresh(t, e)
	e.job(t, "t.a", "dead", `{}`, 0, ago(0))
	e.job(t, "t.b", "in_doubt", `{}`, 0, ago(0))
	e.job(t, "t.c", "pending", `{}`, 100*time.Second, nil)
	e.metric(t, time.Now(), "ingest@a", "spool_files", "", 2)
	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES ('base', true, now() - interval '3 hours')")
	r := deep(t, e)
	if !r.OK || r.Dead != 1 || r.InDoubt != 1 || r.SpoolFiles != 2 || r.OldestPendingAgeS < 99 || r.OldestPendingAgeS > 130 {
		t.Fatalf("báo cáo: %+v", r)
	}
	if r.LastBackupAgeH == nil || *r.LastBackupAgeH != 3 || r.WorkerHeartbeatAgeS == nil || *r.WorkerHeartbeatAgeS > 30 {
		t.Fatalf("tuổi: %+v", r)
	}
	e.job(t, "t.c", "pending", `{}`, 125*time.Second, nil)
	if r := deep(t, e); r.OK {
		t.Fatalf("việc chờ 125 s phải OK false: %+v", r)
	}
}

func TestDeepHealthStaleIngest(t *testing.T) {
	e := newEnv(t)
	e.heartbeat(t, "worker@a", "worker", time.Now())
	e.heartbeat(t, "ingest@a", "ingest", time.Now().Add(-91*time.Second))
	if r := deep(t, e); r.OK {
		t.Fatalf("ingest 91 s phải OK false: %+v", r)
	}
}

// Đồng hồ Go lệch ±5 phút so với PostgreSQL: tuổi sao lưu (mốc ghi bằng now() của PostgreSQL) và tuổi WAL (đồng hồ
// PostgreSQL) không đổi; nhịp sống ghi bằng chính đồng hồ Go lệch đó vẫn tươi. Mốc 3 giờ 3 phút bắt đồng hồ Go chậm
// (cách tính cũ ra 2), 3 giờ 57 phút bắt đồng hồ Go nhanh (cách tính cũ ra 4).
func TestDeepHealthClockSkew(t *testing.T) {
	for _, skew := range []time.Duration{-5 * time.Minute, 5 * time.Minute} {
		for _, ago := range []string{"3 hours 3 minutes", "3 hours 57 minutes"} {
			t.Run(skew.String()+" "+ago, func(t *testing.T) { checkDeepHealthSkew(t, skew, ago) })
		}
	}
}

// checkDeepHealthSkew: nhịp sống ghi lúc time.Now()+skew, bản sao lưu lúc now() - ago của PostgreSQL, DeepHealth chạy
// với đồng hồ Go lệch skew.
func checkDeepHealthSkew(t *testing.T, skew time.Duration, ago string) {
	t.Helper()
	e := newEnv(t)
	goNow := time.Now().Add(skew)
	e.heartbeat(t, "worker@a", "worker", goNow)
	e.heartbeat(t, "ingest@a", "ingest", goNow)
	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES ('base', true, now() - $1::interval)", ago)
	r, err := observability.DeepHealthAt(t.Context(), e.d, goNow)
	if err != nil {
		t.Fatalf("DeepHealth: %v", err)
	}
	if r.LastBackupAgeH == nil || *r.LastBackupAgeH != 3 {
		t.Fatalf("tuổi sao lưu: muốn 3, %v", deref(r.LastBackupAgeH))
	}
	if !r.OK || r.WorkerHeartbeatAgeS == nil || *r.WorkerHeartbeatAgeS < 0 || *r.WorkerHeartbeatAgeS > 30 {
		t.Fatalf("nhịp sống: %+v", r)
	}
	want := pgWalAgeS(t, e)
	if (want == nil) != (r.LastWalArchivedAgeS == nil) ||
		(want != nil && (*r.LastWalArchivedAgeS < *want-5 || *r.LastWalArchivedAgeS > *want+5)) {
		t.Fatalf("tuổi WAL: muốn %v (đồng hồ PostgreSQL), %v", deref(want), deref(r.LastWalArchivedAgeS))
	}
}

// pgWalAgeS: tuổi (giây) lần đẩy WAL cuối theo đồng hồ PostgreSQL; nil khi archive tắt hay chưa đẩy lần nào.
func pgWalAgeS(t *testing.T, e *env) *int64 {
	t.Helper()
	var v *float64
	err := e.d.Pool.QueryRow(t.Context(), `SELECT CASE WHEN current_setting('archive_mode') <> 'off'
		THEN extract(epoch FROM now() - last_archived_time)::float8 END FROM pg_catalog.pg_stat_archiver`).Scan(&v)
	if err != nil {
		t.Fatalf("đọc pg_stat_archiver: %v", err)
	}
	if v == nil {
		return nil
	}
	n := int64(*v)
	return &n
}
