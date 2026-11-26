package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// HandlePolicy xử lý sự kiện policy.enforcement của nguồn source (kế hoạch M4, Task 150; 02 mục 8) trong tx tenant của
// việc vào: tìm Page theo mã trên kênh rồi gọi channel.Writer.ReportPolicyEnforcement (degraded meta_policy:<hành
// động>, cảnh báo gấp meta-policy:<id>; unblock gỡ). Page không có trong shop → bỏ qua; Data hỏng → queue.ErrPermanent.
// Lý do Meta gửi kèm (văn bản tự do) không được ghi đi đâu. Handler vào của nguồn (Task 153) gọi hàm này cho loại
// channel.EventPolicyEnforcement.
func HandlePolicy(ctx context.Context, tx pgx.Tx, d deps.Deps, source string, e webhooks.Inbound) error {
	var p channel.PolicyPayload
	if err := json.Unmarshal(e.Data, &p); err != nil || p.PageID == "" {
		return fmt.Errorf("giải dữ liệu sự kiện %s: %w", e.Type, queue.ErrPermanent)
	}
	acc, err := channel.AccountByExternal(ctx, tx, source, p.PageID)
	if errors.Is(err, channel.ErrAccountNotFound) {
		return nil
	}
	if err != nil {
		return err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	return channel.Writer{Realtime: d.Bus}.ReportPolicyEnforcement(ctx, tx, acc.ID, p.Action) //nolint:wrapcheck // channel đã bọc
}
