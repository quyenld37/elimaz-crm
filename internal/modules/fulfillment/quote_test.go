package fulfillment_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// quoteCarrier là hãng giả chỉ báo phí (kế hoạch M3, Task M3-13): trả results hay err, treo hang (theo ctx) trước khi
// trả; locations = true khai khả năng Locations (bên gọi kiểm địa giới trước khi gọi). Đếm số lần gọi; ghi hạn của ctx,
// lúc được gọi và việc lần treo bị ctx cắt — test kiểm hạn bằng các mốc đó (đồng hồ đơn điệu), không đo giờ thật của cả
// lời gọi (Task 400, 496: máy tải làm lời gọi chậm hơn hạn mà mã vẫn đúng).
type quoteCarrier struct {
	fulfillment.Unsupported
	key       string
	results   []fulfillment.QuoteResult
	err       error
	hang      time.Duration
	locations bool
	calls     atomic.Int32
	mu        sync.Mutex
	deadline  time.Time // hạn của ctx ở lần gọi gần nhất (zero: không có hạn)
	calledAt  time.Time // lúc lần gọi gần nhất bắt đầu
	cut       bool      // lần treo gần nhất kết thúc vì ctx hết hạn
}

// lastCall trả hạn của ctx, lúc gọi và việc lần treo có bị ctx cắt không, của lần gọi gần nhất.
func (q *quoteCarrier) lastCall() (deadline, calledAt time.Time, cut bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.deadline, q.calledAt, q.cut
}

func (q *quoteCarrier) Key() string { return q.key }

func (q *quoteCarrier) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{Quote: true, Locations: q.locations}
}

func (q *quoteCarrier) Quote(ctx context.Context, _ fulfillment.Account, _ fulfillment.QuoteInput) ([]fulfillment.QuoteResult, error) {
	q.calls.Add(1)
	dl, _ := ctx.Deadline()
	q.mu.Lock()
	q.deadline, q.calledAt, q.cut = dl, time.Now(), false
	q.mu.Unlock()
	if q.hang > 0 {
		select {
		case <-ctx.Done():
			q.mu.Lock()
			q.cut = true
			q.mu.Unlock()
			return nil, fmt.Errorf("báo phí giả: %w", ctx.Err())
		case <-time.After(q.hang):
		}
	}
	return q.results, q.err
}

// Ba hãng của VC02: A hai dịch vụ (28.000 STD, 41.000 EXP), B một (31.500), C treo 20 s.
func vc02Carriers() (a, b, c *quoteCarrier) {
	a = &quoteCarrier{key: "qa", results: []fulfillment.QuoteResult{
		{Service: "EXP", ServiceName: "Nhanh", Total: 41000, Breakdown: map[string]int64{"main": 41000}, ExpectedDays: 1},
		{Service: "STD", ServiceName: "Tiêu chuẩn", Total: 28000, Breakdown: map[string]int64{"main": 25000, "cod": 3000}, ExpectedDays: 3},
	}}
	b = &quoteCarrier{key: "qb", results: []fulfillment.QuoteResult{
		{Service: "STD", ServiceName: "Chuẩn", Total: 31500, ExpectedDays: 2},
	}}
	c = &quoteCarrier{key: "qc", hang: 20 * time.Second}
	return a, b, c
}

// QuoteNow trực tiếp với A, B, C (hạn 3 s): A, B có kết quả, C nhận ctx hạn ≤ 3 s, bị ctx cắt (không chờ hết 20 s)
// và ra timeout. Service không có database (DB nil): một câu SQL nào cũng làm test hỏng, nên QuoteNow chắc chắn không
// mở transaction.
func TestQuoteNow_ParallelTimeoutNoDB(t *testing.T) {
	t.Parallel()
	a, b, c := vc02Carriers()
	s := fulfillment.New(fulfillment.Deps{Carriers: mustRegistry(t, a, b, c)}).Service
	accs := []fulfillment.Account{
		{ID: uuid.New(), CarrierKey: "qa"}, {ID: uuid.New(), CarrierKey: "qb"}, {ID: uuid.New(), CarrierKey: "qc"},
	}
	out := s.QuoteNow(t.Context(), accs, fulfillment.QuoteInput{}, 3*time.Second)
	if dl, at, cut := c.lastCall(); dl.IsZero() || dl.Sub(at) > 3*time.Second || !cut {
		t.Fatalf("hãng treo: hạn %v, còn %s lúc gọi, bị ctx cắt %v; muốn hạn ≤ 3 s, bị cắt", dl, dl.Sub(at), cut)
	}
	if len(out) != 3 {
		t.Fatalf("FeeQuote = %d, muốn 3", len(out))
	}
	for i, want := range []struct {
		n    int
		code string
	}{{2, ""}, {1, ""}, {0, fulfillment.QuoteErrTimeout}} {
		got := out[i]
		if got.AccountID != accs[i].ID || got.CarrierKey != accs[i].CarrierKey {
			t.Errorf("FeeQuote %d của %s/%s, muốn %s/%s", i, got.AccountID, got.CarrierKey, accs[i].ID, accs[i].CarrierKey)
		}
		if len(got.Results) != want.n || got.ErrorCode != want.code {
			t.Errorf("FeeQuote %d = %d kết quả, mã %q; muốn %d, %q", i, len(got.Results), got.ErrorCode, want.n, want.code)
		}
	}
	for _, carrier := range []*quoteCarrier{a, b, c} {
		if n := carrier.calls.Load(); n != 1 {
			t.Errorf("hãng %s được gọi %d lần, muốn 1", carrier.key, n)
		}
	}
}

// Hạn lớn hơn 10 s bị kẹp về 10 s; hạn ≤ 0 lấy 10 s (không bao giờ chờ vô hạn).
func TestQuoteNow_TimeoutClamped(t *testing.T) {
	t.Parallel()
	c := &quoteCarrier{key: "qc", hang: time.Minute}
	s := fulfillment.New(fulfillment.Deps{Carriers: mustRegistry(t, c)}).Service
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	start := time.Now()
	out := s.QuoteNow(ctx, []fulfillment.Account{{ID: uuid.New(), CarrierKey: "qc"}}, fulfillment.QuoteInput{}, time.Hour)
	// Hạn kẹp đặt sau start, trước lúc hãng được gọi: hạn tính từ start ≥ 10 s, hạn còn lại lúc gọi ≤ 10 s (một đồng hồ
	// đơn điệu; không đo thời gian chạy cả lời gọi).
	dl, at, cut := c.lastCall()
	if out[0].ErrorCode != fulfillment.QuoteErrTimeout || !cut || dl.IsZero() || dl.Sub(at) > 10*time.Second ||
		dl.Sub(start) < 10*time.Second {
		t.Fatalf("hạn 1 giờ: mã %q, hạn còn %s lúc gọi (%s tính từ lúc bắt đầu), bị cắt %v; muốn timeout, hạn kẹp 10 s",
			out[0].ErrorCode, dl.Sub(at), dl.Sub(start), cut)
	}
}

// Phân loại lỗi của hãng; phí 0, âm, quá 2^53−1 bị bỏ (không bao giờ thành phí 0 thay lỗi); không còn dịch vụ → rejected;
// adapter không có (hay không báo phí được) → rejected; địa giới chưa khớp (hãng có Locations) → address_unmapped,
// không gọi hãng.
func TestQuoteNow_Classify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		carrier *quoteCarrier
		acc     fulfillment.Account
		in      fulfillment.QuoteInput
		code    string
		results int
		calls   int32
	}{
		{name: "429", carrier: &quoteCarrier{err: &fulfillment.CarrierError{Class: fulfillment.ClassRateLimited, RetryAfter: 5 * time.Second}}, code: fulfillment.QuoteErrRateLimited, calls: 1},
		{name: "401", carrier: &quoteCarrier{err: &fulfillment.CarrierError{Class: fulfillment.ClassAuth, Code: "bad_token"}}, code: fulfillment.QuoteErrAuth, calls: 1},
		{name: "từ chối", carrier: &quoteCarrier{err: &fulfillment.CarrierError{Class: fulfillment.ClassRejected}}, code: fulfillment.QuoteErrRejected, calls: 1},
		{name: "M1 từ chối", carrier: &quoteCarrier{err: fulfillment.ErrRejected}, code: fulfillment.QuoteErrRejected, calls: 1},
		{name: "không hỗ trợ", carrier: &quoteCarrier{err: fulfillment.ErrNotSupported}, code: fulfillment.QuoteErrRejected, calls: 1},
		{name: "5xx", carrier: &quoteCarrier{err: &fulfillment.CarrierError{Class: fulfillment.ClassNotSentRetry}}, code: fulfillment.QuoteErrTimeout, calls: 1},
		{name: "lỗi mạng", carrier: &quoteCarrier{err: errors.New("dial tcp: refused")}, code: fulfillment.QuoteErrTimeout, calls: 1},
		{name: "phí 0", carrier: &quoteCarrier{results: []fulfillment.QuoteResult{{Service: "STD", Total: 0}}}, code: fulfillment.QuoteErrRejected, calls: 1},
		{name: "rỗng", carrier: &quoteCarrier{}, code: fulfillment.QuoteErrRejected, calls: 1},
		{name: "bỏ phí âm, quá lớn, giữ phí đúng", carrier: &quoteCarrier{results: []fulfillment.QuoteResult{
			{Service: "A", Total: -1}, {Service: "B", Total: 1 << 53}, {Service: "C", Total: 15000},
		}}, results: 1, calls: 1},
		{name: "dịch vụ trùng giữ giá rẻ", carrier: &quoteCarrier{results: []fulfillment.QuoteResult{
			{Service: "STD", Total: 30000}, {Service: "STD", Total: 20000},
		}}, results: 1, calls: 1},
		{
			name: "chưa khớp tỉnh", carrier: &quoteCarrier{locations: true, results: []fulfillment.QuoteResult{{Service: "S", Total: 1}}},
			acc: fulfillment.Account{Locations: mapLoc{"province:79": "HCM"}}, in: fulfillment.QuoteInput{To: fulfillment.Address{ProvinceCode: "01"}},
			code: fulfillment.QuoteErrAddressUnmapped,
		},
		{
			name: "chưa khớp phường", carrier: &quoteCarrier{locations: true, results: []fulfillment.QuoteResult{{Service: "S", Total: 1}}},
			acc: fulfillment.Account{Locations: mapLoc{"province:79": "HCM"}}, in: fulfillment.QuoteInput{To: fulfillment.Address{ProvinceCode: "79", WardCode: "26734"}},
			code: fulfillment.QuoteErrAddressUnmapped,
		},
		{
			name: "thiếu tỉnh", carrier: &quoteCarrier{locations: true}, acc: fulfillment.Account{Locations: mapLoc{}},
			code: fulfillment.QuoteErrAddressUnmapped,
		},
		{
			name: "khớp đủ", carrier: &quoteCarrier{locations: true, results: []fulfillment.QuoteResult{{Service: "S", Total: 1}}},
			acc: fulfillment.Account{Locations: mapLoc{"province:79": "HCM", "ward:26734": "W1"}},
			in:  fulfillment.QuoteInput{To: fulfillment.Address{ProvinceCode: "79", WardCode: "26734"}}, results: 1, calls: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.carrier.key = "qx"
			s := fulfillment.New(fulfillment.Deps{Carriers: mustRegistry(t, c.carrier)}).Service
			acc := c.acc
			acc.ID, acc.CarrierKey = uuid.New(), "qx"
			out := s.QuoteNow(t.Context(), []fulfillment.Account{acc}, c.in, time.Second)
			if out[0].ErrorCode != c.code || len(out[0].Results) != c.results {
				t.Fatalf("= mã %q, %d kết quả; muốn %q, %d", out[0].ErrorCode, len(out[0].Results), c.code, c.results)
			}
			if n := c.carrier.calls.Load(); n != c.calls {
				t.Fatalf("gọi hãng %d lần, muốn %d", n, c.calls)
			}
			for _, r := range out[0].Results {
				if r.Total <= 0 {
					t.Fatalf("kết quả phí %d", r.Total)
				}
			}
		})
	}
	t.Run("không có adapter", func(t *testing.T) {
		s := fulfillment.New(fulfillment.Deps{}).Service
		out := s.QuoteNow(t.Context(), []fulfillment.Account{{ID: uuid.New(), CarrierKey: "nope"}}, fulfillment.QuoteInput{}, time.Second)
		if out[0].ErrorCode != fulfillment.QuoteErrRejected {
			t.Fatalf("mã %q, muốn rejected", out[0].ErrorCode)
		}
	})
	t.Run("429 giữ Retry-After", func(t *testing.T) {
		c := &quoteCarrier{key: "qx", err: &fulfillment.CarrierError{Class: fulfillment.ClassRateLimited, RetryAfter: 7 * time.Second}}
		s := fulfillment.New(fulfillment.Deps{Carriers: mustRegistry(t, c)}).Service
		out := s.QuoteNow(t.Context(), []fulfillment.Account{{ID: uuid.New(), CarrierKey: "qx"}}, fulfillment.QuoteInput{}, time.Second)
		if out[0].RetryAfter != 7*time.Second {
			t.Fatalf("RetryAfter = %s, muốn 7s", out[0].RetryAfter)
		}
	})
}

// mapLoc là LocationMap trong bộ nhớ: khóa "<level>:<mã CRM>".
type mapLoc map[string]string

func (m mapLoc) Carrier(level, our string) (string, bool) {
	v, ok := m[level+":"+our]
	return v, ok
}

// Quy luật của QuoteNow: mỗi tài khoản đúng một FeeQuote theo thứ tự vào; có kết quả xor có mã lỗi; không kết quả nào
// phí ≤ 0 hay quá 2^53−1.
func TestQuoteNow_Property(t *testing.T) {
	t.Parallel()
	errs := []error{
		nil, fulfillment.ErrRejected, &fulfillment.CarrierError{Class: fulfillment.ClassAuth},
		&fulfillment.CarrierError{Class: fulfillment.ClassRateLimited}, context.DeadlineExceeded, errors.New("x"),
	}
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 5).Draw(rt, "accounts")
		var adapters []fulfillment.CarrierAdapter
		var accs []fulfillment.Account
		for i := range n {
			c := &quoteCarrier{key: fmt.Sprintf("q%d", i), err: rapid.SampledFrom(errs).Draw(rt, "err")}
			for j := range rapid.IntRange(0, 4).Draw(rt, "services") {
				c.results = append(c.results, fulfillment.QuoteResult{
					Service: fmt.Sprintf("S%d", rapid.IntRange(0, j).Draw(rt, "svc")),
					Total:   rapid.Int64Range(-5, 1<<54).Draw(rt, "fee"),
				})
			}
			adapters = append(adapters, c)
			accs = append(accs, fulfillment.Account{ID: uuid.New(), CarrierKey: c.key})
		}
		s := fulfillment.New(fulfillment.Deps{Carriers: mustRegistry(t, adapters...)}).Service
		out := s.QuoteNow(context.Background(), accs, fulfillment.QuoteInput{}, time.Second)
		if msg := feeQuoteLaw(accs, out); msg != "" {
			rt.Fatal(msg)
		}
	})
}

// feeQuoteLaw trả mô tả lần vi phạm đầu tiên của quy luật QuoteNow ("" khi đúng).
func feeQuoteLaw(accs []fulfillment.Account, out []fulfillment.FeeQuote) string {
	if len(out) != len(accs) {
		return fmt.Sprintf("FeeQuote = %d, muốn %d", len(out), len(accs))
	}
	for i, q := range out {
		if q.AccountID != accs[i].ID {
			return fmt.Sprintf("FeeQuote %d sai tài khoản", i)
		}
		if (len(q.Results) > 0) == (q.ErrorCode != "") {
			return fmt.Sprintf("FeeQuote %d: %d kết quả và mã %q", i, len(q.Results), q.ErrorCode)
		}
		for _, r := range q.Results {
			if r.Total <= 0 || r.Total > 1<<53-1 {
				return fmt.Sprintf("FeeQuote %d có phí %d", i, r.Total)
			}
		}
	}
	return ""
}
