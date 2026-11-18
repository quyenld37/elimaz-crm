package webhooks

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
)

// packetRef chỉ tới một sự kiện trong bản nhận: mã bản nhận và vị trí trong SplitResult.Events của nó.
type packetRef struct {
	DeliveryID uuid.UUID
	Index      int
}

// enqueueEvent ghi việc InboundKind(source) của sự kiện e cho tenant trong tx: payload chỉ mang mã bản nhận (spec 04
// mục 11), chống trùng theo DedupeKey (rỗng thì theo vị trí trong bản nhận), thứ tự theo OrderKey và OccurredAt.
// created = false khi việc cùng khóa chống trùng đã có.
func enqueueEvent(ctx context.Context, tx pgx.Tx, q queue.JobQueue, tenant uuid.UUID, source string, e InboundEvent,
	ref packetRef,
) (bool, error) {
	kind := InboundKind(source)
	key := eventDedupeKey(e, ref.DeliveryID, ref.Index)
	nj := queue.NewJob{
		TenantID: &tenant,
		Payload: inboundPayload{
			Type: e.Type, DeliveryID: ref.DeliveryID, Index: ref.Index, DedupeKey: key, OccurredAt: e.OccurredAt.UTC(),
		},
		DedupeKey: key,
		OrderSeq:  e.OccurredAt.UnixMicro(),
	}
	if e.OrderKey != "" {
		// Tiền tố theo loại việc: order_key của platform.jobs dùng chung mọi loại việc trong một tenant.
		nj.OrderKey = kind.Name + ":" + e.OrderKey
	}
	_, created, err := q.Enqueue(ctx, tx, kind, nj)
	if err != nil {
		return false, fmt.Errorf("ghi việc sự kiện vào %s: %w", kind.Name, err)
	}
	return created, nil
}
