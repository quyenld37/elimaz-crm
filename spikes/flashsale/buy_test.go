package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Test chạy trên PostgreSQL dev (docker/compose.dev.yml). CRM_TEST_DATABASE_URL là chuỗi kết nối
// của crm_app; CRM_TEST_OWNER_DATABASE_URL (không bắt buộc) của crm_owner.
const defaultTestAppURL = "postgres://crm_app:crm_app_dev@127.0.0.1:5442/crm?sslmode=disable"

var (
	testApp   *pgxpool.Pool
	testOwner *pgxpool.Pool
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()
	appURL := os.Getenv("CRM_TEST_DATABASE_URL")
	if appURL == "" {
		appURL = defaultTestAppURL
	}
	ownerURL := os.Getenv("CRM_TEST_OWNER_DATABASE_URL")
	if ownerURL == "" {
		var err error
		if ownerURL, err = ownerURLFrom(appURL); err != nil {
			fmt.Fprintln(os.Stderr, "chuỗi kết nối test sai:", err)
			return 1
		}
	}
	var err error
	if testOwner, err = openPool(ctx, ownerURL, 4); err != nil {
		fmt.Fprintln(os.Stderr, "không nối được PostgreSQL dev (docker compose -f docker/compose.dev.yml up -d):", err)
		return 1
	}
	defer testOwner.Close()
	if err := applySchema(ctx, testOwner); err != nil {
		fmt.Fprintln(os.Stderr, "dựng schema spike:", err)
		return 1
	}
	if testApp, err = openPool(ctx, appURL, 40); err != nil {
		fmt.Fprintln(os.Stderr, "nối bằng crm_app:", err)
		return 1
	}
	defer testApp.Close()
	return m.Run()
}

func newUUID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// fixture: một tenant mới, một món mới có units suất, một máy chủ đo với giới hạn limit.
type fixture struct {
	tenant, item string
	limit        int
	bs           *buyServer
	srv          *httptest.Server
}

func newFixture(t *testing.T, units, limit int) *fixture {
	t.Helper()
	f := &fixture{tenant: newUUID(t), item: newUUID(t), limit: limit}
	if err := seed(context.Background(), testOwner, f.tenant, f.item, units); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f.bs = newBuyServer(testApp, limit, 64)
	f.srv = httptest.NewServer(f.bs)
	t.Cleanup(f.srv.Close)
	return f
}

type buyResp struct {
	Status int
	Body   []byte
	Result string `json:"result"`
}

func (f *fixture) buy(t *testing.T, key, customer string, qty int) buyResp {
	t.Helper()
	return f.buyRaw(t, key, fmt.Sprintf(`{"item_id":%q,"customer_id":%q,"qty":%d}`, f.item, customer, qty))
}

func (f *fixture) buyRaw(t *testing.T, key, body string) buyResp {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.srv.URL+"/buy", bytes.NewBufferString(body))
	if err != nil {
		t.Error(err)
		return buyResp{}
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req.Header.Set("X-Tenant", f.tenant)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Error(err)
		return buyResp{}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	r := buyResp{Status: res.StatusCode, Body: raw}
	_ = json.Unmarshal(raw, &r)
	return r
}

func (f *fixture) mustVerify(t *testing.T) {
	t.Helper()
	rep, err := verify(context.Background(), testApp, f.tenant, f.item, f.limit)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("verify sai: %v", rep.Problems)
	}
}

// count đếm số dòng của một bảng spike trong ngữ cảnh tenant của fixture.
func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	err := pgx.BeginFunc(context.Background(), testApp, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), "SELECT set_config('app.tenant_id', $1, true)", f.tenant); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), query, args...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("đếm %q: %v", query, err)
	}
	return n
}

func TestTenUnitsFiftyBuyersGetExactlyTen(t *testing.T) {
	f := newFixture(t, 10, 1)
	results := make([]buyResp, 50)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			// Thử lại với cùng khóa khi nhận "retry", như website.
			key, customer := newUUID(t), newUUID(t)
			for range 50 {
				results[i] = f.buy(t, key, customer, 1)
				if results[i].Result != "retry" {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
	wg.Wait()
	got := map[string]int{}
	for _, r := range results {
		got[r.Result]++
	}
	if got["bought"] != 10 || got["sold_out"] != 40 {
		t.Fatalf("muốn 10 bought, 40 sold_out; được %v", got)
	}
	if n := f.count(t, "SELECT count(*) FROM spike.orders"); n != 10 {
		t.Fatalf("số đơn = %d, muốn 10", n)
	}
	f.mustVerify(t)
}

func TestSameKeyTwiceReturnsFirstBody(t *testing.T) {
	f := newFixture(t, 5, 1)
	key, customer := newUUID(t), newUUID(t)
	first := f.buy(t, key, customer, 1)
	if first.Status != http.StatusOK || first.Result != "bought" {
		t.Fatalf("lần một: %d %s", first.Status, first.Body)
	}
	second := f.buy(t, key, customer, 1)
	if second.Status != first.Status || !bytes.Equal(second.Body, first.Body) {
		t.Fatalf("lần hai khác lần một:\n%d %s\n%d %s", first.Status, first.Body, second.Status, second.Body)
	}
	if n := f.count(t, "SELECT count(*) FROM spike.orders"); n != 1 {
		t.Fatalf("số đơn = %d, muốn 1", n)
	}
	f.mustVerify(t)
}

func TestSameKeyDifferentBodyIs422(t *testing.T) {
	f := newFixture(t, 5, 2)
	key, customer := newUUID(t), newUUID(t)
	if r := f.buy(t, key, customer, 1); r.Result != "bought" {
		t.Fatalf("lần một: %d %s", r.Status, r.Body)
	}
	if r := f.buy(t, key, customer, 2); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("cùng khóa khác nội dung: muốn 422, được %d %s", r.Status, r.Body)
	}
	if n := f.count(t, "SELECT count(*) FROM spike.orders"); n != 1 {
		t.Fatalf("số đơn = %d, muốn 1", n)
	}
	f.mustVerify(t)
}

func TestMissingOrBadKeyIs400(t *testing.T) {
	f := newFixture(t, 1, 1)
	customer := newUUID(t)
	for _, key := range []string{"", "có dấu", string(bytes.Repeat([]byte("a"), 129))} {
		if r := f.buy(t, key, customer, 1); r.Status != http.StatusBadRequest {
			t.Errorf("khóa %q: muốn 400, được %d %s", key, r.Status, r.Body)
		}
	}
	if n := f.count(t, "SELECT count(*) FROM spike.idempotency_keys"); n != 0 {
		t.Fatalf("có %d dòng khóa, muốn 0", n)
	}
}

func TestOneCustomerLimitTwoOnlyTwoBought(t *testing.T) {
	f := newFixture(t, 50, 2)
	customer := newUUID(t)
	results := make([]buyResp, 20)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			key := newUUID(t)
			for range 50 {
				results[i] = f.buy(t, key, customer, 1)
				if results[i].Result != "retry" {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
	wg.Wait()
	got := map[string]int{}
	for _, r := range results {
		got[r.Result]++
	}
	if got["bought"] != 2 || got["over_limit"] != 18 {
		t.Fatalf("muốn 2 bought, 18 over_limit; được %v", got)
	}
	f.mustVerify(t)
}

func TestLockedUnitGivesRetryNotSoldOut(t *testing.T) {
	f := newFixture(t, 2, 2)
	ctx := context.Background()

	// Một transaction khác khóa 1 trong 2 đơn vị cuối và giữ 200 ms.
	locked, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- pgx.BeginFunc(ctx, testApp, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", f.tenant); err != nil {
				return err
			}
			var id string
			if err := tx.QueryRow(ctx, `SELECT id::text FROM spike.flash_sale_units
				WHERE tenant_id = $1 AND item_id = $2 ORDER BY id DESC LIMIT 1 FOR UPDATE`, f.tenant, f.item).Scan(&id); err != nil {
				return err
			}
			close(locked)
			// Giữ ít nhất 200 ms và tới khi lượt mua đã có câu trả lời (không phụ thuộc tốc độ máy).
			time.Sleep(200 * time.Millisecond)
			<-release
			return nil
		})
	}()
	<-locked

	key, customer := newUUID(t), newUUID(t)
	r := f.buy(t, key, customer, 2)
	close(release)
	if r.Status != http.StatusServiceUnavailable || r.Result != "retry" {
		t.Fatalf("muốn 503 retry, được %d %s", r.Status, r.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(r.Body, &body); err != nil || body["after_ms"] != float64(500) {
		t.Fatalf("body retry sai: %s", r.Body)
	}
	if n := f.count(t, "SELECT count(*) FROM spike.idempotency_keys WHERE key = $1", key); n != 0 {
		t.Fatalf("còn %d dòng khóa cho lượt retry, muốn 0", n)
	}
	if n := f.count(t, "SELECT coalesce(sum(quantity), 0) FROM spike.flash_sale_claims WHERE item_id = $1", f.item); n != 0 {
		t.Fatalf("lượt retry vẫn tính vào giới hạn: %d", n)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// Khóa được nhả: thử lại với cùng khóa thì mua được.
	if r := f.buy(t, key, customer, 2); r.Result != "bought" {
		t.Fatalf("thử lại sau khi nhả khóa: %d %s", r.Status, r.Body)
	}
	f.mustVerify(t)
}

func TestNoTenantContextSeesNoRows(t *testing.T) {
	f := newFixture(t, 1, 1)
	if r := f.buy(t, newUUID(t), newUUID(t), 1); r.Result != "bought" {
		t.Fatalf("mua: %d %s", r.Status, r.Body)
	}
	ctx := context.Background()

	// Kết nối mới, chưa từng đặt tenant.
	var n int
	if err := testApp.QueryRow(ctx, "SELECT count(*) FROM spike.orders").Scan(&n); err != nil || n != 0 {
		t.Fatalf("ngoài ngữ cảnh tenant: count=%d err=%v, muốn 0 và không lỗi", n, err)
	}

	// Kết nối vừa dùng cho một transaction có tenant (Review Focus 3).
	conn, err := testApp.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", f.tenant); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM spike.orders").Scan(&n)
	})
	if err != nil || n != 1 {
		t.Fatalf("trong ngữ cảnh tenant: count=%d err=%v, muốn 1", n, err)
	}
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM spike.orders").Scan(&n); err != nil || n != 0 {
		t.Fatalf("cùng kết nối, sau transaction có tenant: count=%d err=%v, muốn 0 và không lỗi", n, err)
	}
}

// verify phải báo sai khi dữ liệu sai: nếu không, mọi bài đo "verify đạt" đều vô nghĩa.
func TestVerifyCatchesBrokenData(t *testing.T) {
	f := newFixture(t, 3, 1)
	customer := newUUID(t)
	if r := f.buy(t, newUUID(t), customer, 1); r.Result != "bought" {
		t.Fatalf("mua: %d %s", r.Status, r.Body)
	}
	f.mustVerify(t)
	ctx := context.Background()
	err := inTenantTx(ctx, testApp, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE spike.flash_sale_claims SET quantity = quantity + 1
			WHERE tenant_id = $1 AND item_id = $2 AND customer_id = $3`, f.tenant, f.item, customer)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := verify(ctx, testApp, f.tenant, f.item, f.limit)
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(rep, "(c)") || !hasProblem(rep, "(d)") {
		t.Fatalf("verify phải báo (c) và (d), được: %v", rep.Problems)
	}
}

// Một đơn vị bị gắn sang dòng đơn khác → verify báo (b).
func TestVerifyCatchesUnitOnWrongLine(t *testing.T) {
	f := newFixture(t, 3, 1)
	for range 2 {
		if r := f.buy(t, newUUID(t), newUUID(t), 1); r.Result != "bought" {
			t.Fatalf("mua: %d %s", r.Status, r.Body)
		}
	}
	ctx := context.Background()
	err := inTenantTx(ctx, testApp, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE spike.flash_sale_units SET order_item_id =
			(SELECT min(order_item_id::text)::uuid FROM spike.flash_sale_units WHERE tenant_id = $1 AND item_id = $2)
			WHERE tenant_id = $1 AND item_id = $2 AND order_item_id IS NOT NULL`, f.tenant, f.item)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := verify(ctx, testApp, f.tenant, f.item, f.limit)
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(rep, "(b)") {
		t.Fatalf("verify phải báo (b), được: %v", rep.Problems)
	}
}

func hasProblem(rep report, prefix string) bool {
	for _, p := range rep.Problems {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// holdClaimRow khóa dòng flash_sale_claims của khách (tạo với quantity 0) tới khi release đóng.
func (f *fixture) holdClaimRow(t *testing.T, customer string, release <-chan struct{}) <-chan error {
	t.Helper()
	ctx := context.Background()
	err := inTenantTx(ctx, testApp, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO spike.flash_sale_claims (tenant_id, item_id, customer_id, quantity)
			VALUES ($1, $2, $3, 0)`, f.tenant, f.item, customer)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	locked, done := make(chan struct{}), make(chan error, 1)
	go func() {
		done <- inTenantTx(ctx, testApp, f.tenant, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT 1 FROM spike.flash_sale_claims
				WHERE tenant_id = $1 AND item_id = $2 AND customer_id = $3 FOR UPDATE`, f.tenant, f.item, customer); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	return done
}

// ADR-46: chờ khóa quá lock_timeout (55P03) thì chạy lại cả transaction; khóa nhả kịp thì mua được.
func TestLockTimeoutIsRetriedThenBought(t *testing.T) {
	f := newFixture(t, 2, 1)
	customer := newUUID(t)
	release := make(chan struct{})
	done := f.holdClaimRow(t, customer, release)
	go func() {
		time.Sleep(300 * time.Millisecond) // lâu hơn lock_timeout 200 ms, ngắn hơn tổng các lần chạy lại
		close(release)
	}()
	if r := f.buy(t, newUUID(t), customer, 1); r.Result != "bought" {
		t.Fatalf("muốn bought sau khi chạy lại, được %d %s", r.Status, r.Body)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got := f.bs.retries.snapshot()
	if got["55P03"] < 1 || got["exhausted"] != 0 {
		t.Fatalf("bộ đếm chạy lại: %v, muốn 55P03 >= 1 và không hết lượt", got)
	}
	f.mustVerify(t)
}

// ADR-46: chạy lại 3 lần vẫn 55P03 → 503 retry, không lưu, xóa dòng khóa; gửi lại cùng khóa sau đó → bought.
func TestLockTimeoutExhaustedGivesRetryNotStored(t *testing.T) {
	f := newFixture(t, 2, 1)
	customer := newUUID(t)
	release := make(chan struct{})
	done := f.holdClaimRow(t, customer, release)
	key := newUUID(t)
	r := f.buy(t, key, customer, 1)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if r.Status != http.StatusServiceUnavailable || r.Result != "retry" {
		t.Fatalf("muốn 503 retry, được %d %s", r.Status, r.Body)
	}
	if got := f.bs.retries.snapshot(); got["55P03"] != 3 || got["exhausted"] != 1 {
		t.Fatalf("bộ đếm chạy lại: %v, muốn 55P03=3, exhausted=1", got)
	}
	if n := f.count(t, "SELECT count(*) FROM spike.idempotency_keys WHERE key = $1", key); n != 0 {
		t.Fatalf("còn %d dòng khóa cho lượt hết lượt chạy lại, muốn 0", n)
	}
	if r := f.buy(t, key, customer, 1); r.Result != "bought" {
		t.Fatalf("gửi lại cùng khóa: %d %s", r.Status, r.Body)
	}
	f.mustVerify(t)
}
