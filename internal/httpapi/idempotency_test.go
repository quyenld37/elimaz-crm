package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/idempotency"
	"elimaz.com.vn/crm/internal/testkit"
)

var errProbeHandler = errors.New("probe: lỗi giữa transaction")

// idemEnv: database thật có hai nhân viên cùng tenant, máy chủ khóa Access thử, bảng thử probe.counter và ba route
// thử: POST /api/v1/_probe/counter (required), POST /api/v1/_probe/secret (no-replay), POST /api/v1/_probe/quote
// (exempt).
type idemEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	keys   *cfaccesstest.AccessKeys
	mux    *http.ServeMux
	logs   *bytes.Buffer
	tenant uuid.UUID
	users  map[string]uuid.UUID // email → mã nhân viên
	calls  atomic.Int64         // số lần handler counter chạy
}

const (
	userA = "a@elimaz.test"
	userB = "b@elimaz.test"
)

func newIdemEnv(t *testing.T) *idemEnv {
	t.Helper()
	e := &idemEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t), logs: &bytes.Buffer{}}
	e.staff.ProbeCounter(t)
	e.tenant = e.staff.Tenant(t, "A")
	e.users = map[string]uuid.UUID{}
	for _, email := range []string{userA, userB} {
		u := e.staff.User(t, e.tenant, email, "active")
		e.staff.Grant(t, e.tenant, u, []string{"platform.jobs.manage"}, "all", "")
		e.users[email] = u
	}
	authE := &authEnv{t: t, keys: e.keys}
	d := prepare(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App,
		Logger: slog.New(slog.NewJSONHandler(&lockedBuffer{buf: e.logs}, nil)), Access: authE.verifier(),
	})
	e.mux = http.NewServeMux()
	for _, p := range []struct {
		rt Route
		h  http.Handler
	}{
		{Route{
			Method: "POST", Pattern: "POST /api/v1/_probe/counter", OperationID: "probeCounter",
			Permission: "platform.jobs.manage", Write: true, IdempotencyMode: "required",
		}, e.counterHandler(d)},
		{Route{
			Method: "POST", Pattern: "POST /api/v1/_probe/secret", OperationID: "probeSecret",
			Permission: "platform.jobs.manage", Write: true, IdempotencyMode: "no-replay",
			IdempotencyReason: "trả bí mật một lần",
		}, secretHandler(d)},
		{Route{
			Method: "POST", Pattern: "POST /api/v1/_probe/quote", OperationID: "probeQuote",
			Permission: "platform.jobs.manage", Write: true, IdempotencyMode: "exempt",
			IdempotencyReason: "chỉ đọc",
		}, quoteHandler(d)},
		{Route{
			Method: "POST", Pattern: "POST /api/v1/_probe/forbidden", OperationID: "probeForbidden",
			Permission: "audit.read", Write: true, IdempotencyMode: "required",
		}, e.counterHandler(d)},
	} {
		e.mux.Handle(p.rt.Pattern, chain(d, p.rt, p.h))
	}
	return e
}

// lockedBuffer: logger dùng chung giữa các yêu cầu song song.
type lockedBuffer struct {
	mu  sync.Mutex
	buf *bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck // test
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// rawProbe: thân JSON có thứ tự khóa không theo bảng chữ cái, khoảng trắng riêng, ký tự HTML (Task 396).
const rawProbe = "{\"zeta\": 1,  \"alpha\" :[ 2,1 ],\n \"html\":\"<b>&\"}"

// counterHandler tăng bộ đếm của người gọi trong WriteTx. Thân {"mode": …}: "ok" (mặc định) kết quả cuối 200;
// "raw" kết quả cuối 200 là rawProbe (json.RawMessage); "after" kết quả cuối 200 nhưng handler lại ghi 500 sau khi
// WriteTx đã commit; "retry" câu trả lời "thử lại" 503 (final = false); "fail" lỗi giữa transaction; "busy" lỗi tạm
// hết lượt; "reject" trả 400 không gọi WriteTx.
func (e *idemEnv) counterHandler(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.calls.Add(1)
		var in struct {
			Mode string `json:"mode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Mode == "reject" {
			writeError(w, r, http.StatusBadRequest, codeBadRequest)
			return
		}
		u, _ := authUser(r.Context())
		var status int
		var body any
		//nolint:contextcheck // fn của WriteTx (kế hoạch) không nhận ctx; dùng ctx của yêu cầu
		err := WriteTx(r.Context(), d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
			var n int
			if err := tx.QueryRow(r.Context(), `INSERT INTO probe.counter (tenant_id, key, n) VALUES ($1, $2, 1)
				ON CONFLICT (tenant_id, key) DO UPDATE SET n = probe.counter.n + 1 RETURNING n`,
				u.TenantID, u.UserID.String()).Scan(&n); err != nil {
				return 0, nil, false, err //nolint:wrapcheck // test
			}
			switch in.Mode {
			case "fail":
				return 0, nil, false, errProbeHandler
			case "busy":
				return 0, nil, false, &pgconn.PgError{Code: "40P01"}
			case "retry":
				status, body = http.StatusServiceUnavailable, map[string]any{"error": "busy"}
				return status, body, false, nil
			}
			if in.Mode == "raw" {
				status, body = http.StatusOK, json.RawMessage(rawProbe)
				return status, body, true, nil
			}
			status, body = http.StatusOK, map[string]any{"count": n, "mode": in.Mode}
			return status, body, true, nil
		})
		switch {
		case err != nil:
			writeHandlerError(d, w, r, err)
		case in.Mode == "after":
			writeError(w, r, http.StatusInternalServerError, "after_commit")
		default:
			writeJSON(w, status, body) // json.Encoder: thêm "\n", khác byte của bản lưu
		}
	})
}

func secretHandler(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := authUser(r.Context())
		var body any
		err := WriteTx(r.Context(), d, u.TenantID, func(pgx.Tx) (int, any, bool, error) {
			body = map[string]any{"secret": "s-" + uuid.NewString()}
			return http.StatusOK, body, true, nil
		})
		if err != nil {
			writeHandlerError(d, w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, body)
	})
}

// quoteHandler (exempt) trả {"total": a + b} từ thân, không ghi gì; "writetx": true thì gọi WriteTx (lỗi lập trình).
func quoteHandler(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			A, B    int
			WriteTx bool `json:"writetx"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if _, ok := claimFrom(r.Context()); ok {
			writeError(w, r, http.StatusInternalServerError, "claim_on_exempt")
			return
		}
		if in.WriteTx {
			u, _ := authUser(r.Context())
			if err := WriteTx(r.Context(), d, u.TenantID, func(pgx.Tx) (int, any, bool, error) {
				return http.StatusOK, nil, true, nil
			}); err != nil {
				writeHandlerError(d, w, r, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"total": in.A + in.B})
	})
}

func (e *idemEnv) post(path, email, key, body string, mod ...func(*http.Request)) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequestWithContext(e.t.Context(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cf-Access-Jwt-Assertion", e.keys.Sign(cfaccesstest.AccessClaims{Email: email, AUD: testAUD, MFA: []string{"mfa"}}))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	for _, m := range mod {
		m(req)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

func (e *idemEnv) counter(email string) int {
	e.t.Helper()
	var n int
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(),
		"SELECT coalesce(sum(n), 0) FROM probe.counter WHERE tenant_id = $1 AND key = $2",
		e.tenant, e.users[email].String()).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

type keyRow struct {
	Status     string
	RespStatus *int32
	RespBody   *string
}

func (e *idemEnv) keyRows() []keyRow {
	e.t.Helper()
	var out []keyRow
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rs, err := tx.Query(e.t.Context(),
			"SELECT status, response_status, response_body::text FROM platform.idempotency_keys ORDER BY created_at")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = pgx.CollectRows(rs, pgx.RowToStructByPos[keyRow])
		return err //nolint:wrapcheck // test
	})
	return out
}

// ownerSQL chạy câu sửa dòng khóa bằng chủ bảng trong ngữ cảnh tenant (giả lập thời gian trôi, tiến trình chết).
func (e *idemEnv) ownerSQL(q string, args ...any) {
	e.t.Helper()
	tenantSQL(e.t, e.staff, e.tenant, q, args...)
}

// tenantSQL chạy một câu q trong ngữ cảnh tenant của staff (chủ bảng sửa dòng cho test).
func tenantSQL(t *testing.T, staff *testkit.Staff, tenant uuid.UUID, q string, args ...any) {
	t.Helper()
	staff.Exec(t, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), q, args...)
		return err //nolint:wrapcheck // test
	})
}

func jsonOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("thân không phải JSON: %v %q", err, rec.Body.String())
	}
	return m
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decodeErr(t, rec).Error
}

const counterPath = "/api/v1/_probe/counter"

// Review Focus 4: 50 yêu cầu song song cùng khóa, cùng thân → bộ đếm tăng đúng 1; mỗi câu trả lời là 200 cùng thân
// hoặc 409 request_in_progress.
func TestIdempotencyFiftyParallelSameKey(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.post(counterPath, userA, "k-50", `{"mode":"ok"}`) })
	}
	wg.Wait()
	ok := 0
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusOK:
			ok++
			if m := jsonOf(t, rec); m["count"] != float64(1) {
				t.Errorf("mọi 200 phải cùng thân count 1, có %s", rec.Body.String())
			}
		case http.StatusConflict:
			if c := errCode(t, rec); c != "request_in_progress" {
				t.Errorf("409 phải request_in_progress, có %q", c)
			}
		default:
			t.Errorf("chỉ được 200 hoặc 409, có %d %s", rec.Code, rec.Body.String())
		}
	}
	if ok == 0 {
		t.Fatal("phải có ít nhất một 200")
	}
	if n := e.counter(userA); n != 1 {
		t.Fatalf("bộ đếm phải tăng đúng 1, có %d", n)
	}
}

func TestIdempotencyHeaderRules(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	for _, c := range []struct {
		name, key, want string
	}{
		{"thiếu khóa", "", "idempotency_key_required"},
		{"quá 128 ký tự", strings.Repeat("a", 129), "idempotency_key_invalid"},
		{"ký tự lạ", "k 1", "idempotency_key_invalid"},
		{"ký tự ngoài ASCII", "khóa", "idempotency_key_invalid"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := e.post(counterPath, userA, c.key, `{}`)
			if rec.Code != http.StatusBadRequest || errCode(t, rec) != c.want {
				t.Fatalf("mong 400 %s, có %d %s", c.want, rec.Code, rec.Body.String())
			}
		})
	}
	if rec := e.post(counterPath, userA, strings.Repeat("a", 128), `{}`); rec.Code != http.StatusOK {
		t.Fatalf("khóa 128 ký tự hợp lệ phải chạy, có %d %s", rec.Code, rec.Body.String())
	}
	if e.calls.Load() != 1 {
		t.Fatalf("khóa thiếu hoặc sai không được tới handler; handler chạy %d lần", e.calls.Load())
	}
}

func TestIdempotencySameKeyDifferentBody422(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	if rec := e.post(counterPath, userA, "k1", `{"mode":"ok"}`); rec.Code != http.StatusOK {
		t.Fatalf("lần đầu phải 200, có %d", rec.Code)
	}
	rec := e.post(counterPath, userA, "k1", `{"mode":"ok","x":1}`)
	if rec.Code != http.StatusUnprocessableEntity || errCode(t, rec) != "idempotency_key_reused" {
		t.Fatalf("mong 422 idempotency_key_reused, có %d %s", rec.Code, rec.Body.String())
	}
	if n := e.counter(userA); n != 1 {
		t.Fatalf("bộ đếm phải 1, có %d", n)
	}
}

func TestIdempotencyReplayAfterOneHour(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	first := e.post(counterPath, userA, "k1", `{"mode":"ok"}`)
	if first.Code != http.StatusOK || first.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("lần đầu phải 200, không phát lại: %d %v", first.Code, first.Header())
	}
	e.ownerSQL(`UPDATE platform.idempotency_keys SET created_at = created_at - interval '1 hour',
		expires_at = expires_at - interval '1 hour', locked_until = locked_until - interval '1 hour'`)
	again := e.post(counterPath, userA, "k1", ` {"mode": "ok"} `)
	if again.Code != http.StatusOK || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gửi lại phải 200 kèm Idempotent-Replayed: true, có %d %v", again.Code, again.Header())
	}
	sameReply(t, first, again)
	if n := e.counter(userA); n != 1 || e.calls.Load() != 1 {
		t.Fatalf("bộ đếm %d, handler chạy %d lần; muốn 1 và 1", n, e.calls.Load())
	}
}

// sameReply: lần phát lại có thân giống hệt từng byte và cùng Content-Type (jsonContentType) với lần đầu (Task 396).
func sameReply(t *testing.T, first, again *httptest.ResponseRecorder) {
	t.Helper()
	if !bytes.Equal(first.Body.Bytes(), again.Body.Bytes()) {
		t.Fatalf("thân khác byte:\nlần đầu %q\nphát lại %q", first.Body.Bytes(), again.Body.Bytes())
	}
	if a, b := first.Header().Get("Content-Type"), again.Header().Get("Content-Type"); a != jsonContentType || b != a {
		t.Fatalf("Content-Type lần đầu %q, phát lại %q; muốn cùng %q", a, b, jsonContentType)
	}
}

// Task 396 (ADR-41): gửi lại cùng khóa nhận thân giống hệt từng byte và cùng Content-Type với lần đầu — kể cả khi
// handler tự ghi thân khác byte bản lưu ("ok": json.Encoder thêm xuống dòng), thân là JSON thứ tự khóa lạ, khoảng
// trắng riêng ("raw", lần đầu trả nguyên rawProbe), và handler ghi lỗi sau khi kết quả đã commit ("after": lần đầu
// cũng là kết quả đã lưu, khớp dữ liệu).
func TestIdempotencyReplayExactBytes(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	for _, mode := range []string{"ok", "raw", "after"} {
		t.Run(mode, func(t *testing.T) {
			body := `{"mode":"` + mode + `"}`
			first := e.post(counterPath, userA, "exact-"+mode, body)
			again := e.post(counterPath, userA, "exact-"+mode, body)
			if first.Code != http.StatusOK || again.Code != http.StatusOK ||
				first.Header().Get("Idempotent-Replayed") != "" || again.Header().Get("Idempotent-Replayed") != "true" {
				t.Fatalf("mong 200 rồi 200 phát lại: %d %v / %d %v", first.Code, first.Header(), again.Code, again.Header())
			}
			sameReply(t, first, again)
			if mode == "raw" && first.Body.String() != rawProbe {
				t.Fatalf("lần đầu phải đúng byte handler đưa WriteTx: %q", first.Body.String())
			}
		})
	}
	if n := e.counter(userA); n != 3 {
		t.Fatalf("mỗi khóa chạy đúng một lần: bộ đếm %d, muốn 3", n)
	}
}

// Review Focus 4: tiến trình chết sau bước ghi nhận khóa (dòng running, locked_until đã qua) → yêu cầu mới cùng khóa
// chạy được, bộ đếm tăng đúng 1. Dòng running còn hạn → 409.
func TestIdempotencyDeadProcessReclaimedAfterLock(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	body := `{"mode":"ok"}`
	userID := e.users[userA]
	insert := func(key string, lockedUntil time.Time) {
		e.ownerSQL(`INSERT INTO platform.idempotency_keys (tenant_id, actor, route, key, request_hash, status, locked_until, expires_at)
			VALUES ($1, $2, $3, $4, $5, 'running', $6, now() + interval '72 hours')`,
			e.tenant, "user:"+userID.String(), "POST /api/v1/_probe/counter", key,
			idempotency.RequestHash(http.MethodPost, counterPath, []byte(body)), lockedUntil)
	}
	insert("dead", time.Now().Add(-time.Second))
	insert("alive", time.Now().Add(time.Minute))
	if rec := e.post(counterPath, userA, "alive", body); rec.Code != http.StatusConflict || errCode(t, rec) != "request_in_progress" {
		t.Fatalf("running còn hạn phải 409, có %d %s", rec.Code, rec.Body.String())
	}
	rec := e.post(counterPath, userA, "dead", body)
	if rec.Code != http.StatusOK || jsonOf(t, rec)["count"] != float64(1) {
		t.Fatalf("running quá hạn phải chạy lại được, có %d %s", rec.Code, rec.Body.String())
	}
	if n := e.counter(userA); n != 1 {
		t.Fatalf("bộ đếm phải 1, có %d", n)
	}
}

// Câu trả lời "thử lại" (final = false) không được lưu: không còn dòng khóa; gửi lại cùng khóa chạy lại từ đầu.
func TestIdempotencyRetryAnswerNotStored(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	for i := range 2 {
		rec := e.post(counterPath, userA, "k1", `{"mode":"retry"}`)
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Idempotent-Replayed") != "" {
			t.Fatalf("lần %d: mong 503 của handler, không phát lại; có %d %v", i, rec.Code, rec.Header())
		}
		if rows := e.keyRows(); len(rows) != 0 {
			t.Fatalf("lần %d: không được còn dòng khóa: %+v", i, rows)
		}
	}
	if e.calls.Load() != 2 || e.counter(userA) != 0 {
		t.Fatalf("handler phải chạy lại (2 lần), bộ đếm 0 vì lùi; có %d, %d", e.calls.Load(), e.counter(userA))
	}
}

// Lỗi tạm hết lượt (db.ErrRetryLater) → 503 retry_later kèm Retry-After: 1, không lưu, không còn dòng khóa.
func TestIdempotencyRetryLaterNotStored(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	rec := e.post(counterPath, userA, "k1", `{"mode":"busy"}`)
	if rec.Code != http.StatusServiceUnavailable || errCode(t, rec) != "retry_later" || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("mong 503 retry_later, Retry-After 1; có %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	if rows := e.keyRows(); len(rows) != 0 {
		t.Fatalf("không được còn dòng khóa: %+v", rows)
	}
}

// Handler lỗi giữa transaction → bộ đếm không đổi, dòng khóa vẫn running (lần sau 409 tới hết 60 s).
func TestIdempotencyHandlerErrorKeepsRunning(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	if rec := e.post(counterPath, userA, "k1", `{"mode":"fail"}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("lỗi giữa transaction phải 500, có %d", rec.Code)
	}
	if n := e.counter(userA); n != 0 {
		t.Fatalf("bộ đếm phải 0, có %d", n)
	}
	if rows := e.keyRows(); len(rows) != 1 || rows[0].Status != "running" {
		t.Fatalf("dòng khóa phải còn running: %+v", rows)
	}
	if rec := e.post(counterPath, userA, "k1", `{"mode":"fail"}`); rec.Code != http.StatusConflict {
		t.Fatalf("gửi lại trong 60 s phải 409, có %d", rec.Code)
	}
}

// Handler trả lời mà không gọi WriteTx (từ chối trước khi ghi) → dòng khóa được trả lại, gửi lại chạy từ đầu.
func TestIdempotencyUnusedClaimReleased(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	for range 2 {
		if rec := e.post(counterPath, userA, "k1", `{"mode":"reject"}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("mong 400 của handler, có %d", rec.Code)
		}
		if rows := e.keyRows(); len(rows) != 0 {
			t.Fatalf("không được còn dòng khóa: %+v", rows)
		}
	}
	if e.calls.Load() != 2 {
		t.Fatalf("handler phải chạy 2 lần, có %d", e.calls.Load())
	}
}

// Bị guard từ chối thì không tạo dòng khóa (chống trùng chạy sau auth, guard).
func TestIdempotencyAfterGuard(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	if rec := e.post("/api/v1/_probe/forbidden", userA, "k1", `{}`); rec.Code != http.StatusForbidden {
		t.Fatalf("thiếu quyền phải 403, có %d", rec.Code)
	}
	if rows := e.keyRows(); len(rows) != 0 {
		t.Fatalf("yêu cầu bị từ chối không được tạo dòng khóa: %+v", rows)
	}
}

func TestIdempotencyTwoUsersIndependent(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	for _, u := range []string{userA, userB} {
		rec := e.post(counterPath, u, "same", `{"mode":"ok"}`)
		if rec.Code != http.StatusOK || rec.Header().Get("Idempotent-Replayed") != "" || jsonOf(t, rec)["count"] != float64(1) {
			t.Fatalf("%s: mong 200 count 1 không phát lại, có %d %s", u, rec.Code, rec.Body.String())
		}
	}
	if e.counter(userA) != 1 || e.counter(userB) != 1 {
		t.Fatalf("mỗi người một kết quả: %d, %d", e.counter(userA), e.counter(userB))
	}
}

// no-replay (ADR-47 bổ sung): cùng khóa cùng thân lần hai → 409 already_processed, response_body NULL, bí mật lần một
// không xuất hiện; khác thân → 422.
func TestIdempotencyNoReplay(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	first := e.post("/api/v1/_probe/secret", userA, "k1", `{}`)
	secret, _ := jsonOf(t, first)["secret"].(string)
	if first.Code != http.StatusOK || secret == "" {
		t.Fatalf("lần đầu phải 200 kèm bí mật, có %d %s", first.Code, first.Body.String())
	}
	rows := e.keyRows()
	if len(rows) != 1 || rows[0].Status != "done" || rows[0].RespBody != nil || rows[0].RespStatus == nil || *rows[0].RespStatus != 200 {
		t.Fatalf("dòng no-replay phải done, response_status 200, response_body NULL: %+v", rows)
	}
	again := e.post("/api/v1/_probe/secret", userA, "k1", `{}`)
	if again.Code != http.StatusConflict || errCode(t, again) != "already_processed" {
		t.Fatalf("lần hai phải 409 already_processed, có %d %s", again.Code, again.Body.String())
	}
	if strings.Contains(again.Body.String(), secret) || again.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("lần hai không được chứa bí mật hay phát lại: %v %s", again.Header(), again.Body.String())
	}
	if rec := e.post("/api/v1/_probe/secret", userA, "k1", `{"x":1}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("khác thân phải 422, có %d", rec.Code)
	}
	if rec := e.post("/api/v1/_probe/secret", userA, "", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no-replay vẫn bắt buộc khóa, có %d", rec.Code)
	}
}

// exempt (ADR-47): không đọc header, không ghi idempotency_keys, không Claim trong ctx; origin vẫn áp; WriteTx trên
// route exempt → 500 và ErrNoClaim trong log.
func TestIdempotencyExempt(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	const quote = "/api/v1/_probe/quote"
	if rec := e.post(quote, userA, "", `{"A":1,"B":2}`); rec.Code != http.StatusOK || jsonOf(t, rec)["total"] != float64(3) {
		t.Fatalf("không khóa phải 200 total 3, có %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range []struct {
		body  string
		total float64
	}{{`{"A":1,"B":2}`, 3}, {`{"A":5,"B":5}`, 10}} {
		rec := e.post(quote, userA, "same-key", c.body)
		if rec.Code != http.StatusOK || rec.Header().Get("Idempotent-Replayed") != "" || jsonOf(t, rec)["total"] != c.total {
			t.Fatalf("exempt kèm khóa phải chạy theo thân %s: %d %v %s", c.body, rec.Code, rec.Header(), rec.Body.String())
		}
	}
	if rows := e.keyRows(); len(rows) != 0 {
		t.Fatalf("exempt không được ghi idempotency_keys: %+v", rows)
	}
	if rec := e.post(counterPath, userA, "", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("route required bên cạnh thiếu khóa vẫn 400, có %d", rec.Code)
	}
	if rec := e.post(quote, userA, "", `{}`, func(r *http.Request) { r.Header.Del("Origin") }); rec.Code != http.StatusForbidden {
		t.Fatalf("exempt thiếu Origin phải 403, có %d", rec.Code)
	}
	rec := e.post(quote, userA, "", `{"writetx":true}`)
	if rec.Code != http.StatusInternalServerError || errCode(t, rec) != "internal" {
		t.Fatalf("WriteTx trên route exempt phải 500, có %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(e.logs.String(), ErrNoClaim.Error()) {
		t.Fatalf("log phải có ErrNoClaim, có:\n%s", e.logs.String())
	}
}
