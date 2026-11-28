package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Unavailable báo err là lỗi "database ngừng, quá tải hay quá hạn" (Task 529): mọi lỗi của Unreachable, cộng SQLSTATE
// lớp 53 (thiếu tài nguyên: 53100 đầy đĩa, 53200 hết bộ nhớ…), lớp 57 (57014 quá hạn câu lệnh…) và quá hạn
// (context.DeadlineExceeded, pgconn.Timeout) khi database có thể vẫn sống. Hủy từ phía bên gọi (context.Canceled)
// không tính. api đổi thành 503 retry_later kèm Retry-After như ErrRetryLater; RetryTx không chạy lại các lỗi này
// (database ngừng thì chạy lại chỉ thêm tải). Lỗi Unavailable mà không Unreachable vẫn được ghi như lỗi ứng dụng.
func Unavailable(err error) bool {
	if Unreachable(err) {
		return true
	}
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch sqlstateClass(pe) {
		case "53", "57":
			return true
		}
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err)
}

// Unreachable báo err là lỗi "không tới được database" (Task 529): không nối được (*pgconn.ConnectError, trừ khi máy
// chủ đã trả lời bằng SQLSTATE ngoài nhóm dưới, vd 28P01 sai mật khẩu), SQLSTATE lớp 08 (kết nối), 57P01 đang tắt,
// 57P02 sập, 57P03 đang khởi động, 53300 quá nhiều kết nối, kết nối đứt giữa chừng hay đã đóng (lỗi tầng truyền của
// pgconn). Quá hạn do ctx (context.DeadlineExceeded) không tính: database có thể vẫn sống. Lúc này không ghi được
// error_events (spec 04, "Gom lỗi": việc đó do bộ kiểm từ ngoài báo), api trả 503 mà không ghi lỗi từng yêu cầu.
func Unreachable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pe.Code {
		case "57P01", "57P02", "57P03", "53300":
			return true
		}
		return sqlstateClass(pe) == "08"
	}
	if _, ok := errors.AsType[*pgconn.ConnectError](err); ok {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Lỗi tầng truyền của pgconn (gửi, nhận hỏng; "conn closed"; quá hạn mạng) đều có SafeToRetry; lỗi câu lệnh
	// (PgError) thì không. Cũng tính "conn busy", "conn uninitialized" (pgx: lỗi lập trình, vd một pgx.Tx dùng từ hai
	// goroutine): không phân biệt được mà không so chuỗi; mã của CRM không dùng chung kết nối giữa goroutine.
	_, transport := errors.AsType[transportError](err)
	return transport
}

// sqlstateClass là hai ký tự đầu (lớp) của SQLSTATE.
func sqlstateClass(pe *pgconn.PgError) string {
	return pe.Code[:min(len(pe.Code), 2)]
}

// transportError: kiểu lỗi tầng truyền của pgconn (pgconnError, connLockError, errTimeout…, không xuất ra ngoài).
type transportError interface {
	error
	SafeToRetry() bool
}
