package channel

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/observability"
)

// AuthAlertKind là lệnh cảnh báo "token Page hỏng" (key channel-auth:<id>, mức gấp), ghi trong tx của ReportAuthFailure
// khi tài khoản vừa chuyển sang degraded; worker gửi qua Alerter sau commit.
const AuthAlertKind = "channel.auth_alert"

// ReasonTokenInvalid là lý do trạng thái degraded khi kênh báo token hỏng (Meta mã 190…).
const ReasonTokenInvalid = "token_invalid"

// ReportAuthFailure ghi kênh báo token của tài khoản id hỏng (lệnh gửi nhận lỗi lớp auth): tài khoản → degraded lý do
// token_invalid; lần đầu đổi (chưa degraded vì token) thì ghi một lệnh cảnh báo gấp AuthAlertKind (ref: mã của lần báo,
// chống trùng trong loại). Gọi trong tx tenant của bên gọi, trước khi khóa bảng nhóm sau (ADR-46 nhóm 3). Không có tài
// khoản → ErrAccountNotFound.
func ReportAuthFailure(ctx context.Context, tx pgx.Tx, id uuid.UUID, ref string) error {
	changed, err := MarkStatus(ctx, tx, id, StatusDegraded, ReasonTokenInvalid)
	if err != nil || !changed {
		return err
	}
	a, err := Account(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := enqueueAlert(ctx, tx, AuthAlertKind, a, "channel-auth:"+a.ID.String()+":"+ref); err != nil {
		return fmt.Errorf("ghi cảnh báo token Page hỏng: %w", err)
	}
	return nil
}

// NewAuthAlertCommand trả lệnh AuthAlertKind: cảnh báo gấp key "channel-auth:<accountId>", văn bản có tên Page, kênh,
// mã tài khoản (không token); như NewSendModeAlertCommand.
func NewAuthAlertCommand(a observability.Alerter, logger *slog.Logger) commands.Handler { //nolint:ireturn // Runner nhận Handler
	return urgentPageAlert(AuthAlertKind, "channel-auth:", "hết hiệu lực token, tin không gửi được", a, logger)
}
