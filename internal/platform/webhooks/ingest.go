package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

// MaxBody là cỡ thân tối đa của một POST /hooks/* (1 MiB); lớn hơn thì 413.
const MaxBody = 1 << 20

const (
	// defaultMaxInFlight: quá chừng này POST /hooks/* đang xử lý thì 503 (spec 04 mục 11).
	defaultMaxInFlight = 200
	// defaultStaleSpoolAge: còn tệp tạm cũ hơn chừng này thì /health báo degraded (spec 04 mục 5.2: 5 phút).
	defaultStaleSpoolAge = 300 * time.Second
	// saveTimeout: hạn của một lần lưu vào database, tách khỏi việc hủy của yêu cầu (bên gửi ngắt không làm mất bản
	// đã nhận); quá hạn thì ghi tệp tạm.
	saveTimeout = 5 * time.Second
)

// Mã lỗi ingest trả cho bên gửi, body {"error": "<mã>", "request_id": "<id>"} như api. Đây là chỗ duy nhất đổi lỗi
// của ingest sang mã HTTP (ingest không có hợp đồng openapi.yaml, không đi qua internal/httpapi).
const (
	codeNotFound     = "not_found"
	codeForbidden    = "forbidden"
	codeBlocked      = "rate_limited"
	codeBusy         = "busy"
	codeBodyTooLarge = "body_too_large"
	codeBadRequest   = "bad_request"
	codeBadSignature = "bad_signature"
	codeUnavailable  = "unavailable"
)

// IngestOptions cấu hình Ingest.
type IngestOptions struct {
	DB      *db.DB
	Sources []Source
	Spool   *Spool
	Lockout *Lockout
	// MaxInFlight bằng 0 là 200.
	MaxInFlight int
	// StaleSpoolAge bằng 0 là 300 s.
	StaleSpoolAge time.Duration
	// Metrics nil: không ghi chỉ số.
	Metrics *metrics.Registry
	Logger  *slog.Logger
}

// Ingest phục vụ GET, POST /hooks/{source} và GET /health của vai trò ingest.
type Ingest struct {
	o        IngestOptions
	sources  map[string]Source
	inFlight chan struct{}
	save     func(ctx context.Context, e Entry) error
}

// ErrInvalidOptions báo IngestOptions thiếu hoặc sai.
var ErrInvalidOptions = errors.New("webhooks: tùy chọn ingest không hợp lệ")

// NewIngest kiểm o và dựng Ingest. Hai nguồn cùng Key, thiếu DB, Spool hay Lockout là lỗi.
func NewIngest(o IngestOptions) (*Ingest, error) {
	switch {
	case o.DB == nil, o.Spool == nil, o.Lockout == nil:
		return nil, fmt.Errorf("%w: thiếu DB, Spool hoặc Lockout", ErrInvalidOptions)
	case o.Logger == nil:
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.MaxInFlight <= 0 {
		o.MaxInFlight = defaultMaxInFlight
	}
	if o.StaleSpoolAge <= 0 {
		o.StaleSpoolAge = defaultStaleSpoolAge
	}
	in := &Ingest{o: o, sources: map[string]Source{}, inFlight: make(chan struct{}, o.MaxInFlight)}
	for _, s := range o.Sources {
		if _, dup := in.sources[s.Key()]; dup || s.Key() == "" {
			return nil, fmt.Errorf("%w: khóa nguồn %q rỗng hoặc trùng", ErrInvalidOptions, s.Key())
		}
		in.sources[s.Key()] = s
	}
	in.save = func(ctx context.Context, e Entry) error {
		_, err := Save(ctx, o.DB, e)
		return err
	}
	return in, nil
}

// Register đăng ký GET /hooks/{source}, POST /hooks/{source} và GET /health vào mux. POST mở span máy chủ (otelhttp)
// làm gốc dấu vết của webhook: việc tách gói ghi trace_context từ span này, nên ingest, worker và báo hiệu realtime
// nằm trên một dấu vết. Điểm công khai: dấu vết bên gửi đưa tới chỉ thành liên kết, không thành cha.
func (in *Ingest) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /hooks/{source}", in.challenge)
	const receivePattern = "POST /hooks/{source}"
	mux.Handle(receivePattern, otelhttp.NewHandler(http.HandlerFunc(in.receive), receivePattern, otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true })))
	mux.HandleFunc("GET /health", in.health)
}

// challenge trả lời bước GET xác minh: nguồn lạ 404; Challenge đúng thì 200 text/plain đúng chuỗi; không thì 403.
func (in *Ingest) challenge(w http.ResponseWriter, r *http.Request) {
	src, ok := in.sources[r.PathValue("source")]
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound)
		return
	}
	answer, ok := src.Challenge(r.URL.Query())
	if !ok {
		writeError(w, http.StatusForbidden, codeForbidden)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, answer) //nolint:gosec // G705: text/plain + nosniff, chỉ trả khi mã xác minh đúng (bước GET của nguồn)
}

// receive nhận một POST: nguồn lạ 404; địa chỉ đang bị chặn 429 (trước khi đọc thân); quá MaxInFlight 503; thân quá
// MaxBody 413; chữ ký sai 401 (đếm cho Lockout). Hợp lệ: lưu vào database, lỗi thì ghi tệp tạm, cả hai lỗi thì 503;
// còn lại 200 {"received":true}.
func (in *Ingest) receive(w http.ResponseWriter, r *http.Request) {
	srcKey := r.PathValue("source")
	src, ok := in.sources[srcKey]
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound)
		return
	}
	ip := ClientIP(r)
	if in.o.Lockout.Blocked(ip) {
		in.inc("hooks_blocked", "")
		writeError(w, http.StatusTooManyRequests, codeBlocked)
		return
	}
	select {
	case in.inFlight <- struct{}{}:
		defer func() { <-in.inFlight }()
	default:
		in.inc("hooks_busy", "")
		writeError(w, http.StatusServiceUnavailable, codeBusy)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
			writeError(w, http.StatusRequestEntityTooLarge, codeBodyTooLarge)
			return
		}
		writeError(w, http.StatusBadRequest, codeBadRequest)
		return
	}
	if !src.VerifySignature(raw, r.Header) {
		in.o.Lockout.NoteFailure(ip)
		in.inc("hooks_bad_signature", srcKey)
		writeError(w, http.StatusUnauthorized, codeBadSignature)
		return
	}
	headers, err := json.Marshal(logging.Redact(map[string][]string(r.Header), 0))
	if err != nil {
		headers = []byte(emptyHeadersObject)
	}
	e := Entry{Source: srcKey, Headers: headers, Body: raw, ReceivedAt: time.Now().UTC()}
	if !in.store(r.Context(), e) {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, `{"received":true}`)
}

// store lưu e vào database; lỗi thì ghi tệp tạm. false khi cả hai đều lỗi. Log một lần ở đây (mép HTTP), không kèm
// thân hay header.
func (in *Ingest) store(ctx context.Context, e Entry) bool {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveTimeout)
	defer cancel()
	dbErr := in.save(sctx, e)
	if dbErr == nil {
		return true
	}
	name, err := in.o.Spool.Write(e)
	if err != nil {
		in.inc("hooks_spool_failed", e.Source)
		in.o.Logger.ErrorContext(ctx, "không lưu được webhook vào database lẫn tệp tạm",
			slog.String("source", e.Source), slog.String("db_error", dbErr.Error()), slog.String("error", err.Error()))
		return false
	}
	in.inc("hooks_spooled", e.Source)
	in.o.Logger.WarnContext(ctx, "database không ghi được, đã ghi webhook ra tệp tạm",
		slog.String("source", e.Source), slog.String("file", name), slog.String("error", dbErr.Error()))
	return true
}

// healthBody là thân của GET /health của ingest.
type healthBody struct {
	Status                string  `json:"status"`
	SpoolFiles            int     `json:"spoolFiles"`
	OldestSpoolAgeSeconds float64 `json:"oldestSpoolAgeSeconds"`
}

// health luôn 200: "degraded" khi còn tệp tạm cũ hơn StaleSpoolAge hoặc không đọc được thư mục tệp tạm.
func (in *Ingest) health(w http.ResponseWriter, _ *http.Request) {
	files, age, err := in.o.Spool.Stats()
	b := healthBody{Status: "ok", SpoolFiles: files, OldestSpoolAgeSeconds: math.Round(age.Seconds())}
	if err != nil || (files > 0 && age > in.o.StaleSpoolAge) {
		b.Status = "degraded"
	}
	out, _ := json.Marshal(b) //nolint:errchkjson // struct chỉ có chuỗi và số
	writeJSON(w, http.StatusOK, string(out))
}

func (in *Ingest) inc(name, label string) {
	if in.o.Metrics != nil {
		in.o.Metrics.Inc(name, label, 1)
	}
}

type errorBody struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id"`
}

// writeError ghi lỗi dạng {"error": "<mã>", "request_id": "<id>"}; không bao giờ có vết lỗi.
func writeError(w http.ResponseWriter, status int, code string) {
	b, _ := json.Marshal(errorBody{Error: code, RequestID: uuid.NewString()}) //nolint:errchkjson // hai chuỗi
	writeJSON(w, status, string(b))
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body+"\n")
}
