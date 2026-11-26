package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// LiveStatus xử lý sự kiện live.status của nguồn source (Task 170, đường nhận biết bằng webhook live_videos, Q5) trong
// tx của sự kiện vào: LIVE → phiên live detected_by webhook (trùng với hỏi định kỳ vẫn một phiên); LIVE_STOPPED, VOD →
// phiên kết thúc; trạng thái chưa phát, Page không có trong shop → bỏ qua. Bộ phân phát của Task 153 gọi hàm này cho
// channel.EventLiveStatus. Data hỏng → queue.ErrPermanent.
func LiveStatus(ctx context.Context, tx pgx.Tx, source string, e webhooks.Inbound) error {
	var p channel.LiveStatusPayload
	if err := json.Unmarshal(e.Data, &p); err != nil {
		return fmt.Errorf("giải dữ liệu sự kiện %s: %w", e.Type, queue.ErrPermanent)
	}
	state, ok := live.StateOf(p.Status)
	if !ok || p.LiveVideoID == "" {
		return nil
	}
	acc, err := channel.AccountByExternal(ctx, tx, source, p.PageID)
	if errors.Is(err, channel.ErrAccountNotFound) {
		return nil
	}
	if err != nil {
		return err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	at := p.At
	if at.IsZero() {
		at = e.OccurredAt
	}
	if at.IsZero() {
		at = e.ReceivedAt
	}
	_, _, err = live.UpsertSession(ctx, tx, live.Observation{
		AccountID: acc.ID, LiveVideoID: p.LiveVideoID, VideoID: p.VideoID, Title: p.Title, State: state,
		DetectedBy: live.DetectedWebhook, At: at,
	})
	return err //nolint:wrapcheck // live đã bọc việc đang làm
}
