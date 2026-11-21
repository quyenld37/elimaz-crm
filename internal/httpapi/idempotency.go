package httpapi

import (
	"bytes"
	"context"
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

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/idempotency"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Chế độ chống trùng của Route (ADR-41, ADR-47), khớp giá trị routegen sinh.
const (
	modeRequired = "required"
	modeNoReplay = "no-replay"
)

const (
	headerIdempotencyKey = "Idempotency-Key"
	headerReplayed       = "Idempotent-Replayed"
	// jsonContentType: Content-Type của kết quả đã lưu, lần đầu và lần phát lại; trùng giá trị ogen đặt cho JSON.
	jsonContentType = "application/json; charset=utf-8"
)

// idempotencyKeyPattern: Idempotency-Key là 1–128 ký tự [A-Za-z0-9_-] (kế hoạch M0, Global Constraints; ADR-41).
// Trùng chữ với requestIDPattern nhưng là luật khác (ghi ở mục Tái dùng của Task 9).
var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

var (
	// ErrNoClaim báo WriteTx được gọi khi yêu cầu không có khóa chống trùng (route exempt, hay gọi lần hai). Là lỗi lập
	// trình: handler của route exempt không được tưởng mình được chống trùng (ADR-47). writeHandlerError trả 500.
	ErrNoClaim = errors.New("httpapi: WriteTx gọi khi yêu cầu không có khóa chống trùng (ErrNoClaim)")

	// errNoActor: route chống trùng mà không có nhân viên trong ctx (route công khai không dựng được phạm vi khóa).
	errNoActor = errors.New("httpapi: route chống trùng không có người gọi")
	// errNotFinal lùi transaction của WriteTx khi fn trả câu trả lời "thử lại".
	errNotFinal = errors.New("httpapi: câu trả lời chưa phải kết quả cuối")
	// errRawNotJSON: fn trả json.RawMessage không phải JSON hợp lệ (cột lưu đòi JSON); lỗi lập trình, 500.
	errRawNotJSON = errors.New("httpapi: json.RawMessage của kết quả không phải JSON hợp lệ")
)

// claimState là khóa mà middleware đã ghi nhận cho yêu cầu; WriteTx dùng một lần.
type claimState struct {
	claim    idempotency.Claim
	tenantID uuid.UUID
	noReplay bool
	used     bool                // WriteTx đã chạy (chỉ goroutine của yêu cầu đọc, ghi)
	result   *idempotency.Stored // kết quả cuối WriteTx đã commit (route lưu thân); middleware ghi nó làm câu trả lời
}

type claimKey struct{}

func claimFrom(ctx context.Context) (*claimState, bool) {
	st, ok := ctx.Value(claimKey{}).(*claimState)
	return st, ok
}

// idempotent là middleware chống trùng của route ghi required và no-replay (đặt sau auth, guard, userRateLimit).
// Thiếu header → 400 idempotency_key_required; sai mẫu → 400 idempotency_key_invalid. Ghi nhận khóa (phạm vi: tenant,
// idempotencyActor, mẫu route; mã băm: method, đường dẫn thật kèm query, thân chuẩn hóa) bằng idempotency.Acquire:
// đã xong → phát lại nguyên Status, Body kèm Idempotent-Replayed: true (no-replay: 409 already_processed); lỗi đổi ở
// writeHandlerError. Khóa mới: đặt Claim vào ctx cho WriteTx. Handler trả lời mà không gọi WriteTx thì khóa được trả
// lại (Release) để lần gửi lại chạy từ đầu.
//
// Lần đầu và lần phát lại giống hệt từng byte (Task 396): khi WriteTx đã commit kết quả cuối của route lưu thân, mọi
// thứ handler ghi sau đó (ogen mã hóa lại, có thể khác byte: map lặp ngẫu nhiên, thoát ký tự) bị bỏ và middleware ghi
// đúng Status, Body đã lưu — kể cả khi handler sau đó trả lỗi (dữ liệu đã commit). Header khác handler đặt vẫn giữ.
func idempotent(d Deps, rt Route, next http.Handler) http.Handler {
	noReplay := rt.IdempotencyMode == modeNoReplay
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		key := r.Header.Get(headerIdempotencyKey)
		switch {
		case key == "":
			writeError(w, r, http.StatusBadRequest, codeIdempotencyKeyRequired)
			return
		case !idempotencyKeyPattern.MatchString(key):
			writeError(w, r, http.StatusBadRequest, codeIdempotencyKeyInvalid)
			return
		}
		u, ok := authUser(ctx)
		if !ok {
			writeHandlerError(d, w, r, errNoActor)
			return
		}
		body, err := io.ReadAll(r.Body) // bodyLimit đã giới hạn (Route.BodyLimit, mặc định 1 MiB)
		if err != nil {
			writeHandlerError(d, w, r, fmt.Errorf("đọc thân yêu cầu: %w", err))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		target := r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		scope := idempotency.Scope{TenantID: u.TenantID, Actor: idempotencyActor(u), Route: rt.Pattern}
		claim, stored, err := idempotency.Acquire(ctx, d.DB, scope, key, idempotency.RequestHash(r.Method, target, body), time.Now())
		switch {
		case err != nil:
			writeHandlerError(d, w, r, err)
			return
		case stored != nil && noReplay:
			writeError(w, r, http.StatusConflict, codeAlreadyProcessed)
			return
		case stored != nil:
			replay(w, stored)
			return
		}
		st := &claimState{claim: claim, tenantID: u.TenantID, noReplay: noReplay}
		defer func() {
			if !st.used {
				releaseClaim(ctx, d, claim)
			}
		}()
		next.ServeHTTP(&storedWriter{ResponseWriter: w, st: st}, r.WithContext(context.WithValue(ctx, claimKey{}, st)))
		if st.result != nil {
			writeStored(w, st.result)
		}
	})
}

// idempotencyActor là người gọi trong phạm vi khóa chống trùng: "svc:<ServiceKeyID>" cho khóa dịch vụ (Task 189),
// "user:<UserID>" cho nhân viên (giữ dạng của M0 để khóa đang chạy không đổi phạm vi).
func idempotencyActor(u identity.AuthUser) string {
	if u.Kind == identity.KindService {
		return "svc:" + u.ServiceKeyID.String()
	}
	return "user:" + u.UserID.String()
}

// storedWriter bỏ mọi thứ handler ghi sau khi WriteTx đã lưu kết quả cuối (st.result), để idempotent ghi đúng kết quả
// đó; trước đó (từ chối, "thử lại", lỗi, no-replay) ghi thẳng.
type storedWriter struct {
	http.ResponseWriter

	st *claimState
}

func (w *storedWriter) WriteHeader(code int) {
	if w.st.result == nil {
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *storedWriter) Write(b []byte) (int, error) {
	if w.st.result == nil {
		return w.ResponseWriter.Write(b) //nolint:wrapcheck // ResponseWriter của net/http, trả nguyên
	}
	return len(b), nil
}

// Unwrap cho http.ResponseController.
func (w *storedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// writeTxOptions: db.TxAPI, lock_timeout thay bằng d.WriteLockTimeout khi có.
func writeTxOptions(d Deps) db.TxOptions {
	o := db.TxAPI
	if d.WriteLockTimeout > 0 {
		o.LockTimeout = d.WriteLockTimeout
	}
	return o
}

// replay trả lại nguyên kết quả đã lưu, kèm Idempotent-Replayed: true.
func replay(w http.ResponseWriter, s *idempotency.Stored) {
	w.Header().Set(headerReplayed, "true")
	writeStored(w, s)
}

// writeStored ghi đúng Status và từng byte Body đã lưu; Content-Type là jsonContentType khi có thân (lần đầu và lần
// phát lại như nhau).
func writeStored(w http.ResponseWriter, s *idempotency.Stored) {
	h := w.Header()
	h.Del("Content-Length")
	if s.Body != nil {
		h.Set("Content-Type", jsonContentType)
	} else {
		h.Del("Content-Type")
	}
	w.WriteHeader(s.Status)
	_, _ = w.Write(s.Body) //nolint:gosec // G705: thân JSON do chính handler trả và lưu, Content-Type JSON
}

// releaseClaim trả lại khóa; lỗi chỉ ghi log (dòng khóa tự hết hạn giữ chỗ sau 60 s, lần gửi lại nhận 409 tới đó).
func releaseClaim(ctx context.Context, d Deps, c idempotency.Claim) {
	if err := c.Release(ctx); err != nil {
		d.Logger.LogAttrs(ctx, slog.LevelWarn, "không trả lại được khóa chống trùng",
			slog.String("request_id", RequestID(ctx)), logging.Err(err))
	}
}

// WriteTx chạy fn trong tenancy.WithTenant(tenantID) với khóa chống trùng của yêu cầu (ADR-41). fn trả (status,
// body, final, err): final thì Claim.Complete ghi status và body (json.RawMessage lưu nguyên từng byte, kiểu khác qua
// json.Marshal; route no-replay không lưu thân) trong cùng transaction rồi commit; sau commit, chính status và byte
// đã lưu là câu trả lời của yêu cầu (middleware idempotent ghi, bỏ thân handler ghi). Không final (câu trả lời "thử
// lại") thì lùi và trả lại khóa, WriteTx trả nil và handler tự ghi câu trả lời đó. fn có thể chạy tới 4 lần
// (ADR-46): không gọi mạng, gán lại kết quả ở mỗi lần.
//
// Lỗi: db.ErrRetryLater → lùi, trả lại khóa, trả lỗi (writeHandlerError: 503 retry_later, Retry-After: 1; không lưu).
// Lỗi khác của fn → lùi, dòng khóa giữ running tới hết 60 s. Yêu cầu không có khóa (route exempt), gọi lần hai, hay
// tenantID khác tenant của người gọi → ErrNoClaim.
func WriteTx(ctx context.Context, d Deps, tenantID uuid.UUID, fn func(tx pgx.Tx) (status int, body any, final bool, err error)) error {
	st, ok := claimFrom(ctx)
	switch {
	case !ok:
		return ErrNoClaim
	case st.used:
		return fmt.Errorf("%w: WriteTx đã chạy cho yêu cầu này", ErrNoClaim)
	case st.tenantID != tenantID:
		return fmt.Errorf("%w: tenant khác tenant của người gọi", ErrNoClaim)
	}
	st.used = true
	var result *idempotency.Stored
	err := tenancy.WithTenant(ctx, d.DB, tenantID, func(tx pgx.Tx) error {
		result = nil // fn có thể chạy lại: gán lại kết quả ở mỗi lần
		status, body, final, err := fn(tx)
		if err != nil {
			return err
		}
		if !final {
			return errNotFinal
		}
		var raw []byte
		if !st.noReplay {
			if raw, err = encodeResult(body); err != nil {
				return err
			}
		}
		if err := st.claim.Complete(ctx, tx, status, raw); err != nil {
			return err //nolint:wrapcheck // idempotency đã bọc
		}
		if !st.noReplay {
			result = &idempotency.Stored{Status: status, Body: raw}
		}
		return nil
	}, writeTxOptions(d))
	switch {
	case err == nil:
		st.result = result
		return nil
	case errors.Is(err, errNotFinal):
		releaseClaim(ctx, d, st.claim)
		return nil
	case errors.Is(err, db.ErrRetryLater):
		releaseClaim(ctx, d, st.claim)
	}
	return err //nolint:wrapcheck // lỗi của fn và của RetryTx trả nguyên cho writeHandlerError
}

// encodeResult là byte sẽ lưu của kết quả: nil → nil; json.RawMessage → nguyên byte (phải là JSON hợp lệ); kiểu khác
// → json.Marshal.
func encodeResult(body any) ([]byte, error) {
	switch b := body.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		if b == nil {
			return nil, nil
		}
		if !json.Valid(b) {
			return nil, errRawNotJSON
		}
		return bytes.Clone(b), nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả để lưu: %w", err)
	}
	return raw, nil
}
