package httpapi

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strconv"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

// Cổng nhận vào của lượt mua flash sale (kế hoạch M5 Task 203 / M5-20; 05 mục 7.1, QĐ21, QĐ23): đứng sau khóa dịch vụ
// và giới hạn của khóa, trước khóa chống trùng — lượt bị từ chối ở đây không chạm PostgreSQL và không giữ khóa.

const (
	// headerAttempt: lần gửi của cùng Idempotency-Key (1–10, vắng → 1; QĐ21).
	headerAttempt = "X-Attempt"
	maxAttempt    = 10
	// flashBodyMax: thân lượt mua tối đa (route khai x-body-limit 2048; đọc thêm một byte để biết vượt).
	flashBodyMax = 2048
	// soldOutBody: câu trả lời khi cờ hết suất biết chắc (không lưu theo khóa).
	soldOutBody = `{"result":"sold_out"}`
	// flashTracer: tên tracer của các đoạn lượt mua flash sale.
	flashTracer = "elimaz.com.vn/crm/internal/httpapi/flash"
)

// FlashAdmissionOptions là tham số của NewFlashAdmission: cổng điều tiết và cờ hết suất (Gate), bộ giới hạn tần suất
// (Limiter) với giới hạn mỗi khách (Customer, cache.Limits.FlashCustomer) và mỗi IP (IP, FlashIP), Secret là
// CACHE_KEY_SECRET của SubjectKey (rỗng → 32 byte ngẫu nhiên của tiến trình); Metrics nil thì không đếm.
type FlashAdmissionOptions struct {
	Gate         cache.AdmissionGate
	Limiter      cache.RateLimiter
	Customer, IP cache.Limit
	Secret       []byte
	Metrics      *metrics.Registry
}

// flashSubject: phần thân lượt mua mà cổng cần (SĐT, IP khách).
type flashSubject struct {
	Phone    *string `json:"phone"`
	ClientIP *string `json:"clientIp"`
}

// NewFlashAdmission trả middleware cổng nhận vào "flash" (Deps.Admission["flash"]), theo thứ tự:
//
//  1. Đọc thân (≤ 2 KiB) rồi đặt lại cho bước sau. Thân không phải JSON, clientIp thiếu hay sai, X-Attempt không phải
//     số trong 1–10, itemId không phải UUID → 400 bad_request; SĐT thiếu hay sai (customer.NormalizePhone) → 400
//     phone_required.
//  2. Giới hạn theo SĐT (Customer, SubjectKey "phone") rồi theo IP (IP, SubjectKey "ip"; IPv6 theo /64) → 429 too_fast
//     kèm Retry-After.
//  3. X-Attempt ≤ 1 và Gate.SoldOut biết chắc là hết → 200 {"result":"sold_out"} (không lưu, không chạm database).
//     Từ X-Attempt 2 bỏ qua cờ (QĐ21: website gửi lại để lấy kết quả đã lưu).
//  4. Gate.Admit không được → 503 retry_later kèm Retry-After; được → chạy tiếp, trả vé khi xong.
//
// 503 của bước sau không có Retry-After (suất đang bị lượt khác khóa) được thêm Retry-After: 1. SĐT và IP chỉ đi vào
// Valkey dưới dạng HMAC; đoạn dấu vết flash.admission chỉ mang kết quả.
func NewFlashAdmission(o FlashAdmissionOptions) func(http.Handler) http.Handler {
	if len(o.Secret) == 0 {
		o.Secret = make([]byte, randomSecretLen)
		_, _ = rand.Read(o.Secret) // crypto/rand.Read không trả lỗi (Go ≥ 1.24)
	}
	o.Secret = bytes.Clone(o.Secret)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := otel.Tracer(flashTracer).Start(r.Context(), "flash.admission")
			defer span.End()
			r = r.WithContext(ctx)
			outcome := o.admit(w, r, next)
			span.SetAttributes(attribute.String("flash.admission", outcome))
			if o.Metrics != nil && outcome != admitPassed {
				o.Metrics.Inc("flash_result", outcome, 1)
			}
		})
	}
}

// Kết quả của cổng (nhãn flash_result và thuộc tính của đoạn flash.admission).
const (
	admitPassed      = "admitted"
	admitBadInput    = "bad_input"
	admitTooFast     = "too_fast"
	admitSoldOutFlag = "sold_out_flag"
	admitBusy        = "gate_busy"
)

// admit chạy bốn bước của cổng cho r; trả kết quả.
func (o FlashAdmissionOptions) admit(w http.ResponseWriter, r *http.Request, next http.Handler) string {
	ctx := r.Context()
	u, ok := authUser(ctx)
	item, err := uuid.Parse(r.PathValue("itemId"))
	if !ok || err != nil {
		writeError(w, r, http.StatusBadRequest, codeBadRequest)
		return admitBadInput
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, flashBodyMax+1))
	if mbe := (*http.MaxBytesError)(nil); errors.As(err, &mbe) || len(body) > flashBodyMax {
		writeError(w, r, http.StatusRequestEntityTooLarge, codeBodyTooLarge)
		return admitBadInput
	}
	attempt, code := flashAttempt(r.Header.Get(headerAttempt))
	var s flashSubject
	if code == "" && (err != nil || json.Unmarshal(body, &s) != nil) {
		code = codeBadRequest
	}
	var phone, ip string
	if code == "" {
		phone, ip, code = flashSubjectKeys(s)
	}
	if code != "" {
		writeError(w, r, http.StatusBadRequest, code)
		return admitBadInput
	}
	for _, c := range []struct {
		l   cache.Limit
		key string
	}{{o.Customer, cache.SubjectKey(o.Secret, "phone", phone)}, {o.IP, cache.SubjectKey(o.Secret, "ip", ip)}} {
		if d := o.Limiter.Allow(ctx, c.l, c.key); !d.Allowed {
			w.Header().Set("Retry-After", retryAfterSeconds(d.RetryAfter))
			writeError(w, r, http.StatusTooManyRequests, codeTooFast)
			return admitTooFast
		}
	}
	if attempt <= 1 {
		if soldOut, known := o.Gate.SoldOut(ctx, u.TenantID, item); known && soldOut {
			if o.Metrics != nil {
				o.Metrics.Inc("flash_gate_short_circuit", "", 1)
			}
			w.Header().Set("Content-Type", jsonContentType)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, soldOutBody)
			return admitSoldOutFlag
		}
	}
	a := o.Gate.Admit(ctx, u.TenantID, item)
	if !a.Admitted {
		w.Header().Set("Retry-After", retryAfterSeconds(a.RetryAfter))
		writeError(w, r, http.StatusServiceUnavailable, codeRetryLater)
		return admitBusy
	}
	defer a.Release()
	r.Body = io.NopCloser(bytes.NewReader(body))
	next.ServeHTTP(&retryAfterWriter{ResponseWriter: w}, r)
	return admitPassed
}

// flashAttempt đọc X-Attempt: vắng → 1; không phải số trong 1–10 → mã bad_request.
func flashAttempt(v string) (attempt int, code string) {
	if v == "" {
		return 1, ""
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > maxAttempt {
		return 0, codeBadRequest
	}
	return n, ""
}

// flashSubjectKeys trả SĐT đã chuẩn hóa và IP (IPv6 theo /64) làm chủ thể giới hạn; SĐT thiếu, sai → phone_required;
// IP thiếu, sai → bad_request.
func flashSubjectKeys(s flashSubject) (phone, ip, code string) {
	if s.Phone == nil {
		return "", "", codePhoneRequired
	}
	p, err := customer.NormalizePhone(*s.Phone)
	if err != nil {
		return "", "", codePhoneRequired
	}
	if s.ClientIP == nil {
		return "", "", codeBadRequest
	}
	addr, err := netip.ParseAddr(*s.ClientIP)
	if err != nil {
		return "", "", codeBadRequest
	}
	if addr = addr.Unmap(); addr.Is6() {
		addr = netip.PrefixFrom(addr, ipv6Bits).Masked().Addr()
	}
	return string(p), addr.String(), ""
}

// retryAfterWriter thêm Retry-After: 1 cho câu trả lời 503 chưa có (ErrFlashRetry đổi mã ở errors_web.go không đặt
// header).
type retryAfterWriter struct {
	http.ResponseWriter
}

func (w *retryAfterWriter) WriteHeader(code int) {
	if code == http.StatusServiceUnavailable && w.Header().Get("Retry-After") == "" {
		w.Header().Set("Retry-After", "1")
	}
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap cho http.ResponseController.
func (w *retryAfterWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
