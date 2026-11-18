package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

// jobsEnv: máy chủ thật (NewServer, bảng Routes) trên database thật; tenant A có nhân viên có quyền
// platform.jobs.manage (manager) và nhân viên không có quyền (viewer); tenant B có việc riêng.
type jobsEnv struct {
	t        *testing.T
	staff    *testkit.Staff
	keys     *testkit.AccessKeys
	srv      http.Handler
	tenantA  uuid.UUID
	tenantB  uuid.UUID
	managers map[string]uuid.UUID
}

const (
	jobsManager = "manager@elimaz.test"
	jobsViewer  = "viewer@elimaz.test"
)

func newJobsEnv(t *testing.T, lockTimeout time.Duration) *jobsEnv {
	t.Helper()
	e := &jobsEnv{t: t, staff: testkit.OpenStaff(t), keys: testkit.AccessKeyServer(t), managers: map[string]uuid.UUID{}}
	e.tenantA, e.tenantB = e.staff.Tenant(t, "A"), e.staff.Tenant(t, "B")
	m := e.staff.User(t, e.tenantA, jobsManager, "active")
	e.staff.Grant(t, e.tenantA, m, []string{"platform.jobs.manage"}, "all", "")
	e.managers[jobsManager] = m
	v := e.staff.User(t, e.tenantA, jobsViewer, "active")
	e.staff.Grant(t, e.tenantA, v, []string{"platform.health.read"}, "all", "")
	e.srv = testServer(t, e.staff, e.keys, lockTimeout)
	return e
}

// job chèn một việc bằng chủ bảng với trạng thái status (running thì có giữ chỗ).
func (e *jobsEnv) job(tenant uuid.UUID, kind, status string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.staff.Owner.Pool.QueryRow(context.Background(), `INSERT INTO platform.jobs
		(tenant_id, kind, mode, payload, dedupe_key, on_lease_expired, max_attempts, status, attempts, last_error,
		 expires_at, lease_until, locked_by)
		VALUES ($1, $2, 'command', '{"order_id":"o-1"}', $3, 'in_doubt', 8, $4, 3, 'boom', now() + interval '1 minute',
		 CASE WHEN $4 = 'running' THEN now() + interval '1 minute' END, CASE WHEN $4 = 'running' THEN 'w' END)
		RETURNING id`, tenant, kind, uuid.NewString(), status).Scan(&id); err != nil {
		e.t.Fatalf("chèn việc: %v", err)
	}
	return id
}

type jobState struct {
	Status    string
	Attempts  int
	HasExpiry bool
	RunInS    float64
}

func (e *jobsEnv) state(id uuid.UUID) jobState {
	e.t.Helper()
	var s jobState
	if err := e.staff.Owner.Pool.QueryRow(context.Background(), `SELECT status, attempts, expires_at IS NOT NULL,
		extract(epoch FROM run_at - now())::float8 FROM platform.jobs WHERE id = $1`, id).
		Scan(&s.Status, &s.Attempts, &s.HasExpiry, &s.RunInS); err != nil {
		e.t.Fatal(err)
	}
	return s
}

// audits: các dòng audit của việc id ở tenant A (action, actor_id, entity_id, after).
func (e *jobsEnv) audits(id uuid.UUID) []map[string]any {
	e.t.Helper()
	var out []map[string]any
	e.staff.Exec(e.t, e.tenantA, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT jsonb_build_object('action', action, 'actor_type', actor_type,
			'actor_id', actor_id, 'entity_type', entity_type, 'entity_id', entity_id, 'after', after, 'request_id', request_id)
			FROM identity.audit_logs WHERE entity_id = $1`, id.String())
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[map[string]any])
		return err //nolint:wrapcheck // test
	})
	return out
}

func (e *jobsEnv) keyRows() int {
	e.t.Helper()
	var n int
	e.staff.Exec(e.t, e.tenantA, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM platform.idempotency_keys").Scan(&n)
	})
	return n
}

func (e *jobsEnv) do(method, path, email, key string) *httptest.ResponseRecorder {
	e.t.Helper()
	return serveAs(e.srv, e.keys, method, path, email, key, "{}")
}

func jobPath(id uuid.UUID, action string) string {
	return "/api/v1/platform/jobs/" + id.String() + "/" + action
}

func TestListJobs(t *testing.T) {
	e := newJobsEnv(t, 0)
	dead := e.job(e.tenantA, "command.a", "dead")
	doubt := e.job(e.tenantA, "command.b", "in_doubt")
	e.job(e.tenantA, "command.c", "expired")
	e.job(e.tenantA, "command.d", "pending")
	e.job(e.tenantA, "command.e", "done")
	e.job(e.tenantB, "command.f", "dead")

	expect(t, "không quyền", e.do(http.MethodGet, "/api/v1/platform/jobs", jobsViewer, ""), http.StatusForbidden, "forbidden")

	rec := e.do(http.MethodGet, "/api/v1/platform/jobs", jobsManager, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "payload") || strings.Contains(rec.Body.String(), "o-1") {
		t.Fatalf("thân không được có payload: %s", rec.Body.String())
	}
	var all []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, j := range all {
		kinds[j["kind"].(string)] = true //nolint:forcetypeassert // test
		for _, k := range []string{"id", "kind", "status", "attempts", "lastError", "createdAt"} {
			if _, ok := j[k]; !ok {
				t.Fatalf("thiếu %s: %v", k, j)
			}
		}
	}
	if len(all) != 3 || !kinds["command.a"] || !kinds["command.b"] || !kinds["command.c"] {
		t.Fatalf("chỉ dead, in_doubt, expired của tenant A: %s", rec.Body.String())
	}

	rec = e.do(http.MethodGet, "/api/v1/platform/jobs?status=in_doubt", jobsManager, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil || len(all) != 1 || all[0]["id"] != doubt.String() {
		t.Fatalf("lọc in_doubt: %s", rec.Body.String())
	}
	rec = e.do(http.MethodGet, "/api/v1/platform/jobs?limit=1", jobsManager, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil || len(all) != 1 {
		t.Fatalf("limit 1: %s", rec.Body.String())
	}
	expect(t, "status lạ", e.do(http.MethodGet, "/api/v1/platform/jobs?status=pending", jobsManager, ""), http.StatusBadRequest, "bad_request")
	_ = dead
}

func TestRetryJob(t *testing.T) {
	e := newJobsEnv(t, 0)
	id := e.job(e.tenantA, "command.a", "dead")
	other := e.job(e.tenantB, "command.b", "dead")

	expect(t, "không quyền", e.do(http.MethodPost, jobPath(id, "retry"), jobsViewer, "k1"), http.StatusForbidden, "forbidden")
	expect(t, "thiếu khóa", e.do(http.MethodPost, jobPath(id, "retry"), jobsManager, ""), http.StatusBadRequest, "idempotency_key_required")
	expect(t, "tenant khác", e.do(http.MethodPost, jobPath(other, "retry"), jobsManager, "k2"), http.StatusNotFound, "not_found")
	expect(t, "không có", e.do(http.MethodPost, jobPath(uuid.New(), "retry"), jobsManager, "k3"), http.StatusNotFound, "not_found")
	if s := e.state(other); s.Status != "dead" {
		t.Fatalf("việc của tenant B bị đổi: %+v", s)
	}

	rec := e.do(http.MethodPost, jobPath(id, "retry"), jobsManager, "k4")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "payload") {
		t.Fatalf("retry: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "pending" || body["attempts"] != float64(0) || body["id"] != id.String() {
		t.Fatalf("thân sau retry: %v", body)
	}
	if s := e.state(id); s.Status != "pending" || s.Attempts != 0 || s.HasExpiry || s.RunInS > 1 {
		t.Fatalf("muốn pending, attempts 0, không hạn, chạy ngay: %+v", s)
	}
	au := e.audits(id)
	if len(au) != 1 || au[0]["action"] != "platform.job.retry" || au[0]["actor_type"] != "staff" ||
		au[0]["actor_id"] != e.managers[jobsManager].String() || au[0]["entity_type"] != "platform.job" ||
		au[0]["request_id"] != rec.Header().Get("X-Request-Id") {
		t.Fatalf("dòng audit: %v", au)
	}
	// Gửi lại cùng khóa: phát lại, không audit thêm.
	again := e.do(http.MethodPost, jobPath(id, "retry"), jobsManager, "k4")
	if again.Code != http.StatusOK || again.Header().Get("Idempotent-Replayed") != "true" || len(e.audits(id)) != 1 {
		t.Fatalf("gửi lại: %d %v", again.Code, again.Header())
	}
	// Việc đang pending không chạy lại được (khóa khác).
	expect(t, "pending", e.do(http.MethodPost, jobPath(id, "retry"), jobsManager, "k5"), http.StatusConflict, "job_state_conflict")
}

func TestRetryJobStates(t *testing.T) {
	e := newJobsEnv(t, 0)
	for status, want := range map[string]int{
		"dead": http.StatusOK, "in_doubt": http.StatusOK, "expired": http.StatusOK,
		"pending": http.StatusConflict, "running": http.StatusConflict, "done": http.StatusConflict,
	} {
		id := e.job(e.tenantA, "command.s", status)
		if rec := e.do(http.MethodPost, jobPath(id, "retry"), jobsManager, "r-"+status); rec.Code != want {
			t.Errorf("retry %s: %d, mong %d (%s)", status, rec.Code, want, rec.Body.String())
		}
	}
}

func TestAbandonJob(t *testing.T) {
	e := newJobsEnv(t, 0)
	for status, want := range map[string]int{
		"dead": http.StatusOK, "in_doubt": http.StatusOK,
		"pending": http.StatusConflict, "running": http.StatusConflict, "done": http.StatusConflict, "expired": http.StatusConflict,
	} {
		id := e.job(e.tenantA, "command.s", status)
		rec := e.do(http.MethodPost, jobPath(id, "abandon"), jobsManager, "a-"+status)
		if rec.Code != want {
			t.Errorf("abandon %s: %d, mong %d (%s)", status, rec.Code, want, rec.Body.String())
			continue
		}
		au := e.audits(id)
		switch {
		case want == http.StatusOK && (e.state(id).Status != "expired" || len(au) != 1 || au[0]["action"] != "platform.job.abandon"):
			t.Errorf("abandon %s: %+v %v", status, e.state(id), au)
		case want != http.StatusOK && (e.state(id).Status != status || len(au) != 0):
			t.Errorf("abandon %s bị từ chối mà vẫn đổi: %+v %v", status, e.state(id), au)
		}
	}
	other := e.job(e.tenantB, "command.b", "dead")
	expect(t, "tenant khác", e.do(http.MethodPost, jobPath(other, "abandon"), jobsManager, "a-other"), http.StatusNotFound, "not_found")
}

// Mẫu đường ghi (Review Focus 4): 50 yêu cầu song song cùng khóa → việc đổi đúng 1 lần, đúng 1 dòng audit; mỗi câu
// trả lời là 200 cùng thân hoặc 409 request_in_progress.
func TestRetryJobFiftyParallel(t *testing.T) {
	e := newJobsEnv(t, 0)
	id := e.job(e.tenantA, "command.a", "dead")
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.do(http.MethodPost, jobPath(id, "retry"), jobsManager, "k-50") })
	}
	wg.Wait()
	// Task 396: so theo byte (không so nội dung JSON), cả Content-Type; lần đầu và mọi lần phát lại như nhau.
	var first []byte
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusOK:
			if first == nil {
				first = rec.Body.Bytes()
			}
			if !bytes.Equal(rec.Body.Bytes(), first) {
				t.Errorf("mọi 200 phải cùng từng byte: %q / %q", first, rec.Body.Bytes())
			}
			if ct := rec.Header().Get("Content-Type"); ct != jsonContentType {
				t.Errorf("mọi 200 phải Content-Type %q, có %q", jsonContentType, ct)
			}
		case http.StatusConflict:
			if c := errCode(t, rec); c != "request_in_progress" {
				t.Errorf("409 phải request_in_progress, có %q", c)
			}
		default:
			t.Errorf("chỉ được 200 hoặc 409: %d %s", rec.Code, rec.Body.String())
		}
	}
	if first == nil || len(e.audits(id)) != 1 || e.state(id).Status != "pending" {
		t.Fatalf("muốn một lần đổi, một dòng audit: %v", e.audits(id))
	}
}

// ADR-46: dòng jobs bị khóa lâu hơn lock_timeout → 503 retry_later có Retry-After, không lưu kết quả, không còn dòng
// khóa, không audit; gửi lại cùng khóa sau khi nhả → 200, pending, 1 audit, không phải phát lại.
func TestRetryJobRetryLaterNotStored(t *testing.T) {
	e := newJobsEnv(t, 50*time.Millisecond)
	id := e.job(e.tenantA, "command.a", "dead")
	locked, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		holder <- e.staff.Owner.RetryTx(context.Background(), db.TxOptions{}, func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(), "SELECT 1 FROM platform.jobs WHERE id = $1 FOR UPDATE", id); err != nil {
				return err //nolint:wrapcheck // test
			}
			close(locked)
			select {
			case <-release:
			case <-time.After(2 * time.Second):
			}
			return nil
		})
	}()
	<-locked
	rec := e.do(http.MethodPost, jobPath(id, "retry"), jobsManager, "k-lock")
	close(release)
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || errCode(t, rec) != "retry_later" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("mong 503 retry_later có Retry-After: %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	if n := e.keyRows(); n != 0 {
		t.Fatalf("không được còn dòng khóa: %d", n)
	}
	if au := e.audits(id); len(au) != 0 || e.state(id).Status != "dead" {
		t.Fatalf("không được đổi gì: %v %+v", au, e.state(id))
	}
	again := e.do(http.MethodPost, jobPath(id, "retry"), jobsManager, "k-lock")
	if again.Code != http.StatusOK || again.Header().Get("Idempotent-Replayed") != "" ||
		e.state(id).Status != "pending" || len(e.audits(id)) != 1 {
		t.Fatalf("gửi lại sau khi nhả: %d %v %+v", again.Code, again.Header(), e.state(id))
	}
}
