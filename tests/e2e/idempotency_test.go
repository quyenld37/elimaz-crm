package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Cùng một yêu cầu ghi gửi 50 lần song song (Review Focus 4), qua HTTP thật tới api: lệnh command.selftest bị bên
// ngoài từ chối khi worker thật chạy (lệnh thử coi mọi lỗi là không chắc: in_doubt); POST
// /api/v1/platform/jobs/{id}/retry cùng Idempotency-Key 50 lần song song → đúng 1 dòng audit, việc về pending một
// lần, mỗi câu trả lời là 200 cùng thân hoặc 409.
func TestRetryFiftyParallelOneResult(t *testing.T) {
	// Không t.Parallel (Task 484): newStack đặt tracer provider của cả tiến trình (telemetry.Setup).
	s := newStack(t, stackOptions{})
	s.ext.SetMode(testkit.FakeReject)
	ctx := t.Context()
	var id uuid.UUID
	if err := tenancy.WithTenant(ctx, s.app, s.tenant, func(tx pgx.Tx) error {
		var err error
		id, _, err = queue.NewQueue().Enqueue(ctx, tx, selftest.CommandKind(), queue.NewJob{TenantID: &s.tenant, DedupeKey: "rej-1"})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "lệnh bị từ chối thành in_doubt", func() bool {
		return s.count("SELECT count(*) FROM platform.jobs WHERE id = $1 AND status = 'in_doubt'", id) == 1
	})
	s.ext.SetMode(testkit.FakeOK) // nếu worker chạy lại lệnh sau retry thì cũng chỉ một lần

	replies := s.postParallel(50, "/api/v1/platform/jobs/"+id.String()+"/retry", "e2e-50")
	ok := checkSameOrConflict(t, replies)
	var audits int
	if err := tenancy.WithTenant(context.Background(), s.owner, s.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1", id.String()).Scan(&audits)
	}); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("muốn đúng 1 dòng audit, nhận %d", audits)
	}
	// Việc về pending đúng một lần: chạy lại đúng một lần nữa rồi done (bên ngoài nhận 1 lần), không bị retry lần hai.
	waitFor(t, 10*time.Second, "lệnh chạy lại xong", func() bool {
		return s.count("SELECT count(*) FROM platform.jobs WHERE id = $1 AND status = 'done'", id) == 1
	})
	if calls := s.ext.Calls(); len(calls) != 1 {
		t.Fatalf("bên ngoài nhận %d lần sau retry, muốn 1", len(calls))
	}
	t.Logf("50 yêu cầu song song: %d trả 200 cùng thân, %d trả 409", ok, len(replies)-ok)
}

type reply struct {
	status int
	body   string
}

// postParallel gửi n yêu cầu POST path (thân {}) song song bằng thẻ chủ shop, cùng Idempotency-Key key.
func (s *stack) postParallel(n int, path, key string) []reply {
	s.t.Helper()
	client := &http.Client{Timeout: 30 * time.Second}
	replies := make([]reply, n)
	var wg sync.WaitGroup
	for i := range replies {
		wg.Go(func() {
			req, err := http.NewRequestWithContext(s.t.Context(), http.MethodPost, s.api.URL+path, strings.NewReader("{}"))
			if err != nil {
				s.t.Error(err)
				return
			}
			req.Header.Set("Cf-Access-Jwt-Assertion", s.token)
			req.Header.Set("Origin", stackOrigin)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", key)
			resp, err := client.Do(req)
			if err != nil {
				s.t.Error(err)
				return
			}
			defer func() { _ = resp.Body.Close() }()
			b, _ := io.ReadAll(resp.Body)
			replies[i] = reply{resp.StatusCode, string(b)}
		})
	}
	wg.Wait()
	return replies
}

// checkSameOrConflict đòi mỗi câu trả lời là 200 cùng một thân (cùng giá trị JSON: câu phát lại đọc từ cột jsonb nên
// thứ tự khóa và khoảng trắng có thể khác bản gốc, xem Việc phát hiện thêm của task 18) hoặc 409 có mã lỗi, ít nhất
// một 200; trả số 200.
func checkSameOrConflict(t *testing.T, replies []reply) int {
	t.Helper()
	var first string
	ok := 0
	for i, r := range replies {
		switch r.status {
		case http.StatusOK:
			ok++
			if first == "" {
				first = r.body
			}
			if !sameJSON(r.body, first) {
				t.Fatalf("câu trả lời %d khác thân: %s / %s", i, r.body, first)
			}
		case http.StatusConflict:
			var e struct {
				Error string `json:"error"`
			}
			if json.Unmarshal([]byte(r.body), &e) != nil || e.Error == "" {
				t.Fatalf("409 không đúng dạng lỗi: %s", r.body)
			}
		default:
			t.Fatalf("câu trả lời %d: %d %s, muốn 200 hoặc 409", i, r.status, r.body)
		}
	}
	if ok == 0 {
		t.Fatal("không có câu trả lời 200 nào")
	}
	return ok
}

// sameJSON: a và b là cùng một giá trị JSON.
func sameJSON(a, b string) bool {
	var x, y any
	return json.Unmarshal([]byte(a), &x) == nil && json.Unmarshal([]byte(b), &y) == nil && reflect.DeepEqual(x, y)
}
