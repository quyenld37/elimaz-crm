package observability_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/observability"
)

func deep(t *testing.T, e *env) observability.DeepHealthReport {
	t.Helper()
	r, err := observability.DeepHealth(t.Context(), e.d)
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
	fin := time.Now()
	e.job(t, "t.a", "dead", `{}`, time.Now(), &fin)
	e.job(t, "t.b", "in_doubt", `{}`, time.Now(), &fin)
	e.job(t, "t.c", "pending", `{}`, time.Now().Add(-100*time.Second), nil)
	e.metric(t, time.Now(), "ingest@a", "spool_files", "", 2)
	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES ('base', true, now() - interval '3 hours')")
	r := deep(t, e)
	if !r.OK || r.Dead != 1 || r.InDoubt != 1 || r.SpoolFiles != 2 || r.OldestPendingAgeS < 99 || r.OldestPendingAgeS > 130 {
		t.Fatalf("báo cáo: %+v", r)
	}
	if r.LastBackupAgeH == nil || *r.LastBackupAgeH != 3 || r.WorkerHeartbeatAgeS == nil || *r.WorkerHeartbeatAgeS > 30 {
		t.Fatalf("tuổi: %+v", r)
	}
	e.job(t, "t.c", "pending", `{}`, time.Now().Add(-125*time.Second), nil)
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
