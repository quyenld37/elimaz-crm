package db

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// rollbackTimeout giới hạn lần lùi transaction chạy trên ctx đã tách khỏi việc hủy của bên gọi.
const rollbackTimeout = 5 * time.Second

// Setting là một tham số đặt theo transaction bằng set_config(Name, Value, true) (tenancy thêm app.tenant_id).
type Setting struct {
	Name, Value string
}

// TxOptions của một transaction. LockTimeout, StatementTimeout bằng 0 lấy DefaultTx của pool; DefaultTx cũng bằng 0
// thì giữ mặc định của kết nối (ADR-26). IsoLevel rỗng là READ COMMITTED (ADR-46 điểm 5).
type TxOptions struct {
	LockTimeout      time.Duration
	StatementTimeout time.Duration
	IsoLevel         pgx.TxIsoLevel
	Settings         []Setting
}

// Bộ giới hạn theo đường (ADR-46 điểm 6, giá trị gợi ý; Task 1 đo, đổi theo số đo). Chỉ đọc: muốn giá trị khác thì
// sao chép rồi sửa bản sao.
//
//nolint:gochecknoglobals // Interfaces của kế hoạch M0 Task 4 khai bốn bộ này là var; không mã nào gán lại.
var (
	TxFlashBuy = TxOptions{LockTimeout: 200 * time.Millisecond, StatementTimeout: 2 * time.Second}
	TxAPI      = TxOptions{LockTimeout: time.Second, StatementTimeout: 5 * time.Second}
	TxIngest   = TxOptions{LockTimeout: 500 * time.Millisecond, StatementTimeout: 2 * time.Second}
	TxWorker   = TxOptions{LockTimeout: 5 * time.Second, StatementTimeout: 30 * time.Second}
)

// RetryTx chạy fn trong một transaction rồi commit (ADR-46). Câu đầu tiên của transaction đặt lock_timeout,
// statement_timeout và o.Settings bằng set_config(…, true) trong một lượt đi về, nên không rò sang kết nối.
//
// Lỗi mang SQLSTATE 40P01, 55P03, 40001 ở bất kỳ câu nào, kể cả commit: lùi, chờ Backoff, chạy lại toàn bộ fn trên
// transaction mới; tối đa 3 lần chạy lại, hết lượt trả lỗi bọc ErrRetryLater và lỗi gốc. Lỗi khác (23xxx, 57014,
// lỗi của fn, lỗi commit không có SQLSTATE vì không biết đã commit hay chưa): lùi, trả nguyên, không thử lại.
// fn panic: lùi rồi ném lại. ctx hủy, kể cả lúc chờ: dừng ngay, trả lỗi của ctx.
//
// fn có thể chạy tới 4 lần nên phải chạy lại được an toàn: không gọi mạng, không gửi gì ra ngoài database, không
// cộng dồn vào biến bên ngoài (gán lại kết quả ở mỗi lần chạy). NOTIFY và ghi việc vào bảng jobs thì được.
func (d *DB) RetryTx(ctx context.Context, o TxOptions, fn func(pgx.Tx) error) error {
	o = d.withDefaults(o)
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("mở transaction: %w", err)
		}
		err := d.runOnce(ctx, o, fn)
		if err == nil {
			return nil
		}
		code, transient := TransientCode(err)
		if !transient {
			return err
		}
		if attempt > maxRetries {
			if d.OnExhausted != nil {
				d.OnExhausted(code)
			}
			return fmt.Errorf("%w: %w", ErrRetryLater, err)
		}
		if d.OnRetry != nil {
			d.OnRetry(code, attempt)
		}
		if err := sleep(ctx, Backoff(attempt, rand.Float64)); err != nil {
			return err
		}
	}
}

// withDefaults điền trường bằng 0 của o từ DefaultTx của pool; Settings của pool đứng trước Settings của o.
func (d *DB) withDefaults(o TxOptions) TxOptions {
	if o.LockTimeout == 0 {
		o.LockTimeout = d.defaultTx.LockTimeout
	}
	if o.StatementTimeout == 0 {
		o.StatementTimeout = d.defaultTx.StatementTimeout
	}
	if o.IsoLevel == "" {
		o.IsoLevel = d.defaultTx.IsoLevel
	}
	if o.IsoLevel == "" {
		o.IsoLevel = pgx.ReadCommitted
	}
	if len(d.defaultTx.Settings) > 0 {
		o.Settings = append(append([]Setting(nil), d.defaultTx.Settings...), o.Settings...)
	}
	return o
}

// runOnce là một lần chạy: mở transaction, đặt giới hạn, gọi fn, commit. Mọi đường ra khác commit thành công đều lùi.
func (d *DB) runOnce(ctx context.Context, o TxOptions, fn func(pgx.Tx) error) (err error) {
	tx, err := d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: o.IsoLevel})
	if err != nil {
		return fmt.Errorf("mở transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			// Lùi cả khi ctx đã hủy, có hạn để mạng treo không giữ goroutine; lỗi lùi không che lỗi chính
			// (pgx đóng kết nối hỏng khi lùi không được).
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
			defer cancel()
			_ = tx.Rollback(rctx)
		}
	}()
	if names, vals := txConfig(o); len(names) > 0 {
		if err := store.New(tx).SetTxConfig(ctx, store.SetTxConfigParams{Names: names, Vals: vals}); err != nil {
			return fmt.Errorf("đặt giới hạn của transaction: %w", err)
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// txConfig trả cặp tên, giá trị cho set_config: hai giới hạn (bỏ cái bằng 0) rồi Settings.
func txConfig(o TxOptions) (names, vals []string) {
	if o.LockTimeout > 0 {
		names, vals = append(names, "lock_timeout"), append(vals, pgDuration(o.LockTimeout))
	}
	if o.StatementTimeout > 0 {
		names, vals = append(names, "statement_timeout"), append(vals, pgDuration(o.StatementTimeout))
	}
	for _, s := range o.Settings {
		names, vals = append(names, s.Name), append(vals, s.Value)
	}
	return names, vals
}

// sleep chờ d hoặc tới khi ctx hủy.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("chờ chạy lại transaction: %w", ctx.Err())
	}
}
