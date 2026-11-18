package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Dọn bản nhận (lịch platform.cleanup của Task 13): giữ 30 ngày, mỗi lần tối đa 5.000 dòng.
const (
	deliveryRetention  = 30 * 24 * time.Hour
	cleanupBatchRows   = 5000
	emptyHeadersObject = "{}"
)

// Entry là một lần nhận webhook: nguồn, header đã che, thân gốc, giờ nhận, khóa tuyến (đường /hooks/{source}/{routeKey};
// rỗng với /hooks/{source}). Cũng là nội dung một tệp tạm (JSON {source, headers, bodyBase64, receivedAt, routeKey?}).
type Entry struct {
	Source string `json:"source"`
	// Headers là JSON của header đã qua logging.Redact.
	Headers    json.RawMessage `json:"headers"`
	Body       []byte          `json:"bodyBase64"`
	ReceivedAt time.Time       `json:"receivedAt"`
	RouteKey   string          `json:"routeKey,omitempty"`
}

// splitPayload là payload của việc tách gói: chỉ mang mã bản nhận (spec 04 mục 11).
type splitPayload struct {
	DeliveryID uuid.UUID `json:"deliveryId"`
}

// Save lưu e trong một transaction (db.RetryTx): chèn platform.webhook_deliveries (trùng (source, sha256 của thân —
// của route key cùng thân khi có route key, packetHash) thì không chèn) và, chỉ khi vừa chèn, ghi việc SplitKind với DedupeKey là mã bản nhận. created báo đã chèn dòng mới.
func Save(ctx context.Context, d *db.DB, e Entry) (created bool, err error) {
	headers := e.Headers
	if len(headers) == 0 || string(headers) == "null" {
		headers = []byte(emptyHeadersObject)
	}
	err = d.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		created = false
		id, err := store.New(tx).InsertWebhookDelivery(ctx, store.InsertWebhookDeliveryParams{
			Source:     e.Source,
			BodySha256: packetHash(e.RouteKey, e.Body),
			Headers:    headers,
			Body:       e.Body,
			ReceivedAt: e.ReceivedAt.UTC(),
			RouteKey:   pgtype.Text{String: e.RouteKey, Valid: e.RouteKey != ""},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // trùng: bản trước đã có việc tách gói của nó
		}
		if err != nil {
			return fmt.Errorf("chèn bản nhận: %w", err)
		}
		if _, _, err := queue.NewQueue().Enqueue(ctx, tx, SplitKind, queue.NewJob{
			Payload: splitPayload{DeliveryID: id}, DedupeKey: id.String(),
		}); err != nil {
			return fmt.Errorf("ghi việc tách gói: %w", err)
		}
		created = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("lưu webhook %s: %w", e.Source, err)
	}
	return created, nil
}

// CleanupDeliveries xóa tối đa 5.000 bản nhận cũ hơn 30 ngày tính từ now, trong tx của bên gọi. Chữ ký khớp hàm
// dọn của lịch platform.cleanup (Task 13, RegisterCleanup).
func CleanupDeliveries(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error) {
	n, err := store.New(tx).DeleteOldWebhookDeliveries(ctx, store.DeleteOldWebhookDeliveriesParams{
		Before: now.Add(-deliveryRetention), MaxRows: cleanupBatchRows,
	})
	if err != nil {
		return 0, fmt.Errorf("dọn bản nhận webhook: %w", err)
	}
	return n, nil
}
