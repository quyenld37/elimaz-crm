package db_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/db"
)

// probe dựng schema thử `probe` (bảng lock_pair hai dòng 1, 2 và bảng items) bằng chủ bảng, cấp quyền cho crm_app.
func probe(t *testing.T, ownerURL string) {
	t.Helper()
	conn := ownerConn(t, ownerURL)
	for _, q := range []string{
		"CREATE SCHEMA probe",
		"CREATE TABLE probe.lock_pair (id int PRIMARY KEY, v int NOT NULL)",
		"INSERT INTO probe.lock_pair VALUES (1, 0), (2, 0)",
		"CREATE TABLE probe.items (id int PRIMARY KEY)",
		"GRANT USAGE ON SCHEMA probe TO crm_app",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA probe TO crm_app",
	} {
		if _, err := conn.Exec(t.Context(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func countItems(t *testing.T, d *db.DB) int64 {
	t.Helper()
	var n int64
	if err := d.Pool.QueryRow(t.Context(), "SELECT count(*) FROM probe.items").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func insertItem(ctx context.Context, tx pgx.Tx, id int) error {
	if _, err := tx.Exec(ctx, "INSERT INTO probe.items (id) VALUES ($1)", id); err != nil {
		return fmt.Errorf("chèn probe.items: %w", err)
	}
	return nil
}

// insertThen: hàm cho RetryTx đếm số lần gọi, chèn dòng id=1 vào probe.items rồi kết thúc bằng end (lỗi hay panic).
func insertThen(ctx context.Context, calls *int, end func() error) func(pgx.Tx) error {
	return func(tx pgx.Tx) error {
		*calls++
		if err := insertItem(ctx, tx, 1); err != nil {
			return err
		}
		return end()
	}
}

// hooks đếm OnRetry và OnExhausted (gọi từ nhiều goroutine).
type hooks struct {
	mu        sync.Mutex
	retries   []string
	exhausted []string
}

func (h *hooks) attach(d *db.DB) {
	d.OnRetry = func(code string, _ int) { h.mu.Lock(); h.retries = append(h.retries, code); h.mu.Unlock() }
	d.OnExhausted = func(code string) { h.mu.Lock(); h.exhausted = append(h.exhausted, code); h.mu.Unlock() }
}

func (h *hooks) counts() (retries, exhausted []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.retries...), append([]string(nil), h.exhausted...)
}

func TestRetryTxPlainErrorRollsBackOnce(t *testing.T) {
	d, owner := openApp(t, db.PoolOptions{})
	probe(t, owner)
	errBoom := errors.New("lỗi của hàm")
	calls := 0
	err := d.RetryTx(t.Context(), db.TxOptions{}, insertThen(t.Context(), &calls, func() error { return errBoom }))
	if !errors.Is(err, errBoom) || calls != 1 {
		t.Fatalf("err=%v calls=%d, muốn lỗi của hàm và 1 lần gọi", err, calls)
	}
	if n := countItems(t, d); n != 0 {
		t.Fatalf("dòng đã chèn phải lùi, còn %d", n)
	}
}

func TestRetryTxPanicRollsBackAndRepanics(t *testing.T) {
	d, owner := openApp(t, db.PoolOptions{})
	probe(t, owner)
	calls := 0
	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Fatalf("panic phải được ném lại nguyên, nhận %v", r)
			}
		}()
		_ = d.RetryTx(t.Context(), db.TxOptions{}, insertThen(t.Context(), &calls, func() error { panic("boom") }))
		t.Fatal("RetryTx phải panic")
	}()
	if calls != 1 {
		t.Fatalf("hàm panic được gọi %d lần, muốn 1", calls)
	}
	if n := countItems(t, d); n != 0 {
		t.Fatalf("dòng đã chèn phải lùi sau panic, còn %d", n)
	}
}

// (a) Deadlock thật: A khóa 1 rồi 2, B khóa 2 rồi 1; rào chắn chỉ ở lần gọi đầu của mỗi bên.
func TestRetryTxRealDeadlockBothSucceed(t *testing.T) {
	d, owner := openApp(t, db.PoolOptions{})
	probe(t, owner)
	var h hooks
	h.attach(d)

	var calls atomic.Int32
	aLocked, bLocked := make(chan struct{}), make(chan struct{})
	side := func(first, second int, mine, theirs chan struct{}) func(pgx.Tx) error {
		var n atomic.Int32
		return func(tx pgx.Tx) error {
			calls.Add(1)
			attempt := n.Add(1)
			if _, err := tx.Exec(t.Context(), "UPDATE probe.lock_pair SET v = v + 1 WHERE id = $1", first); err != nil {
				return fmt.Errorf("khóa dòng %d: %w", first, err)
			}
			if attempt == 1 {
				close(mine)
				<-theirs
			}
			if _, err := tx.Exec(t.Context(), "UPDATE probe.lock_pair SET v = v + 1 WHERE id = $1", second); err != nil {
				return fmt.Errorf("khóa dòng %d: %w", second, err)
			}
			return nil
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Go(func() { errs[0] = d.RetryTx(t.Context(), db.TxOptions{}, side(1, 2, aLocked, bLocked)) })
	wg.Go(func() { errs[1] = d.RetryTx(t.Context(), db.TxOptions{}, side(2, 1, bLocked, aLocked)) })
	wg.Wait()

	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("cả hai phải xong: A=%v B=%v", errs[0], errs[1])
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("tổng số lần gọi hàm = %d, muốn 3", got)
	}
	retries, _ := h.counts()
	if len(retries) != 1 || retries[0] != "40P01" {
		t.Fatalf("OnRetry = %v, muốn đúng một lần 40P01", retries)
	}
	rows, err := d.Pool.Query(t.Context(), "SELECT v FROM probe.lock_pair ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	vs, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || vs[0] != 2 || vs[1] != 2 {
		t.Fatalf("v = %v, muốn [2 2] (đủ hai lần cộng)", vs)
	}
}

// (b) Lỗi tạm thời kéo dài: 4 lần gọi rồi ErrRetryLater, giữ được SQLSTATE gốc.
func TestRetryTxExhaustedReturnsRetryLater(t *testing.T) {
	d, _ := openApp(t, db.PoolOptions{})
	var h hooks
	h.attach(d)
	calls := 0
	err := d.RetryTx(t.Context(), db.TxOptions{}, func(pgx.Tx) error {
		calls++
		return &pgconn.PgError{Code: "40001"}
	})
	if !errors.Is(err, db.ErrRetryLater) {
		t.Fatalf("muốn ErrRetryLater, nhận %v", err)
	}
	if code, ok := db.TransientCode(err); !ok || code != "40001" {
		t.Fatalf("TransientCode = %q, %v; muốn 40001", code, ok)
	}
	if calls != 4 {
		t.Fatalf("hàm được gọi %d lần, muốn 4", calls)
	}
	retries, exhausted := h.counts()
	if len(retries) != 3 || len(exhausted) != 1 || exhausted[0] != "40001" {
		t.Fatalf("OnRetry=%v OnExhausted=%v; muốn 3 lần và 1 lần 40001", retries, exhausted)
	}
}

// lockRow giữ khóa FOR UPDATE trên dòng 1 của probe.lock_pair từ một kết nối owner riêng tới khi gọi hàm trả về
// (gọi nhiều lần được; Cleanup tự nhả nếu test chưa nhả). Nhả là đồng bộ: hàm trả về khi khóa đã mất.
func lockRow(t *testing.T, ownerURL string) (release func()) {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), "SELECT 1 FROM probe.lock_pair WHERE id = 1 FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	release = sync.OnceFunc(func() {
		_ = tx.Rollback(context.WithoutCancel(t.Context()))
		_ = conn.Close(context.WithoutCancel(t.Context()))
	})
	t.Cleanup(release)
	return release
}

func updateRow1(ctx context.Context, calls *int) func(pgx.Tx) error {
	return func(tx pgx.Tx) error {
		*calls++
		if _, err := tx.Exec(ctx, "UPDATE probe.lock_pair SET v = v + 1 WHERE id = 1"); err != nil {
			return fmt.Errorf("sửa dòng 1: %w", err)
		}
		return nil
	}
}

func TestRetryTxLockTimeoutRetriesThenGivesUp(t *testing.T) {
	d, owner := openApp(t, db.PoolOptions{})
	probe(t, owner)
	lockRow(t, owner) // giữ tới hết test
	calls := 0
	err := d.RetryTx(t.Context(), db.TxOptions{LockTimeout: 50 * time.Millisecond}, updateRow1(t.Context(), &calls))
	if !errors.Is(err, db.ErrRetryLater) {
		t.Fatalf("muốn ErrRetryLater, nhận %v", err)
	}
	if code, _ := db.TransientCode(err); code != "55P03" || calls != 4 {
		t.Fatalf("SQLSTATE=%q calls=%d; muốn 55P03 và 4", code, calls)
	}
}

// Khóa nhả ngay khi RetryTx báo lần thử lại đầu tiên (không theo đồng hồ, để máy tải cao không đổi thứ tự): lần chạy
// thứ hai phải lấy được khóa và xong.
func TestRetryTxLockTimeoutRecoversWhenLockReleased(t *testing.T) {
	d, owner := openApp(t, db.PoolOptions{})
	probe(t, owner)
	release := lockRow(t, owner)
	var retries []string
	d.OnRetry = func(code string, attempt int) {
		retries = append(retries, code)
		if attempt == 1 {
			release()
		}
	}
	calls := 0
	if err := d.RetryTx(t.Context(), db.TxOptions{LockTimeout: 50 * time.Millisecond}, updateRow1(t.Context(), &calls)); err != nil {
		t.Fatalf("khóa được nhả sau lần chờ khóa đầu, RetryTx phải xong: %v", err)
	}
	if calls != 2 || !slices.Equal(retries, []string{"55P03"}) {
		t.Fatalf("hàm được gọi %d lần, thử lại %v; muốn 2 lần, một lần thử lại 55P03", calls, retries)
	}
}

// (c) Lỗi không tạm thời không bao giờ được thử lại.
func TestRetryTxNonTransientNotRetried(t *testing.T) {
	d, owner := openApp(t, db.PoolOptions{})
	probe(t, owner)
	var h hooks
	h.attach(d)
	if _, err := d.Pool.Exec(t.Context(), "INSERT INTO probe.items VALUES (1)"); err != nil {
		t.Fatal(err)
	}

	calls := 0
	err := d.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error { calls++; return insertItem(t.Context(), tx, 1) })
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || errors.Is(err, db.ErrRetryLater) || calls != 1 {
		t.Fatalf("trùng khóa: err=%v calls=%d; muốn 23505, không ErrRetryLater, 1 lần", err, calls)
	}

	calls = 0
	err = d.RetryTx(t.Context(), db.TxOptions{StatementTimeout: 50 * time.Millisecond}, func(tx pgx.Tx) error {
		calls++
		if _, err := tx.Exec(t.Context(), "SELECT pg_sleep(1)"); err != nil {
			return fmt.Errorf("pg_sleep: %w", err)
		}
		return nil
	})
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" || calls != 1 {
		t.Fatalf("quá statement_timeout: err=%v calls=%d; muốn 57014 và 1 lần", err, calls)
	}
	if retries, _ := h.counts(); len(retries) != 0 {
		t.Fatalf("OnRetry không được gọi, nhận %v", retries)
	}
}

// (d) Số lần gọi khớp số lỗi tạm thời cộng một; các lần trước không để lại dữ liệu.
func TestRetryTxCallsEqualTransientErrorsPlusOne(t *testing.T) {
	d, owner := openApp(t, db.PoolOptions{})
	probe(t, owner)
	calls := 0
	err := d.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		calls++
		if err := insertItem(t.Context(), tx, calls); err != nil {
			return err
		}
		switch calls {
		case 1:
			return &pgconn.PgError{Code: "40P01"}
		case 2:
			return &pgconn.PgError{Code: "55P03"}
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d; muốn nil và 3", err, calls)
	}
	var ids []int
	rows, err := d.Pool.Query(t.Context(), "SELECT id FROM probe.items ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if ids, err = pgx.CollectRows(rows, pgx.RowTo[int]); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 3 {
		t.Fatalf("probe.items = %v, muốn chỉ [3]", ids)
	}
}

func TestBackoff(t *testing.T) {
	half := func() float64 { return 0.5 }
	cases := []struct {
		n    int
		rnd  func() float64
		want time.Duration
	}{
		{1, half, 20 * time.Millisecond},
		{2, half, 40 * time.Millisecond},
		{3, half, 80 * time.Millisecond},
		{1, func() float64 { return 0 }, 10 * time.Millisecond},
		{1, func() float64 { return 1 }, 30 * time.Millisecond},
		{3, func() float64 { return 1 }, 120 * time.Millisecond},
	}
	for _, c := range cases {
		if got := db.Backoff(c.n, c.rnd); got != c.want {
			t.Errorf("Backoff(%d, %v) = %v, muốn %v", c.n, c.rnd(), got, c.want)
		}
	}
}

func TestTransientCode(t *testing.T) {
	for code, want := range map[string]bool{"40P01": true, "55P03": true, "40001": true, "23505": false, "57014": false} {
		got, ok := db.TransientCode(errors.Join(errors.New("bọc"), &pgconn.PgError{Code: code}))
		if ok != want || got != code {
			t.Errorf("TransientCode(%s) = %q, %v; muốn %v", code, got, ok, want)
		}
	}
	if code, ok := db.TransientCode(errors.New("không phải lỗi PostgreSQL")); ok || code != "" {
		t.Errorf("lỗi thường: %q, %v", code, ok)
	}
}

func TestRetryTxStopsWhenContextCanceledDuringWait(t *testing.T) {
	d, _ := openApp(t, db.PoolOptions{})
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	canceled := make(chan time.Time, 1)
	d.OnRetry = func(string, int) {
		time.AfterFunc(5*time.Millisecond, func() { canceled <- time.Now(); cancel(errors.New("lý do riêng của bên gọi")) })
	}
	calls := 0
	err := d.RetryTx(ctx, db.TxOptions{}, func(pgx.Tx) error { calls++; return &pgconn.PgError{Code: "40P01"} })
	returned := time.Now()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("muốn context.Canceled, nhận %v", err)
	}
	if calls != 1 {
		t.Fatalf("hàm không được gọi thêm sau khi ctx hủy; gọi %d lần", calls)
	}
	if canceledAt := <-canceled; returned.Sub(canceledAt) > 50*time.Millisecond {
		t.Fatalf("phải trả trong 50 ms sau khi hủy (hủy lúc %v, trả lúc %v)", canceledAt, returned)
	}
}

// Giá trị đặt theo transaction không rò sang kết nối (một kết nối duy nhất trong pool).
func TestRetryTxLimitsDoNotLeakToConnection(t *testing.T) {
	d, _ := openApp(t, db.PoolOptions{MaxConns: 1})
	err := d.RetryTx(t.Context(), db.TxOptions{LockTimeout: 200 * time.Millisecond}, func(tx pgx.Tx) error {
		if got := showIn(t, tx, "lock_timeout"); got != "200ms" {
			t.Errorf("trong transaction lock_timeout = %q, muốn 200ms", got)
		}
		if got := showIn(t, tx, "statement_timeout"); got != "30s" {
			t.Errorf("trong transaction statement_timeout = %q, muốn giữ 30s của kết nối", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := showPool(t, d, "lock_timeout"); got != "5s" {
		t.Fatalf("sau transaction lock_timeout = %q, muốn 5s", got)
	}
}

func TestRetryTxUsesPoolDefaultsSettingsAndReadCommitted(t *testing.T) {
	d, _ := openApp(t, db.PoolOptions{MaxConns: 1, DefaultTx: db.TxAPI})
	o := db.TxOptions{Settings: []db.Setting{{Name: "app.tenant_id", Value: "t-1"}}}
	err := d.RetryTx(t.Context(), o, func(tx pgx.Tx) error {
		for name, want := range map[string]string{
			"lock_timeout": "1s", "statement_timeout": "5s", "transaction_isolation": "read committed",
		} {
			if got := showIn(t, tx, name); got != want {
				t.Errorf("%s = %q, muốn %q", name, got, want)
			}
		}
		var tenant string
		if err := tx.QueryRow(t.Context(), "SELECT current_setting('app.tenant_id')").Scan(&tenant); err != nil {
			return fmt.Errorf("đọc app.tenant_id: %w", err)
		}
		if tenant != "t-1" {
			t.Errorf("app.tenant_id = %q", tenant)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var after string
	if err := d.Pool.QueryRow(t.Context(), "SELECT coalesce(current_setting('app.tenant_id', true), '')").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != "" {
		t.Fatalf("app.tenant_id rò sang kết nối: %q", after)
	}

	err = d.RetryTx(t.Context(), db.TxOptions{IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		if got := showIn(t, tx, "transaction_isolation"); got != "repeatable read" {
			t.Errorf("IsoLevel RepeatableRead → %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
