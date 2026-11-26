package channel

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/observability"
)

// AuthAlertKind là lệnh cảnh báo "token Page hỏng" (key channel-auth:<id>, mức gấp), ghi trong tx của degradeAuth
// (Writer.ReportTokenInvalid, Writer.ReportAuthFailure, kiểm sức khỏe) khi tài khoản vừa chuyển sang degraded vì mất
// quyền; worker gửi qua Alerter sau commit.
const AuthAlertKind = "channel.auth_alert"

// ReasonTokenInvalid là lý do trạng thái degraded khi kênh báo token hỏng (Meta mã 190…).
const ReasonTokenInvalid = "token_invalid"

// ReportTokenInvalid ghi kênh báo token của tài khoản id hỏng (lệnh gửi nhận lỗi lớp auth, Task 156; trong tx tenant
// của bên gọi, trước khi khóa bảng nhóm sau, ADR-46 nhóm 3): khóa dòng rồi đi cùng đường với kiểm sức khỏe
// (degradeAuth, Task 150) — degraded lý do token_invalid, một lệnh cảnh báo gấp AuthAlertKind và báo hiệu
// channel.status, chỉ khi chưa degraded vì mất quyền (lý do nào); Page đã ngắt → không làm gì. Không có tài khoản →
// ErrAccountNotFound.
func (w Writer) ReportTokenInvalid(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return w.lockAndDegradeAuth(ctx, tx, id, ReasonTokenInvalid)
}

// NewAuthAlertCommand trả lệnh AuthAlertKind: cảnh báo gấp key "channel-auth:<accountId>", văn bản có tên Page, kênh,
// mã tài khoản (không token); như NewSendModeAlertCommand. Một handler cho cả hai nơi ghi: ReportTokenInvalid (gửi tin
// nhận lỗi token, Task 156) và kiểm sức khỏe mất quyền (Task 150); cmd/crm đăng ký qua HealthAlertCommands.
func NewAuthAlertCommand(a observability.Alerter, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return urgentPageAlert(AuthAlertKind, "channel-auth:", "mất quyền hoặc hết hiệu lực token, tin vẫn nhận nhưng chưa gửi được", a, logger)
}
