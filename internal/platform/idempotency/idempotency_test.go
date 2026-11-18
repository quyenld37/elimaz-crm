package idempotency_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/idempotency"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// env: database thử đã migrate, hai pool (crm_app, chủ bảng) và bảng thử probe.counter mà "nghiệp vụ" tăng.
type env struct {
	staff *testkit.Staff
}

func newEnv(t testing.TB) *env {
	t.Helper()
	e := &env{staff: testkit.OpenStaff(t)}
	e.staff.ProbeCounter(t)
	return e
}

// newScoped: env, một tenant A và phạm vi của người gọi user:1 trên route thử.
func newScoped(t testing.TB) (*env, uuid.UUID, idempotency.Scope) {
	t.Helper()
	e := newEnv(t)
	a := e.staff.Tenant(t, "A")
	return e, a, scope(a, "user:1")
}

//nolint:gochecknoglobals // mốc giờ cố định của test, không gán lại
var t0 = time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)

func scope(tenant uuid.UUID, actor string) idempotency.Scope {
	return idempotency.Scope{TenantID: tenant, Actor: actor, Route: "POST /api/v1/_probe/counter"}
}

func hashOf(body string) []byte {
	return idempotency.RequestHash("POST", "/api/v1/_probe/counter", []byte(body))
}

// bump là "nghiệp vụ": tăng bộ đếm của key rồi ghi "xong" trong cùng transaction.
func (e *env) bump(ctx context.Context, tenant uuid.UUID, key string, c idempotency.Claim, status int, body []byte) error {
	return tenancy.WithTenant(ctx, e.staff.App, tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		if _, err := tx.Exec(ctx, `INSERT INTO probe.counter (tenant_id, key, n) VALUES ($1, $2, 1)
			ON CONFLICT (tenant_id, key) DO UPDATE SET n = probe.counter.n + 1`, tenant, key); err != nil {
			return err //nolint:wrapcheck // test
		}
		return c.Complete(ctx, tx, status, body)
	})
}

func (e *env) count(t testing.TB, tenant uuid.UUID, key string) int {
	t.Helper()
	var n int
	err := e.staff.Owner.Pool.QueryRow(t.Context(), "SELECT coalesce(sum(n), 0) FROM probe.counter WHERE tenant_id = $1 AND key = $2",
		tenant, key).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

type row struct {
	status      string
	lockedUntil *time.Time
	expiresAt   time.Time
	respStatus  *int32
	respBody    []byte
}

// rows đọc mọi dòng khóa của tenant bằng chủ bảng (trong ngữ cảnh tenant vì RLS ép cả chủ bảng).
func (e *env) rows(t testing.TB, tenant uuid.UUID) []row {
	t.Helper()
	var out []row
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		rs, err := tx.Query(t.Context(), `SELECT status, locked_until, expires_at, response_status, response_body::text
			FROM platform.idempotency_keys ORDER BY key`)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rs.Close()
		for rs.Next() {
			var r row
			var body *string
			if err := rs.Scan(&r.status, &r.lockedUntil, &r.expiresAt, &r.respStatus, &body); err != nil {
				return err //nolint:wrapcheck // test
			}
			if body != nil {
				r.respBody = []byte(*body)
			}
			out = append(out, r)
		}
		return rs.Err()
	})
	return out
}

//nolint:ireturn // Acquire trả Claim theo Interfaces của kế hoạch
func mustAcquire(t *testing.T, e *env, s idempotency.Scope, key, body string, now time.Time) idempotency.Claim {
	t.Helper()
	c, st, err := idempotency.Acquire(t.Context(), e.staff.App, s, key, hashOf(body), now)
	if err != nil || st != nil || c == nil {
		t.Fatalf("Acquire khóa mới phải trả Claim: %v, %v, %v", c, st, err)
	}
	return c
}

func TestAcquireNewKeyCompleteThenReplay(t *testing.T) {
	e, a, s := newScoped(t)
	c := mustAcquire(t, e, s, "k1", `{"x":1}`, t0)
	rs := e.rows(t, a)
	if len(rs) != 1 || rs[0].status != "running" || rs[0].lockedUntil == nil ||
		!rs[0].lockedUntil.Equal(t0.Add(60*time.Second)) || !rs[0].expiresAt.Equal(t0.Add(72*time.Hour)) {
		t.Fatalf("dòng mới phải running, giữ 60 s, hết hạn sau 72 giờ: %+v", rs)
	}
	if err := e.bump(t.Context(), a, "k1", c, 201, []byte(`{"count":1}`)); err != nil {
		t.Fatal(err)
	}
	// Gửi lại sau 1 giờ, cùng thân (khác khoảng trắng, thứ tự khóa) → kết quả cũ, không tạo Claim.
	c2, st, err := idempotency.Acquire(t.Context(), e.staff.App, s, "k1", hashOf(` { "x" : 1 } `), t0.Add(time.Hour))
	if err != nil || c2 != nil || st == nil {
		t.Fatalf("khóa đã xong phải trả Stored: %v, %v, %v", c2, st, err)
	}
	if st.Status != 201 || !bytes.Contains(st.Body, []byte(`"count": 1`)) {
		t.Fatalf("Stored sai: %d %s", st.Status, st.Body)
	}
	if n := e.count(t, a, "k1"); n != 1 {
		t.Fatalf("bộ đếm phải 1, có %d", n)
	}
	if rs := e.rows(t, a); rs[0].status != "done" || rs[0].respStatus == nil || *rs[0].respStatus != 201 {
		t.Fatalf("dòng phải done kèm 201: %+v", rs)
	}
}

func TestAcquireRunningIsInProgress(t *testing.T) {
	e, _, s := newScoped(t)
	mustAcquire(t, e, s, "k1", `{}`, t0)
	if _, _, err := idempotency.Acquire(t.Context(), e.staff.App, s, "k1", hashOf(`{}`), t0.Add(59*time.Second)); !errors.Is(err, idempotency.ErrInProgress) {
		t.Fatalf("running còn hạn phải ErrInProgress, có %v", err)
	}
}

func TestAcquireDifferentBodyIsKeyReused(t *testing.T) {
	e, a, s := newScoped(t)
	c := mustAcquire(t, e, s, "k1", `{"x":1}`, t0)
	if _, _, err := idempotency.Acquire(t.Context(), e.staff.App, s, "k1", hashOf(`{"x":2}`), t0); !errors.Is(err, idempotency.ErrKeyReused) {
		t.Fatalf("running, thân khác phải ErrKeyReused, có %v", err)
	}
	if err := e.bump(t.Context(), a, "k1", c, 200, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idempotency.Acquire(t.Context(), e.staff.App, s, "k1", hashOf(`{"x":2}`), t0.Add(time.Hour)); !errors.Is(err, idempotency.ErrKeyReused) {
		t.Fatalf("done, thân khác phải ErrKeyReused, có %v", err)
	}
}

// Tiến trình chết sau bước ghi nhận khóa: dòng running quá hạn được nhận lại; Claim cũ (tiến trình chậm chứ chưa
// chết) không ghi "xong" được nữa và không xóa được dòng của bên đã nhận lại.
func TestAcquireReclaimsStaleRunning(t *testing.T) {
	e, a, s := newScoped(t)
	old := mustAcquire(t, e, s, "k1", `{}`, t0)
	fresh := mustAcquire(t, e, s, "k1", `{}`, t0.Add(61*time.Second))
	if rs := e.rows(t, a); rs[0].lockedUntil == nil || !rs[0].lockedUntil.Equal(t0.Add(121*time.Second)) {
		t.Fatalf("nhận lại phải đặt hạn mới: %+v", rs)
	}
	if err := old.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(e.rows(t, a)) != 1 {
		t.Fatal("Release của Claim cũ không được xóa dòng của bên đã nhận lại")
	}
	if err := e.bump(t.Context(), a, "k1", old, 200, []byte(`{"who":"old"}`)); !errors.Is(err, idempotency.ErrClaimLost) {
		t.Fatalf("Complete của Claim cũ phải ErrClaimLost, có %v", err)
	}
	if n := e.count(t, a, "k1"); n != 0 {
		t.Fatalf("transaction của Claim cũ phải lùi, bộ đếm %d", n)
	}
	if err := e.bump(t.Context(), a, "k1", fresh, 200, []byte(`{"who":"fresh"}`)); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, a, "k1"); n != 1 {
		t.Fatalf("bộ đếm phải 1, có %d", n)
	}
}

func TestReleaseDeletesRowSoKeyRunsAgain(t *testing.T) {
	e, a, s := newScoped(t)
	c := mustAcquire(t, e, s, "k1", `{}`, t0)
	if err := c.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rs := e.rows(t, a); len(rs) != 0 {
		t.Fatalf("Release phải xóa dòng: %+v", rs)
	}
	mustAcquire(t, e, s, "k1", `{}`, t0.Add(time.Second))
}

func TestCompleteNilBodyStoresNull(t *testing.T) {
	e, a, s := newScoped(t)
	c := mustAcquire(t, e, s, "k1", `{}`, t0)
	if err := e.bump(t.Context(), a, "k1", c, 200, nil); err != nil {
		t.Fatal(err)
	}
	rs := e.rows(t, a)
	if rs[0].respBody != nil || rs[0].status != "done" {
		t.Fatalf("thân nil phải lưu NULL: %+v", rs)
	}
	_, st, err := idempotency.Acquire(t.Context(), e.staff.App, s, "k1", hashOf(`{}`), t0)
	if err != nil || st == nil || st.Status != 200 || st.Body != nil {
		t.Fatalf("Stored của thân NULL: %+v, %v", st, err)
	}
}

// Phạm vi của khóa: người gọi khác, tenant khác, route khác → khóa độc lập; tenant B không thấy dòng của A.
func TestScopesAreIndependent(t *testing.T) {
	e := newEnv(t)
	a, b := e.staff.Tenant(t, "A"), e.staff.Tenant(t, "B")
	ca := mustAcquire(t, e, scope(a, "user:1"), "k1", `{}`, t0)
	if err := e.bump(t.Context(), a, "k1", ca, 200, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	mustAcquire(t, e, scope(a, "user:2"), "k1", `{}`, t0)
	mustAcquire(t, e, scope(b, "user:1"), "k1", `{}`, t0)
	other := scope(a, "user:1")
	other.Route = "POST /api/v1/_probe/other"
	mustAcquire(t, e, other, "k1", `{}`, t0)
	if got := len(e.rows(t, b)); got != 1 {
		t.Fatalf("tenant B chỉ thấy dòng của mình, thấy %d", got)
	}
}

func TestExpiredDoneIsNewKey(t *testing.T) {
	e, a, s := newScoped(t)
	c := mustAcquire(t, e, s, "k1", `{"x":1}`, t0)
	if err := e.bump(t.Context(), a, "k1", c, 200, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	// Quá 72 giờ mà chưa bị dọn: coi như khóa mới, kể cả thân khác.
	mustAcquire(t, e, s, "k1", `{"x":2}`, t0.Add(72*time.Hour+time.Second))
	if rs := e.rows(t, a); rs[0].status != "running" || rs[0].respBody != nil || rs[0].respStatus != nil {
		t.Fatalf("khóa hết hạn phải về running, xóa kết quả cũ: %+v", rs)
	}
}

func TestAcquireConcurrentSameKeyOneClaim(t *testing.T) {
	e, _, s := newScoped(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims, inProgress := 0, 0
	for range 20 {
		wg.Go(func() {
			c, st, err := idempotency.Acquire(t.Context(), e.staff.App, s, "k1", hashOf(`{}`), t0)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && c != nil && st == nil:
				claims++
			case errors.Is(err, idempotency.ErrInProgress):
				inProgress++
			default:
				t.Errorf("kết quả lạ: %v, %v, %v", c, st, err)
			}
		})
	}
	wg.Wait()
	if claims != 1 || inProgress != 19 {
		t.Fatalf("phải đúng 1 Claim và 19 ErrInProgress, có %d và %d", claims, inProgress)
	}
}

func TestRequestHash(t *testing.T) {
	h := idempotency.RequestHash
	same := [][2][]byte{
		{h("POST", "/a", []byte(`{"a":1,"b":[1,2]}`)), h("POST", "/a", []byte(" {\n\"b\": [1, 2], \"a\": 1}\n"))},
		{h("POST", "/a", nil), h("POST", "/a", []byte(""))},
		{h("POST", "/a", []byte(`{"n":12345678901234567890}`)), h("POST", "/a", []byte(`{ "n": 12345678901234567890 }`))},
	}
	for i, p := range same {
		if !bytes.Equal(p[0], p[1]) {
			t.Errorf("cặp %d phải cùng mã băm", i)
		}
	}
	base := h("POST", "/a", []byte(`{"a":1}`))
	if len(base) != 32 {
		t.Fatalf("sha256 dài 32 byte, có %d", len(base))
	}
	for i, d := range [][]byte{
		h("PUT", "/a", []byte(`{"a":1}`)),
		h("POST", "/b", []byte(`{"a":1}`)),
		h("POST", "/a", []byte(`{"a":2}`)),
		h("POST", "/a", []byte(`{"a":"1"}`)),
		h("POST", "/a", []byte(`{"a":1`)), // JSON hỏng: băm nguyên byte
		h("POST", "/a{\"a\":1}", nil),
		h("POST", "/a", []byte(`{"a":1.0}`)),
	} {
		if bytes.Equal(base, d) {
			t.Errorf("biến thể %d phải khác mã băm", i)
		}
	}
}
