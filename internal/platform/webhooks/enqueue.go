package webhooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// bodyKindEvent: bản nhận là một sự kiện đã chuẩn hóa do EnqueueInbound ghi (cột body_kind, migration 0291).
const bodyKindEvent = "event"

// ErrInvalidEvent báo tham số của EnqueueInbound sai (tenant, nguồn, giờ nhận rỗng hay via lạ): lỗi lập trình của bên
// gọi, không ghi gì.
var ErrInvalidEvent = errors.New("webhooks: sự kiện vào không hợp lệ")

// packetRef chỉ tới một sự kiện trong bản nhận (mã bản nhận, vị trí trong SplitResult.Events) cùng giờ nhận và đường
// tới của nó.
type packetRef struct {
	DeliveryID uuid.UUID
	Index      int
	ReceivedAt time.Time
	Via        string
}

// storedEvent là thân của bản nhận body_kind event: đúng các trường của InboundEvent mà handler đọc lại (không Route:
// tenant đã biết). Không có giờ nhận, via: cùng một sự kiện đọc lại nhiều lần cho cùng một thân, một dòng.
type storedEvent struct {
	Type       string          `json:"type"`
	DedupeKey  string          `json:"dedupeKey"`
	OrderKey   string          `json:"orderKey"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}

// EnqueueInbound ghi một sự kiện vào không đến từ ingest (M4: luồng live, đọc comment, hỏi bù, lịch sử) cho tenantID
// trong tx của bên gọi, như SplitHandler ghi sự kiện của webhook: cùng DedupeKey (rỗng thì theo bản nhận), OrderKey
// (tiền tố InboundKind(source)), OrderSeq = OccurredAt.UnixMicro(); nên sự kiện đã vào qua webhook hay lần đọc trước
// không tạo thêm việc (created = false). Nội dung lưu vào platform.webhook_deliveries (body_kind event, một dòng cho
// mỗi nội dung); payload của việc chỉ mang mã bản nhận cùng receivedAt, via (spec 04 mục 11). via là một trong
// ViaWebhook … ViaHistory. Bảng ngoài thứ tự nhóm (a) rồi platform.jobs (nhóm 13): gọi sau mọi khóa dòng nghiệp vụ.
func EnqueueInbound(ctx context.Context, tx pgx.Tx, q queue.JobQueue, tenantID uuid.UUID, source string, e InboundEvent,
	receivedAt time.Time, via string,
) (created bool, err error) {
	switch {
	case tenantID == uuid.Nil, source == "", receivedAt.IsZero():
		return false, fmt.Errorf("%w: thiếu tenant, nguồn hoặc giờ nhận", ErrInvalidEvent)
	case !validVia(via):
		return false, fmt.Errorf("%w: via %q", ErrInvalidEvent, via)
	}
	body, err := json.Marshal(storedEvent{
		Type: e.Type, DedupeKey: e.DedupeKey, OrderKey: e.OrderKey, OccurredAt: e.OccurredAt.UTC(), Payload: e.Payload,
	})
	if err != nil {
		return false, fmt.Errorf("mã hóa sự kiện vào %s: %w", source, err)
	}
	id, err := storeEvent(ctx, tx, source, body, receivedAt.UTC())
	if err != nil {
		return false, err
	}
	return enqueueEvent(ctx, tx, q, tenantID, source, e, packetRef{DeliveryID: id, Index: 0, ReceivedAt: receivedAt, Via: via})
}

// storeEvent chèn bản nhận body_kind event, trùng thân thì trả mã của dòng có sẵn (READ COMMITTED: dòng của
// transaction khác vừa commit thấy được ở câu đọc sau ON CONFLICT).
func storeEvent(ctx context.Context, tx pgx.Tx, source string, body []byte, receivedAt time.Time) (uuid.UUID, error) {
	sum := eventHash(body)
	q := store.New(tx)
	id, err := q.InsertWebhookEvent(ctx, store.InsertWebhookEventParams{
		Source: source, BodySha256: sum, Body: body, ReceivedAt: receivedAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		id, err = q.FindWebhookDelivery(ctx, store.FindWebhookDeliveryParams{Source: source, BodySha256: sum})
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lưu sự kiện vào %s: %w", source, err)
	}
	return id, nil
}

// decodeStoredEvent đọc thân body_kind event thành SplitResult một sự kiện.
func decodeStoredEvent(body []byte) (SplitResult, error) {
	var se storedEvent
	if err := json.Unmarshal(body, &se); err != nil {
		return SplitResult{}, fmt.Errorf("giải mã sự kiện đã lưu: %w", err)
	}
	return SplitResult{Events: []InboundEvent{{
		Type: se.Type, DedupeKey: se.DedupeKey, OrderKey: se.OrderKey, OccurredAt: se.OccurredAt, Payload: se.Payload,
	}}}, nil
}

// Băm chống trùng của platform.webhook_deliveries (UNIQUE (source, body_sha256)): thân gốc không route key giữ đúng
// sha256 của thân như M0; có route key thì băm cả khóa (hai tuyến cùng thân là hai bản nhận); sự kiện lưu có tiền tố
// riêng để không trùng thân gốc.
func packetHash(routeKey string, body []byte) string {
	if routeKey == "" {
		return sha256Hex(body)
	}
	return sha256Hex(append([]byte("route\x00"+routeKey+"\x00"), body...))
}

func eventHash(body []byte) string { return sha256Hex(append([]byte("event\x00"), body...)) }

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// enqueueEvent ghi việc InboundKind(source) của sự kiện e cho tenant trong tx: payload chỉ mang mã bản nhận (spec 04
// mục 11) cùng giờ nhận, via; chống trùng theo DedupeKey (rỗng thì theo vị trí trong bản nhận), thứ tự theo OrderKey
// và OccurredAt. created = false khi việc cùng khóa chống trùng đã có.
func enqueueEvent(ctx context.Context, tx pgx.Tx, q queue.JobQueue, tenant uuid.UUID, source string, e InboundEvent,
	ref packetRef,
) (bool, error) {
	kind := InboundKind(source)
	key := eventDedupeKey(e, ref.DeliveryID, ref.Index)
	nj := queue.NewJob{
		TenantID: &tenant,
		Payload: inboundPayload{
			Type: e.Type, DeliveryID: ref.DeliveryID, Index: ref.Index, DedupeKey: key, OccurredAt: e.OccurredAt.UTC(),
			ReceivedAt: ref.ReceivedAt.UTC(), Via: ref.Via,
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
