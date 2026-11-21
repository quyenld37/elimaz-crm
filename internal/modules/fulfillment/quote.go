package fulfillment

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Báo phí các hãng (kế hoạch M3, Task M3-13; A19): QuoteNow gọi Quote của adapter cho từng tài khoản song song, ngoài
// transaction, có hạn; lệnh command.shipping.quote (quote_command.go) và báo phí đồng bộ của website (M5) cùng dùng.
// Không bao giờ trả phí 0 thay cho lỗi: phí ≤ 0 hay quá 2^53−1 bị bỏ, không còn dịch vụ nào → rejected.

// Mã lỗi báo phí (shipping_quotes.error_code, FeeQuote.ErrorCode).
const (
	// QuoteErrTimeout: hãng không trả lời trong hạn, lỗi mạng, 5xx, kết quả không rõ.
	QuoteErrTimeout = "timeout"
	// QuoteErrRateLimited: hãng báo 429 (FeeQuote.RetryAfter là thời gian hãng xin chờ).
	QuoteErrRateLimited = "rate_limited"
	// QuoteErrAuth: hãng từ chối khóa API (FeeQuote.AuthCode là mã lỗi của hãng).
	QuoteErrAuth = "auth"
	// QuoteErrAddressUnmapped: địa chỉ giao chưa khớp mã địa giới của hãng (không gọi hãng).
	QuoteErrAddressUnmapped = "address_unmapped"
	// QuoteErrRejected: hãng từ chối, không báo phí được (không có adapter, không hỗ trợ) hay không còn phí hợp lệ nào.
	QuoteErrRejected = "rejected"
)

const (
	// maxQuoteCall là hạn tối đa của một lần gọi hãng (hạn ≤ 0 hay lớn hơn đều lấy giá trị này).
	maxQuoteCall = 10 * time.Second
	// maxQuoteFee là phí lớn nhất nhận (số nguyên an toàn của JSON, CHECK của shipping_quotes.fee_total).
	maxQuoteFee int64 = 1<<53 - 1
)

// FeeQuote là kết quả báo phí của một tài khoản: Results (phí > 0, mỗi dịch vụ một dòng, rẻ nhất trước) xor ErrorCode
// (QuoteErr*). RetryAfter chỉ có với rate_limited; AuthCode (mã lỗi của hãng, không dữ liệu cá nhân) chỉ có với auth.
type FeeQuote struct {
	AccountID  uuid.UUID
	CarrierKey string
	Results    []QuoteResult
	ErrorCode  string
	RetryAfter time.Duration
	AuthCode   string
}

// QuoteNow gọi Quote của adapter cho mọi tài khoản accounts song song, ngoài transaction (không đọc database), mỗi lần
// gọi có hạn timeout (≤ 0 hay quá 10 s → 10 s; adapter phải dừng theo ctx, kết quả về sau hạn bị bỏ); trả đúng một FeeQuote mỗi
// tài khoản theo thứ tự vào. Lỗi một hãng không làm hỏng hãng khác. Không có adapter hay adapter không báo phí →
// rejected không gọi; adapter có Locations mà địa chỉ giao (tỉnh, phường) chưa khớp acc.Locations → address_unmapped
// không gọi.
func (s *Service) QuoteNow(ctx context.Context, accounts []Account, in QuoteInput, timeout time.Duration) []FeeQuote {
	if timeout <= 0 || timeout > maxQuoteCall {
		timeout = maxQuoteCall
	}
	out := make([]FeeQuote, len(accounts))
	var wg sync.WaitGroup
	for i, acc := range accounts {
		wg.Go(func() { out[i] = s.quoteOne(ctx, acc, in, timeout) })
	}
	wg.Wait()
	return out
}

type quoteAnswer struct {
	res []QuoteResult
	err error
}

func (s *Service) quoteOne(ctx context.Context, acc Account, in QuoteInput, timeout time.Duration) FeeQuote {
	fq := FeeQuote{AccountID: acc.ID, CarrierKey: acc.CarrierKey}
	ad, ok := s.adapter(acc.CarrierKey)
	if !ok || !ad.Capabilities().Quote {
		fq.ErrorCode = QuoteErrRejected
		return fq
	}
	if ad.Capabilities().Locations && !addressMapped(acc.Locations, in.To) {
		fq.ErrorCode = QuoteErrAddressUnmapped
		return fq
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := ad.Quote(cctx, acc, in)
	if err == nil && cctx.Err() != nil { // trả sau hạn: không nhận (hạn là lời hứa với bên gọi)
		err = cctx.Err()
	}
	a := quoteAnswer{res: res, err: err}
	if a.err != nil {
		fq.ErrorCode, fq.RetryAfter, fq.AuthCode = classifyQuoteError(a.err)
		return fq
	}
	if fq.Results = cleanQuotes(a.res); len(fq.Results) == 0 {
		fq.ErrorCode = QuoteErrRejected
	}
	return fq
}

// addressMapped: địa chỉ có tỉnh và phường, cả hai đã có mã của hãng trong m.
func addressMapped(m LocationMap, to Address) bool {
	if m == nil || to.ProvinceCode == "" || to.WardCode == "" {
		return false
	}
	_, p := m.Carrier(levelProvince, to.ProvinceCode)
	_, w := m.Carrier(levelWard, to.WardCode)
	return p && w
}

// classifyQuoteError đổi lỗi gọi hãng thành mã báo phí: 429 → rate_limited (kèm Retry-After); 401/403 → auth (kèm mã
// của hãng); hãng từ chối, lỗi vĩnh viễn, ErrRejected, ErrNotSupported → rejected; còn lại (quá hạn, lỗi mạng, 5xx,
// chưa gửi, không rõ) → timeout.
func classifyQuoteError(err error) (code string, retryAfter time.Duration, authCode string) {
	if ce, ok := errors.AsType[*CarrierError](err); ok {
		switch ce.Class {
		case ClassRateLimited:
			return QuoteErrRateLimited, ce.RetryAfter, ""
		case ClassAuth:
			return QuoteErrAuth, 0, ce.Code
		case ClassRejected, ClassPermanent:
			return QuoteErrRejected, 0, ""
		case ClassNotSentRetry, ClassUnknownOutcome:
			return QuoteErrTimeout, 0, ""
		}
		return QuoteErrTimeout, 0, ""
	}
	if errors.Is(err, ErrRejected) || errors.Is(err, ErrNotSupported) {
		return QuoteErrRejected, 0, ""
	}
	return QuoteErrTimeout, 0, ""
}

// cleanQuotes bỏ phí ≤ 0 hay quá maxQuoteFee, gộp dịch vụ trùng (giữ phí thấp nhất), xếp phí tăng dần rồi mã dịch vụ.
func cleanQuotes(rs []QuoteResult) []QuoteResult {
	best := map[string]QuoteResult{}
	for _, r := range rs {
		if r.Total <= 0 || r.Total > maxQuoteFee {
			continue
		}
		if cur, ok := best[r.Service]; !ok || r.Total < cur.Total {
			best[r.Service] = r
		}
	}
	out := make([]QuoteResult, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b QuoteResult) int {
		return cmp.Or(cmp.Compare(a.Total, b.Total), cmp.Compare(a.Service, b.Service))
	})
	return out
}
