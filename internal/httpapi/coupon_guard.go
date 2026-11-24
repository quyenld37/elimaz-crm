package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// CouponGuard giới hạn số lần nhập mã giảm giá sai qua các route website (Task 482; threat model M5 B1 "mã bị dò"):
// đếm lần sai theo SĐT khách (cache.Limits.CouponGuessCustomer) và theo IP khách (CouponGuessIP; IPv6 theo /64; website
// không chuyển IP → theo khóa dịch vụ). Một chủ thể đã đủ Max lần sai thì mọi lần dùng mã (kể cả mã đúng) bị chặn tới
// hết cửa sổ. Khóa Valkey chỉ mang SubjectKey (HMAC), không SĐT hay IP gốc.
//
// Khi nào không dùng: không phải chốt nghiệp vụ (mã đúng/sai vẫn do promotion.Check quyết định); không gọi trong hàm
// của RetryTx.
type CouponGuard struct {
	rl       cache.RateLimiter
	customer cache.Limit
	ip       cache.Limit
	secret   []byte
}

// CouponSubject là người đang dùng mã: SĐT đã chuẩn hóa (rỗng: không có), IP khách (không hợp lệ: website không
// chuyển) và khóa dịch vụ gọi.
type CouponSubject struct {
	Phone        logging.Phone
	IP           netip.Addr
	ServiceKeyID uuid.UUID
}

// NewCouponGuard dựng CouponGuard trên rl với giới hạn customer (theo SĐT) và ip (theo IP hoặc khóa dịch vụ); secret
// là CACHE_KEY_SECRET (rỗng → 32 byte ngẫu nhiên của tiến trình, như NewWebOrderLimiter).
func NewCouponGuard(rl cache.RateLimiter, customer, ip cache.Limit, secret []byte) *CouponGuard {
	if len(secret) == 0 {
		secret = make([]byte, randomSecretLen)
		_, _ = rand.Read(secret) // crypto/rand.Read không trả lỗi (Go ≥ 1.24)
	}
	return &CouponGuard{rl: rl, customer: customer, ip: ip, secret: bytes.Clone(secret)}
}

// ipv6Bits: IPv6 đếm theo khối /64 (một máy thường giữ cả /64).
const ipv6Bits = 64

// guardSubject là một bộ đếm (giới hạn, khóa chủ thể).
type guardSubject struct {
	l   cache.Limit
	key string
}

// subjects trả các bộ đếm của s: SĐT (nếu có), rồi IP (nếu có) hoặc khóa dịch vụ.
func (g *CouponGuard) subjects(s CouponSubject) []guardSubject {
	out := make([]guardSubject, 0, 2) //nolint:mnd // SĐT + IP
	if s.Phone != "" {
		out = append(out, guardSubject{g.customer, cache.SubjectKey(g.secret, "phone", string(s.Phone))})
	}
	if ip := s.IP.Unmap(); ip.IsValid() {
		if ip.Is6() {
			ip = netip.PrefixFrom(ip, ipv6Bits).Masked().Addr()
		}
		out = append(out, guardSubject{g.ip, cache.SubjectKey(g.secret, "ip", ip.String())})
	} else {
		out = append(out, guardSubject{g.ip, "svc:" + s.ServiceKeyID.String()})
	}
	return out
}

// Blocked trả true khi s đã quá ngưỡng nhập mã sai ở một bộ đếm nào đó (không tính lượt); wait là thời gian chờ lâu nhất.
func (g *CouponGuard) Blocked(ctx context.Context, s CouponSubject) (blocked bool, wait time.Duration) {
	for _, sub := range g.subjects(s) {
		if d := g.rl.Peek(ctx, sub.l, sub.key); !d.Allowed {
			blocked, wait = true, max(wait, d.RetryAfter)
		}
	}
	return blocked, wait
}

// Record ghi kết quả một lần dùng mã của s sau khi đã kiểm: failed → tính một lần sai ở mọi bộ đếm; không thì chỉ đọc
// (Peek) cùng số bộ đếm — mã đúng và mã sai làm cùng số lệnh Valkey, không lộ đúng/sai qua thời gian phản hồi.
func (g *CouponGuard) Record(ctx context.Context, s CouponSubject, failed bool) {
	for _, sub := range g.subjects(s) {
		if failed {
			g.rl.Allow(ctx, sub.l, sub.key)
		} else {
			g.rl.Peek(ctx, sub.l, sub.key)
		}
	}
}

// couponGuessFailed: lý do mã không áp được là một lần đoán sai (mã không có, hay mã gắn SĐT khác — cả hai lộ mã có
// tồn tại không). Lý do khác (hết hạn, chưa đủ tiền…) là mã có thật, khách dùng lại nhiều lần là bình thường.
func couponGuessFailed(reason string) bool {
	return reason == string(promotion.ReasonNotFound) || reason == string(promotion.ReasonBoundOther)
}
