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
)

// claimState là khóa mà middleware đã ghi nhận cho yêu cầu; WriteTx dùng một lần.
type claimState struct {
	claim    idempotency.Claim
	tenantID uuid.UUID
	noReplay bool
	used     bool // WriteTx đã chạy (chỉ goroutine của yêu cầu đọc, ghi)
}

type claimKey struct{}

func claimFrom(ctx context.Context) (*claimState, bool) {
	st, ok := ctx.Value(claimKey{}).(*claimState)
	return st, ok
}

// idempotent là middleware chống trùng của route ghi required và no-replay (đặt sau auth, guard, userRateLimit).
// Thiếu header → 400 idempotency_key_required; sai mẫu → 400 idempotency_key_invalid. Ghi nhận khóa (phạm vi: tenant,
// "user:<mã nhân viên>", mẫu route; mã băm: method, đường dẫn thật kèm query, thân chuẩn hóa) bằng idempotency.Acquire:
// đã xong → phát lại nguyên Status, Body kèm Idempotent-Replayed: true (no-replay: 409 already_processed); lỗi đổi ở
// writeHandlerError. Khóa mới: đặt Claim vào ctx cho WriteTx. Handler trả lời mà không gọi WriteTx thì khóa được trả
// lại (Release) để lần gửi lại chạy từ đầu.
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
		body, err := io.ReadAll(r.Body) // bodyLimit đã giới hạn 1 MiB
		if err != nil {
			writeHandlerError(d, w, r, fmt.Errorf("đọc thân yêu cầu: %w", err))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		target := r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		scope := idempotency.Scope{TenantID: u.TenantID, Actor: "user:" + u.UserID.String(), Route: rt.Pattern}
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
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, claimKey{}, st)))
	})
}

// writeTxOptions: db.TxAPI, lock_timeout thay bằng d.WriteLockTimeout khi có.
func writeTxOptions(d Deps) db.TxOptions {
	o := db.TxAPI
	if d.WriteLockTimeout > 0 {
		o.LockTimeout = d.WriteLockTimeout
	}
	return o
}

// replay trả lại nguyên kết quả đã lưu.
func replay(w http.ResponseWriter, s *idempotency.Stored) {
	h := w.Header()
	h.Set(headerReplayed, "true")
	if s.Body != nil {
		h.Set("Content-Type", "application/json")
	}
	w.WriteHeader(s.Status)
	_, _ = w.Write(s.Body) //nolint:gosec // G705: thân JSON do chính handler trả và lưu, Content-Type application/json
}

// releaseClaim trả lại khóa; lỗi chỉ ghi log (dòng khóa tự hết hạn giữ chỗ sau 60 s, lần gửi lại nhận 409 tới đó).
func releaseClaim(ctx context.Context, d Deps, c idempotency.Claim) {
	if err := c.Release(ctx); err != nil {
		d.Logger.LogAttrs(ctx, slog.LevelWarn, "không trả lại được khóa chống trùng",
			slog.String("request_id", RequestID(ctx)), logging.Err(err))
	}
}

// WriteTx chạy fn trong tenancy.WithTenant(tenantID) với khóa chống trùng của yêu cầu (ADR-41). fn trả (status,
// body, final, err): final thì Claim.Complete ghi status và body (JSON; route no-replay không lưu thân) trong cùng
// transaction rồi commit; không final (câu trả lời "thử lại") thì lùi và trả lại khóa, WriteTx trả nil và handler tự
// ghi câu trả lời đó. fn có thể chạy tới 4 lần (ADR-46): không gọi mạng, gán lại kết quả ở mỗi lần.
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
	err := tenancy.WithTenant(ctx, d.DB, tenantID, func(tx pgx.Tx) error {
		status, body, final, err := fn(tx)
		if err != nil {
			return err
		}
		if !final {
			return errNotFinal
		}
		var raw []byte
		if body != nil && !st.noReplay {
			if raw, err = json.Marshal(body); err != nil {
				return fmt.Errorf("mã hóa kết quả để lưu: %w", err)
			}
		}
		return st.claim.Complete(ctx, tx, status, raw)
	}, writeTxOptions(d))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errNotFinal):
		releaseClaim(ctx, d, st.claim)
		return nil
	case errors.Is(err, db.ErrRetryLater):
		releaseClaim(ctx, d, st.claim)
	}
	return err //nolint:wrapcheck // lỗi của fn và của RetryTx trả nguyên cho writeHandlerError
}
