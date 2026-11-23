package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/cache"
)

type quoteBody struct {
	Lines []struct {
		VariantID        string          `json:"variantId"`
		Qty              int             `json:"qty"`
		UnitPrice        int64           `json:"unitPrice"`
		CampaignDiscount int64           `json:"campaignDiscount"`
		CouponDiscount   int64           `json:"couponDiscount"`
		Net              int64           `json:"net"`
		Campaign         json.RawMessage `json:"campaign"`
		Problem          string          `json:"problem"`
	} `json:"lines"`
	Merchandise int64  `json:"merchandise"`
	ShippingFee int64  `json:"shippingFee"`
	GrandTotal  int64  `json:"grandTotal"`
	Fingerprint string `json:"fingerprint"`
	Changed     bool   `json:"changed"`
	Coupon      *struct {
		Code    string `json:"code"`
		Applied bool   `json:"applied"`
		Reason  string `json:"reason"`
	} `json:"coupon"`
}

// post gửi POST path thân JSON body với khóa key (rỗng: không gửi).
func (e *env) post(key, path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequestWithContext(e.t.Context(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return e.send(req, key)
}

func quoteReq(fee int64, lines ...string) string {
	return fmt.Sprintf(`{"lines":[%s],"shippingFee":%d}`, strings.Join(lines, ","), fee)
}

func quoteLine(v uuid.UUID, qty int, price int64) string {
	return fmt.Sprintf(`{"variantId":%q,"qty":%d,"quotedUnitPrice":%d}`, v, qty, price)
}

func (e *env) quote(key, body string) quoteBody {
	e.t.Helper()
	rec := e.post(key, "/api/v1/web/quote", body)
	var out quoteBody
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		e.t.Fatalf("quote: %d %s", rec.Code, rec.Body.String())
	}
	return out
}

// setPrice đặt giá biến thể v (chủ bảng; không sự kiện, không xóa khóa bộ đệm: mô phỏng bộ đệm còn giá cũ).
func (e *env) setPrice(v uuid.UUID, price int64) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE catalog.variants SET price = $1 WHERE id = $2`, price, v)
		return err //nolint:wrapcheck // test
	})
}

// Hình dạng câu trả lời: 2 dòng (một ngừng bán), giá CRM, tổng, dấu vân tay 64 hex; 10 lần gọi không Idempotency-Key
// → 200, platform.idempotency_keys không thêm dòng; khóa không quyền web.orders.create → 403; không khóa → 401.
func TestWebQuote(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	key := e.serviceKey("web.orders.create")
	p := e.product("Q1", "[]")
	ok, gone := p.Variants[0].ID, p.Variants[2].ID
	e.stock(ok, 5)
	body := quoteReq(30000, quoteLine(ok, 2, 350000), quoteLine(gone, 1, 350000))
	before := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.idempotency_keys`)
	var first quoteBody
	for i := range 10 {
		got := e.quote(key, body)
		if i == 0 {
			first = got
		}
		if got.Fingerprint != first.Fingerprint {
			t.Fatalf("lần %d: dấu vân tay đổi", i)
		}
	}
	if after := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.idempotency_keys`); after != before {
		t.Fatalf("báo giá ghi khóa chống trùng: %d → %d", before, after)
	}
	q := first
	if len(q.Lines) != 2 || q.Lines[0].UnitPrice != 350000 || q.Lines[0].Net != 700000 || q.Lines[0].Problem != "" ||
		q.Lines[1].Problem != "unavailable" || q.Merchandise != 700000 || q.ShippingFee != 30000 || q.GrandTotal != 730000 ||
		len(q.Fingerprint) != 64 || q.Changed || q.Coupon != nil || q.Lines[0].Campaign != nil {
		t.Fatalf("báo giá: %+v", q)
	}
	if rec := e.post(e.key, "/api/v1/web/quote", body); rec.Code != http.StatusForbidden {
		t.Errorf("khóa web.catalog.read: mong 403, có %d", rec.Code)
	}
	if rec := e.post("", "/api/v1/web/quote", body); rec.Code != http.StatusUnauthorized {
		t.Errorf("không khóa: mong 401, có %d", rec.Code)
	}
	c := e.quote(key, `{"lines":[`+quoteLine(ok, 1, 350000)+`],"shippingFee":0,"couponCode":"KHONGCO","customerPhone":"0901234567"}`)
	if c.Coupon == nil || c.Coupon.Applied || c.Coupon.Reason != "not_found" || c.Coupon.Code != "KHONGCO" {
		t.Fatalf("mã lạ: %+v", c.Coupon)
	}
}

// Giới hạn: 51 dòng, số lượng 0/21, thiếu shippingFee, giá âm, trùng biến thể → 400; 20 × 9.000.000.000.000.000 → 422
// total_overflow.
func TestWebQuote_Limits(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	key := e.serviceKey("web.orders.create")
	p := e.product("Q2", "[]")
	v := p.Variants[0].ID
	many := make([]string, 51)
	for i := range many {
		many[i] = quoteLine(uuid.New(), 1, 1)
	}
	for name, body := range map[string]string{
		"51 dòng":        quoteReq(0, many...),
		"0 dòng":         quoteReq(0),
		"số lượng 0":     quoteReq(0, quoteLine(v, 0, 1)),
		"số lượng 21":    quoteReq(0, quoteLine(v, 21, 1)),
		"giá âm":         quoteReq(0, quoteLine(v, 1, -1)),
		"thiếu phí":      `{"lines":[` + quoteLine(v, 1, 1) + `]}`,
		"trùng biến thể": quoteReq(0, quoteLine(v, 1, 1), quoteLine(v, 1, 1)),
	} {
		if rec := e.post(key, "/api/v1/web/quote", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: mong 400, có %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := e.post(key, "/api/v1/web/quote", quoteReq(0, many[:50]...)); rec.Code != http.StatusOK {
		t.Errorf("50 dòng: mong 200, có %d %s", rec.Code, rec.Body.String())
	}
	e.setPrice(v, 9_000_000_000_000_000)
	e.stock(v, 50)
	rec := e.post(key, "/api/v1/web/quote", quoteReq(0, quoteLine(v, 20, 9_000_000_000_000_000)))
	if rec.Code != http.StatusUnprocessableEntity || !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"total_overflow"`)) {
		t.Fatalf("tràn: %d %s", rec.Code, rec.Body.String())
	}
}

// pinnedCache giữ mãi giá trị tải lần đầu của mỗi khóa (bộ đệm "còn giá cũ" chắc chắn, không phụ thuộc Valkey).
type pinnedCache struct {
	cache.ReadCache

	mu sync.Mutex
	m  map[string][]byte
}

func (c *pinnedCache) GetOrLoad(ctx context.Context, _ uuid.UUID, space cache.Space, id string, _ time.Duration,
	load func(context.Context) ([]byte, error),
) ([]byte, cache.Source, error) {
	k := fmt.Sprint(space) + "/" + id
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.m[k]; ok {
		return v, cache.SourceCache, nil
	}
	v, err := load(ctx)
	if err != nil {
		return nil, cache.SourceDB, err
	}
	c.m[k] = v
	return v, cache.SourceDB, nil
}

// Bộ đệm còn giá cũ của mẫu → danh mục web vẫn trả giá cũ, nhưng báo giá dùng giá mới trong PostgreSQL và không đọc
// bộ đệm lần nào.
func TestWebQuote_IgnoresReadCache(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	e.rc.ReadCache = &pinnedCache{ReadCache: e.rc.ReadCache, m: map[string][]byte{}}
	key := e.serviceKey("web.orders.create")
	p := e.product("Q3", "[]")
	v := p.Variants[0].ID
	e.stock(v, 5)
	path := "/api/v1/web/catalog/products?ids=" + p.ID.String()
	if rec := e.get(e.key, path); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "350000") {
		t.Fatalf("danh mục: %d %s", rec.Code, rec.Body.String())
	}
	e.setPrice(v, 420000)
	if rec := e.get(e.key, path); !strings.Contains(rec.Body.String(), "350000") || strings.Contains(rec.Body.String(), "420000") {
		t.Fatalf("bộ đệm không giữ giá cũ: %s", rec.Body.String())
	}
	e.rc.take()
	q := e.quote(key, quoteReq(0, quoteLine(v, 1, 350000)))
	if n, src := e.rc.take(); n != 0 || len(src) != 0 {
		t.Fatalf("báo giá đọc bộ đệm: %d lần tải, nguồn %v", n, src)
	}
	if q.Lines[0].UnitPrice != 420000 || q.Lines[0].Problem != "price_changed" || !q.Changed || q.GrandTotal != 420000 {
		t.Fatalf("báo giá dùng giá cũ: %+v", q)
	}
}
