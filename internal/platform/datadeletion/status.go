package datadeletion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

const (
	// maxForm: cỡ tối đa của form POST (signed_request vài trăm byte).
	maxForm = 16 << 10
	// codeBytes: mã xác nhận 16 byte ngẫu nhiên (32 ký tự hex).
	codeBytes = 16
	// saveTimeout: hạn ghi database, tách khỏi việc hủy của yêu cầu; quá hạn thì ghi tệp tạm.
	saveTimeout = 5 * time.Second
	// statusTimeout: hạn đọc trạng thái.
	statusTimeout = 5 * time.Second
	// vnOffset: giờ Việt Nam (UTC+7, không đổi giờ) của trang trạng thái.
	vnOffset = 7 * 60 * 60
	// Mã lỗi trả cho bên gọi, body {"error": "<mã>", "request_id": "<id>"} như ingest.
	codeBadSignature = "bad_signature"
	codeBadRequest   = "bad_request"
	codeNotFound     = "not_found"
	codeUnavailable  = "unavailable"
)

var codePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ErrInvalidOptions báo EndpointOptions thiếu trường.
var ErrInvalidOptions = errors.New("datadeletion: tùy chọn không hợp lệ")

// EndpointOptions cấu hình Endpoint.
type EndpointOptions struct {
	DB    *db.DB
	Spool *Spool
	// Source là khóa nguồn trong đường dẫn /hooks/<Source>/data-deletion và cột source (vd "facebook").
	Source string
	// BaseURL là gốc công khai của /hooks (HOOKS_PUBLIC_BASE_URL, không có "/" cuối): url trả cho Meta.
	BaseURL string
	// Parse kiểm signed_request và trả mã người dùng app; lỗi → 400.
	Parse  func(signedRequest string) (string, error)
	Logger *slog.Logger
}

// Endpoint phục vụ POST /hooks/<Source>/data-deletion và GET /hooks/<Source>/data-deletion/status/{code} ở ingest.
type Endpoint struct {
	o EndpointOptions
}

// NewEndpoint kiểm o và dựng Endpoint.
func NewEndpoint(o EndpointOptions) (*Endpoint, error) {
	if o.DB == nil || o.Spool == nil || o.Source == "" || o.BaseURL == "" || o.Parse == nil {
		return nil, fmt.Errorf("%w: thiếu DB, Spool, Source, BaseURL hoặc Parse", ErrInvalidOptions)
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return &Endpoint{o: o}, nil
}

func (e *Endpoint) path() string { return "/hooks/" + e.o.Source + "/data-deletion" }

// Register đăng ký hai đường vào mux (cụ thể hơn POST /hooks/{source}/{routeKey} của webhook nên thắng khi trùng).
func (e *Endpoint) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST "+e.path(), e.receive)
	mux.HandleFunc("GET "+e.path()+"/status/{code}", e.status)
}

// receive: signed_request sai → 400 bad_signature; đúng → mã xác nhận mới, ghi yêu cầu (database lỗi → tệp tạm; cả
// hai lỗi → 503 để Meta gọi lại), trả {"url", "confirmation_code"}. Không log mã người dùng hay signed_request.
func (e *Endpoint) receive(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxForm)
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest)
		return
	}
	userID, err := e.o.Parse(r.PostForm.Get("signed_request"))
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadSignature)
		return
	}
	raw := make([]byte, codeBytes)
	_, _ = rand.Read(raw) // crypto/rand.Read không bao giờ lỗi (Go ≥ 1.24)
	req := request{Code: hex.EncodeToString(raw), Source: e.o.Source, UserRefHash: UserRefHash(userID), ReceivedAt: time.Now().UTC()}
	if !e.store(r.Context(), req) {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable)
		return
	}
	body, _ := json.Marshal(map[string]string{ //nolint:errchkjson // hai chuỗi
		"url": e.o.BaseURL + e.path() + "/status/" + req.Code, "confirmation_code": req.Code,
	})
	writeBody(w, http.StatusOK, "application/json", string(body))
}

// store ghi req vào database; lỗi thì ghi tệp tạm. false khi cả hai lỗi. Log một lần ở đây (mép HTTP).
func (e *Endpoint) store(ctx context.Context, req request) bool {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveTimeout)
	defer cancel()
	dbErr := e.o.DB.RetryTx(sctx, db.TxIngest, func(tx pgx.Tx) error { return insert(sctx, tx, req) })
	if dbErr == nil {
		return true
	}
	if err := e.o.Spool.Write(req); err != nil {
		e.o.Logger.ErrorContext(ctx, "không lưu được yêu cầu xóa dữ liệu vào database lẫn tệp tạm",
			slog.String("code", req.Code), slog.String("db_error", logging.ErrorText(dbErr)), logging.Err(err))
		return false
	}
	e.o.Logger.WarnContext(ctx, "database không ghi được, đã ghi yêu cầu xóa dữ liệu ra tệp tạm",
		slog.String("code", req.Code), logging.Err(dbErr))
	return true
}

// status trả chữ thuần "Yêu cầu <mã>: đã xử lý lúc <giờ Việt Nam>" hoặc "Yêu cầu <mã>: đang xử lý"; mã sai mẫu hay
// không có → 404; database lỗi → 503.
func (e *Endpoint) status(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if !codePattern.MatchString(code) {
		writeError(w, http.StatusNotFound, codeNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), statusTimeout)
	defer cancel()
	row, err := store.New(e.o.DB.Pool).GetDataDeletionRequest(ctx, code)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, codeNotFound)
		return
	case err != nil:
		e.o.Logger.WarnContext(ctx, "đọc trạng thái yêu cầu xóa dữ liệu", logging.Err(err))
		writeError(w, http.StatusServiceUnavailable, codeUnavailable)
		return
	}
	text := "Yêu cầu " + code + ": đang xử lý"
	if row.CompletedAt.Valid {
		text = "Yêu cầu " + code + ": đã xử lý lúc " + row.CompletedAt.Time.In(time.FixedZone("ICT", vnOffset)).Format("15:04 02/01/2006") + " (giờ Việt Nam)"
	}
	writeBody(w, http.StatusOK, "text/plain; charset=utf-8", text)
}

func writeError(w http.ResponseWriter, status int, code string) {
	b, _ := json.Marshal(map[string]string{"error": code, "request_id": uuid.NewString()}) //nolint:errchkjson // hai chuỗi
	writeBody(w, status, "application/json", string(b))
}

func writeBody(w http.ResponseWriter, status int, contentType, body string) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body) //nolint:gosec // G705: body là JSON hoặc chữ thuần do máy chủ dựng (mã đã khớp mẫu hex)
}
