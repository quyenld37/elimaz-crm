package db

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrRetryLater báo transaction gặp lỗi tạm thời (40P01, 55P03, 40001) ở cả lần chạy đầu và 3 lần chạy lại.
// Lỗi trả về bọc cả ErrRetryLater và lỗi gốc: errors.Is(err, ErrRetryLater) và TransientCode(err) đều dùng được.
// api đổi thành 503 retry_later (không lưu vào idempotency_keys), worker coi là một lần thử lỗi (ADR-46 điểm 2).
var ErrRetryLater = errors.New("db: retry later")

// maxRetries: một lần chạy đầu cộng tối đa 3 lần chạy lại (hàm được gọi tối đa 4 lần, ADR-46 điểm 1).
const maxRetries = 3

const backoffBase = 20 * time.Millisecond

// Backoff là thời gian chờ trước lần chạy lại thứ n (n ≥ 1): 20 ms × 2^(n−1) nhân (0,5 + rnd()), với rnd trong [0, 1)
// (20, 40, 80 ms ± 50%).
func Backoff(n int, rnd func() float64) time.Duration {
	base := backoffBase << max(n-1, 0)
	return time.Duration(float64(base) * (0.5 + rnd()))
}

// TransientCode đọc SQLSTATE của *pgconn.PgError trong chuỗi lỗi; ok khi mã là lỗi tạm thời mà RetryTx chạy lại:
// 40P01 deadlock_detected, 55P03 lock_not_available, 40001 serialization_failure. Lỗi không có SQLSTATE trả "".
func TransientCode(err error) (code string, ok bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return "", false
	}
	switch pgErr.Code {
	case "40P01", "55P03", "40001":
		return pgErr.Code, true
	}
	return pgErr.Code, false
}
