package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/imager"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/testkit"
)

// testHTTP là client của imager giả khi gọi kho (MemStore).
//
//nolint:gochecknoglobals // chỉ đọc, dùng chung trong test
var testHTTP = &http.Client{Timeout: 30 * time.Second}

// publicBase là FILES_PUBLIC_BASE_URL của test.
const publicBase = "https://img.test"

// Nội dung ảnh thử điều khiển imager giả: "HANG" treo tới khi bên gọi bỏ; "REJ:<lý do>\n" trả rejected với lý do đó;
// "BUSY" trả 503 bận; "E500" trả 500; "ODD:h0", "ODD:w0", "ODD:type", "ODD:w", "ODD:fmt", "ODD:b0" trả ảnh tốt nhưng kết quả lạ
// (cao 0, rộng 0, thiếu loại đã nhận; đầu ra đầu tiên sai rộng, sai định dạng, 0 byte); nội dung khác là ảnh tốt (JPEG 2000×1500).
const (
	oddPrefix  = "ODD:"
	hangPrefix = "HANG"
	rejPrefix  = "REJ:"
	busyPrefix = "BUSY"
	e500Prefix = "E500"
)

// fakeImager là imager giả bằng httptest, nói đúng hợp đồng của internal/imager: tải nguồn bằng SourceURL, ghi từng
// đầu ra bằng PutURL kèm Headers (MemStore kiểm chữ ký, loại, If-None-Match), trả imager.Result.
type fakeImager struct {
	t       *testing.T
	srv     *httptest.Server
	release chan struct{} // đóng khi test xong: nhả các yêu cầu đang treo

	mu       sync.Mutex
	calls    int
	reqs     []imager.Request
	inflight int
	peak     int
	busy     int           // số lần trả 503 vì quá fakeCapacity
	delay    time.Duration // thời gian xử lý giả của một ảnh tốt
	onCall   func()        // khác nil: gọi khi nhận một yêu cầu hợp lệ (ngoài khóa)

	// down: imager ngừng (khởi động lại, OOM): đóng kết nối không trả lời.
	down atomic.Bool
}

// fakeCapacity là IMAGER_MAX_PARALLEL của imager giả: quá số việc này cùng lúc thì trả 503 như imager thật.
const fakeCapacity = 2

func newFakeImager(t *testing.T) *fakeImager {
	t.Helper()
	f := &fakeImager{t: t, release: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	t.Cleanup(func() { close(f.release) }) // chạy trước srv.Close
	return f
}

// client giữ tối đa fakeCapacity việc cùng lúc, như worker cấu hình IMAGER_MAX_PARALLEL bằng của imager.
func (f *fakeImager) client() *imager.Client {
	return imager.NewClient(f.srv.URL, &http.Client{}, fakeCapacity)
}

func (f *fakeImager) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeImager) requests() []imager.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]imager.Request(nil), f.reqs...)
}

func (f *fakeImager) serve(w http.ResponseWriter, r *http.Request) {
	var req imager.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Validate() != nil {
		http.Error(w, "bad_request", http.StatusBadRequest)
		return
	}
	if f.hangUpIfDown(w) {
		return
	}
	f.called()
	f.mu.Lock()
	if f.inflight >= fakeCapacity {
		f.busy++
		f.mu.Unlock()
		w.Header().Set("Retry-After", "1")
		http.Error(w, `{"error":"busy"}`, http.StatusServiceUnavailable)
		return
	}
	f.inflight++
	f.peak = max(f.peak, f.inflight)
	f.calls++
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inflight--
		f.mu.Unlock()
	}()
	src, ok := fetch(r.Context(), req.SourceURL)
	if !ok {
		writeResult(w, imager.Result{Status: imager.StatusRejected, Reason: imager.ReasonSourceFailed})
		return
	}
	switch {
	case bytes.HasPrefix(src, []byte(hangPrefix)):
		select {
		case <-r.Context().Done():
		case <-f.release:
		}
		return
	case bytes.HasPrefix(src, []byte(e500Prefix)):
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	case bytes.HasPrefix(src, []byte(busyPrefix)):
		w.Header().Set("Retry-After", "7")
		http.Error(w, `{"error":"busy"}`, http.StatusServiceUnavailable)
		return
	case bytes.HasPrefix(src, []byte(rejPrefix)):
		reason, _, _ := strings.Cut(strings.TrimPrefix(string(src), rejPrefix), "\n")
		writeResult(w, imager.Result{Status: imager.StatusRejected, Reason: reason})
		return
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	writeResult(w, goodResult(r.Context(), req, string(src)))
}

// goodResult xử lý ảnh tốt như imager giả dùng chung (đọc lại nguồn, ghi từng đầu ra bằng PutURL, trả JPEG 2000×1500),
// rồi làm lạ kết quả theo nội dung "ODD:…" (Task 403: kết quả imager lạ không được ghi ready).
func goodResult(ctx context.Context, req imager.Request, src string) imager.Result {
	res, err := testkit.StubImager{}.Process(ctx, req)
	if err != nil {
		return imager.Result{Status: imager.StatusRejected, Reason: imager.ReasonInternal}
	}
	switch src {
	case oddPrefix + "h0":
		res.Height = 0
	case oddPrefix + "w0":
		res.Width = 0
	case oddPrefix + "type":
		res.DetectedType = ""
	case oddPrefix + "w":
		res.Outputs[0].Width++
	case oddPrefix + "fmt":
		res.Outputs[0].Format = "gif"
	case oddPrefix + "b0":
		res.Outputs[0].Bytes = 0
	}
	return res
}

// hangUpIfDown: imager đang ngừng (down) → đóng kết nối không trả lời (bên gọi nhận lỗi mạng), trả true.
func (f *fakeImager) hangUpIfDown(w http.ResponseWriter) bool {
	if !f.down.Load() {
		return false
	}
	if hj, ok := w.(http.Hijacker); ok {
		if conn, _, err := hj.Hijack(); err == nil {
			_ = conn.Close()
			return true
		}
	}
	panic(http.ErrAbortHandler)
}

// called gọi onCall (nếu có) ngoài khóa.
func (f *fakeImager) called() {
	f.mu.Lock()
	hook := f.onCall
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
}

func writeResult(w http.ResponseWriter, res imager.Result) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func fetch(ctx context.Context, u string) ([]byte, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false
	}
	resp, err := testHTTP.Do(req)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return body, err == nil && resp.StatusCode == http.StatusOK
}

// bucketOf là bucket của một đường dẫn ký sẵn của MemStore (<gốc>/<bucket>/<khóa>?…).
func bucketOf(t *testing.T, raw string) (files.Bucket, string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, key, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	return files.Bucket(b), key
}

// fakeBus ghi các báo hiệu được phát (chỉ tính khi transaction commit không kiểm được ở đây; đếm số lần gọi).
type fakeBus struct {
	mu   sync.Mutex
	sigs []realtime.Signal
	// fail: Publish trả lỗi (transaction ghi trạng thái tệp lùi lại, như database lỗi mãi).
	fail atomic.Bool
}

var errBusDown = errors.New("fakeBus: lỗi")

func (b *fakeBus) Publish(_ context.Context, _ pgx.Tx, s realtime.Signal) error {
	if b.fail.Load() {
		return errBusDown
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sigs = append(b.sigs, s)
	return nil
}

func (b *fakeBus) signals() []realtime.Signal {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]realtime.Signal(nil), b.sigs...)
}

// procEnv: env của files_test cộng MemStore, imager giả, các lệnh xử lý ảnh, lệnh đối chiếu kho và một worker.
type procEnv struct {
	*env
	mem   *files.MemStore
	img   *fakeImager
	bus   *fakeBus
	reg   *metrics.Registry
	pub   commands.Handler
	priv  commands.Handler
	regen commands.Handler
	ver   *files.Verifier
	w     *queue.Worker
	q     queue.JobQueue
}

func newProcEnv(t *testing.T) *procEnv {
	t.Helper()
	return newProcEnvWith(t, nil)
}

// newProcEnvWith như newProcEnv, opt (khác nil) sửa ProcessorOptions trước khi dựng các lệnh.
func newProcEnvWith(t *testing.T, opt func(*files.ProcessorOptions)) *procEnv {
	t.Helper()
	p := &procEnv{
		env: newEnv(t), mem: testkit.MemFileStore(t), img: newFakeImager(t), bus: &fakeBus{},
		reg: metrics.NewRegistry(), q: queue.NewQueue(),
	}
	files.RegisterPurpose(testPrivateImagePurpose())
	if err := p.addPurposeRow("test_private_image", "private", "private"); err != nil {
		t.Fatal(err)
	}
	o := files.ProcessorOptions{
		DB: p.staff.App, Store: p.mem, Imager: p.img.client(), PresetVersion: 1, Bus: p.bus, Metrics: p.reg,
	}
	if opt != nil {
		opt(&o)
	}
	p.pub, p.priv, p.regen = files.NewProcessHandler(o), files.NewProcessPrivateHandler(o), files.NewRegenerateHandler(o)
	p.ver = files.NewVerifyHandler(o)
	p.w = p.newWorker()
	return p
}

func (p *procEnv) newWorker() *queue.Worker {
	return queue.NewWorker(p.staff.App, nil, queue.WorkerOptions{
		WorkerID: "w-" + uuid.NewString(), Poll: 50 * time.Millisecond,
		Commands: commands.NewRunner(p.staff.App, []commands.Handler{p.pub, p.priv, p.regen, p.ver}),
	})
}

// drain chạy worker tới khi không còn việc đến hạn (testkit.DrainDue: chịu được đồng hồ PostgreSQL lùi; việc hẹn thử
// lại giữ lịch).
func (p *procEnv) drain() {
	p.t.Helper()
	testkit.DrainDue(p.t, p.staff.Owner, p.w)
}

// makeDue đưa mọi việc pending về hạn chạy ngay (thay cho chờ lịch thử lại). Bỏ qua dòng đang bị khóa (worker chạy
// song song đang nhận hay ghi kết quả việc đó) thay vì chờ: chờ khóa dòng của worker dưới tải có thể quá lock_timeout
// (55P03) và làm test đỏ oan; nơi gọi lúc worker chạy song song gọi lặp, lần sau bắt được dòng đó.
func (p *procEnv) makeDue() {
	p.t.Helper()
	if _, err := p.staff.Owner.Pool.Exec(p.t.Context(), `UPDATE platform.jobs SET run_at = now() WHERE id IN (
		SELECT id FROM platform.jobs WHERE status = 'pending' FOR UPDATE SKIP LOCKED)`); err != nil {
		p.t.Fatal(err)
	}
}

// upload tải body lên như nhân viên (CreateUpload, PUT, CompleteUpload) cho mục đích purpose; trả mã tệp.
func (p *procEnv) upload(tenant uuid.UUID, purpose string, body []byte) uuid.UUID {
	p.t.Helper()
	f, put := p.mustCreate(p.mem, files.UploadInput{
		TenantID: tenant, Purpose: purpose, FileName: "anh.jpg", ContentType: "image/jpeg", SizeBytes: int64(len(body)),
	})
	if code := putVia(p.t, put, body); code != http.StatusOK {
		p.t.Fatalf("PUT: %d", code)
	}
	if _, err := p.complete(p.mem, tenant, f.ID); err != nil {
		p.t.Fatalf("CompleteUpload: %v", err)
	}
	return f.ID
}

// registerPrivate ghi body vào TmpKey rồi RegisterPrivateImage (đường máy chủ ghi của M4, M5).
func (p *procEnv) registerPrivate(tenant uuid.UUID, body []byte) uuid.UUID {
	p.t.Helper()
	id := uuid.Must(uuid.NewV7())
	if err := p.mem.Put(p.t.Context(), files.BucketTmp, files.TmpKey(tenant, id), bytes.NewReader(body), int64(len(body)), "image/jpeg"); err != nil {
		p.t.Fatal(err)
	}
	got, err := p.registerPrivateRow(tenant, files.NewPrivateImage{
		ID: id, Purpose: "test_private_image", Size: int64(len(body)), ContentType: "image/jpeg", FileName: "khach.jpg",
	})
	if err != nil {
		p.t.Fatalf("RegisterPrivateImage: %v", err)
	}
	if got != id {
		p.t.Fatalf("RegisterPrivateImage trả %s, muốn %s", got, id)
	}
	return id
}

func (p *procEnv) registerPrivateRow(tenant uuid.UUID, in files.NewPrivateImage) (uuid.UUID, error) {
	var id uuid.UUID
	err := p.inTenant(tenant, func(tx pgx.Tx) error {
		var err error
		id, err = files.RegisterPrivateImage(p.t.Context(), tx, p.q, in)
		return err //nolint:wrapcheck // test
	})
	return id, err
}

// info đọc tệp kèm biến thể công khai (như GET /api/v1/files/{id}).
func (p *procEnv) info(tenant, id uuid.UUID) files.Info {
	p.t.Helper()
	var info files.Info
	p.must(tenant, func(tx pgx.Tx) error {
		var err error
		info, err = files.Describe(p.t.Context(), tx, id, publicBase)
		return err //nolint:wrapcheck // test
	})
	return info
}

type variantRow struct {
	Preset, Width int
	Format        string
	Bucket, Key   string
	Size          int64
}

// asOwner chạy fn bằng chủ bảng lần lượt trong ngữ cảnh tenant A rồi B (RLS ép cả chủ bảng); dừng ở tenant đầu tiên
// mà fn trả found.
func (p *procEnv) asOwner(fn func(tx pgx.Tx) (bool, error)) {
	p.t.Helper()
	for _, tenant := range []uuid.UUID{p.a, p.b} {
		found := false
		err := pgx.BeginFunc(p.t.Context(), p.staff.Owner.Pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(p.t.Context(), "SELECT set_config('app.tenant_id', $1, true)", tenant.String()); err != nil {
				return err //nolint:wrapcheck // test
			}
			var err error
			found, err = fn(tx)
			return err
		})
		if err != nil {
			p.t.Fatal(err)
		}
		if found {
			return
		}
	}
}

// variants đọc mọi dòng file_variants của tệp bằng chủ bảng.
func (p *procEnv) variants(id uuid.UUID) []variantRow {
	p.t.Helper()
	var out []variantRow
	p.asOwner(func(tx pgx.Tx) (bool, error) {
		rows, err := tx.Query(p.t.Context(),
			`SELECT preset_version, width, format, bucket, key, size_bytes FROM platform.file_variants WHERE file_id = $1
			ORDER BY width, format`, id)
		if err != nil {
			return false, err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var v variantRow
			if err := rows.Scan(&v.Preset, &v.Width, &v.Format, &v.Bucket, &v.Key, &v.Size); err != nil {
				return false, err //nolint:wrapcheck // test
			}
			out = append(out, v)
		}
		return len(out) > 0, rows.Err()
	})
	return out
}

type jobState struct {
	Status    string
	Attempts  int
	LastError string
}

// job đọc lệnh kind của tệp id (DedupeKey = mã tệp).
func (p *procEnv) job(kind string, id uuid.UUID) jobState {
	p.t.Helper()
	var s jobState
	var last *string
	if err := p.staff.Owner.Pool.QueryRow(p.t.Context(),
		"SELECT status, attempts, last_error FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2", kind, id.String()).
		Scan(&s.Status, &s.Attempts, &last); err != nil {
		p.t.Fatalf("đọc lệnh %s của %s: %v", kind, id, err)
	}
	if last != nil {
		s.LastError = *last
	}
	return s
}

// fileRow đọc trạng thái, lý do, mã băm, kích thước của tệp bằng chủ bảng.
type fileRow struct {
	Status, Reason, Detected, Bucket, Key string
	SHA                                   []byte
	Width, Height, Preset                 *int
}

func (p *procEnv) fileRow(id uuid.UUID) fileRow {
	p.t.Helper()
	var r fileRow
	var reason, detected *string
	found := false
	p.asOwner(func(tx pgx.Tx) (bool, error) {
		err := tx.QueryRow(p.t.Context(), `SELECT status, reject_reason, detected_type, bucket, key,
			content_sha256, width, height, preset_version FROM platform.files WHERE id = $1`, id).
			Scan(&r.Status, &reason, &detected, &r.Bucket, &r.Key, &r.SHA, &r.Width, &r.Height, &r.Preset)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		found = err == nil
		return found, err //nolint:wrapcheck // test
	})
	if !found {
		p.t.Fatalf("không có tệp %s", id)
	}
	if reason != nil {
		r.Reason = *reason
	}
	if detected != nil {
		r.Detected = *detected
	}
	return r
}

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// metricCount trả Count của chỉ số name, nhãn label trong reg (Drain lấy rồi xóa).
func metricRows(reg *metrics.Registry) map[string]int64 {
	out := map[string]int64{}
	for _, r := range reg.Drain() {
		out[r.Name+"|"+r.Label] += r.Count
	}
	return out
}
