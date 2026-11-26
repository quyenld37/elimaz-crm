package deps

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Lệnh tải tệp đính kèm của tin vào (Task 155; TP02, TP04): chỉ đọc từ máy chủ của kênh, hạn 1 giờ, mỗi lần 30 s.
// Handler ở phần inbound; phần nào ghi tin có đính kèm (đường vào, lịch sử) xếp lệnh bằng EnqueueAttachmentFetches.
const (
	fetchAttachmentName    = "command.fb.fetch_attachment"
	fetchAttachmentExpiry  = time.Hour
	fetchAttachmentTimeout = 30 * time.Second
)

// FetchAttachmentKind là loại lệnh tải một tệp đính kèm.
func FetchAttachmentKind() queue.Kind {
	return commands.CommandKind(fetchAttachmentName, fetchAttachmentExpiry, fetchAttachmentTimeout)
}

// AttachmentPayload là payload của lệnh tải: chỉ mã tin và chỉ số mục (URL nằm trong mục, không trong lệnh).
type AttachmentPayload struct {
	MessageID uuid.UUID `json:"messageId"`
	Index     int       `json:"index"`
}

// EnqueueAttachmentFetches xếp lệnh tải từng tệp có URL của tin msgID (tenant tenantID) trong tx của bên ghi tin; mục
// không có URL (sticker không ảnh, fallback) giữ pending không lệnh. DedupeKey <msgID>:<chỉ số>: tin giao lại không thêm
// lệnh.
func EnqueueAttachmentFetches(ctx context.Context, tx pgx.Tx, q queue.JobQueue, tenantID, msgID uuid.UUID,
	refs []channel.AttachmentRef,
) error {
	for i, r := range refs {
		if r.URL == "" {
			continue
		}
		if _, _, err := q.Enqueue(ctx, tx, FetchAttachmentKind(), queue.NewJob{
			TenantID: &tenantID, Payload: AttachmentPayload{MessageID: msgID, Index: i},
			DedupeKey: msgID.String() + ":" + strconv.Itoa(i),
		}); err != nil {
			return fmt.Errorf("ghi lệnh tải tệp đính kèm: %w", err)
		}
	}
	return nil
}
