// Package db mở pool pgx có giới hạn thời gian (ADR-26), chạy transaction có thử lại (db.RetryTx, ADR-46) và chạy
// migration goose có checksum và luật mở rộng trước (Migrate, LintContract).
//
// Mọi transaction của mã sản phẩm đi qua DB.RetryTx (trực tiếp hoặc qua tenancy.WithTenant); không gói nào ngoài
// gói này gọi Begin, BeginTx, pgx.BeginFunc (test kiến trúc txusage_test kiểm).
package db

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Giới hạn mặc định của mọi kết nối ứng dụng (ADR-26).
const (
	defaultStatementTimeout = 30 * time.Second
	defaultIdleInTxTimeout  = 60 * time.Second
	defaultLockTimeout      = 5 * time.Second
)

// PoolOptions cấu hình pool. Trường thời gian bằng 0 lấy mặc định của ADR-26 (30 s, 60 s, 5 s); MaxConns bằng 0
// giữ mặc định của pgxpool.
type PoolOptions struct {
	MaxConns         int32
	StatementTimeout time.Duration
	IdleInTxTimeout  time.Duration
	LockTimeout      time.Duration
	// DefaultTx là bộ giới hạn theo transaction của vai trò (TxAPI, TxIngest, TxWorker); RetryTx dùng nó cho trường
	// nào của TxOptions bằng 0.
	DefaultTx TxOptions
	// Tracing gắn otelpgx (câu SQL không kèm giá trị tham số) khi dấu vết bật.
	Tracing bool
}

// DB giữ pool và hai móc chỉ số của RetryTx.
type DB struct {
	Pool *pgxpool.Pool
	// OnRetry được gọi trước mỗi lần chạy lại (code là SQLSTATE, attempt là số lần chạy đã lỗi: 1, 2, 3).
	// Task 10 nối vào chỉ số tx_retry. nil thì bỏ qua. Phải an toàn khi gọi từ nhiều goroutine.
	OnRetry func(code string, attempt int)
	// OnExhausted được gọi khi hết lượt (trả ErrRetryLater). Task 10 nối vào tx_retry_exhausted.
	OnExhausted func(code string)

	defaultTx TxOptions
}

// Open mở pool tới url: đặt statement_timeout, idle_in_transaction_session_timeout, lock_timeout qua RuntimeParams
// (giá trị mặc định của mọi câu không đi qua RetryTx), chế độ câu lệnh QueryExecModeCacheStatement (cấm simple
// protocol), và otelpgx khi o.Tracing. Pool mở lười: lỗi kết nối hiện ra ở câu đầu tiên.
func Open(ctx context.Context, url string, o PoolOptions) (*DB, error) {
	pc, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("đọc chuỗi kết nối database: %w", err)
	}
	if o.MaxConns > 0 {
		pc.MaxConns = o.MaxConns
	}
	setSessionLimits(pc.ConnConfig, o.StatementTimeout, o.IdleInTxTimeout, o.LockTimeout)
	pc.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement
	if o.Tracing {
		pc.ConnConfig.Tracer = otelpgx.NewTracer()
	}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("mở pool database: %w", err)
	}
	return &DB{Pool: pool, defaultTx: o.DefaultTx}, nil
}

// setSessionLimits đặt ba giới hạn của ADR-26 (0 = mặc định) vào tham số khởi động của kết nối.
func setSessionLimits(cc *pgx.ConnConfig, statement, idleInTx, lock time.Duration) {
	cc.RuntimeParams["statement_timeout"] = pgDuration(orDefault(statement, defaultStatementTimeout))
	cc.RuntimeParams["idle_in_transaction_session_timeout"] = pgDuration(orDefault(idleInTx, defaultIdleInTxTimeout))
	cc.RuntimeParams["lock_timeout"] = pgDuration(orDefault(lock, defaultLockTimeout))
}

func orDefault(d, def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return d
}

// pgDuration viết thời lượng dương theo mili giây, dạng PostgreSQL hiểu ("1500ms"). Làm tròn lên ít nhất 1 ms:
// "0" với PostgreSQL nghĩa là không giới hạn.
func pgDuration(d time.Duration) string {
	return strconv.FormatInt(max(d.Milliseconds(), 1), 10) + "ms"
}
