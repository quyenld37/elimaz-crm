package inbound

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// OnInboundAttachments xếp lệnh tải từng tệp đính kèm của tin messageID (Task 155 hiện thực; khung của Task 153: tin
// đã được ghi với danh sách đính kèm status pending).
func OnInboundAttachments(context.Context, pgx.Tx, uuid.UUID, []channel.AttachmentRef) error {
	return nil
}
