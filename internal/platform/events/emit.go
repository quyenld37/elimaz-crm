// Package events là sự kiện nội bộ (spec 04 mục 5.4): module phát sự kiện bằng Registry.Emit trong transaction
// nghiệp vụ; sự kiện được ghi vào platform.domain_events (chỉ ghi thêm) và mỗi consumer nghe topic đó nhận một việc
// event.<consumer> riêng trên hàng đợi chung, nên mỗi consumer có tiến độ, thử lại và thứ tự riêng.
//
// Bất biến: Event.Seq lấy từ cột version của đối tượng (AggregateType, AggregateID), tăng dưới khóa dòng của đối
// tượng trong cùng transaction với Emit. Nhờ vậy hai sự kiện của một đối tượng không bao giờ cùng Seq, và việc của
// một consumer chạy đúng thứ tự Seq (OrderKey = "<consumer>:<aggregateType>:<aggregateID>", OrderSeq = Seq); việc
// đầu hàng dead chặn các việc sau của đúng đối tượng đó (BlockOnDead).
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

var (
	// ErrInvalidEvent báo Event thiếu trường bắt buộc hoặc Payload không mã hóa JSON được (không ghi gì).
	ErrInvalidEvent = errors.New("events: sự kiện không hợp lệ")
	// ErrDuplicateEvent báo đã có sự kiện cùng (tenant, AggregateType, AggregateID, Seq): Seq không lấy từ version
	// tăng dưới khóa dòng. Transaction của bên gọi đã hỏng, phải lùi.
	ErrDuplicateEvent = errors.New("events: trùng số thứ tự của đối tượng")
)

// Tên ràng buộc unique (tenant_id, aggregate_type, aggregate_id, seq) trong migration 0008.
const aggregateSeqConstraint = "domain_events_aggregate_seq_key"

// Event là một sự kiện cần phát.
type Event struct {
	TenantID      uuid.UUID
	AggregateType string
	AggregateID   string
	// Seq: cột version của đối tượng, tăng dưới khóa dòng (xem đầu tệp); lớn hơn 0.
	Seq   int64
	Topic string
	// Payload được mã hóa JSON (nil → {}); chỉ mang mã bản ghi, không mang nội dung hay dữ liệu cá nhân.
	Payload any
}

// StoredEvent là sự kiện đã ghi, giao cho Consumer.
type StoredEvent struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	AggregateType string
	AggregateID   string
	Seq           int64
	Topic         string
	Payload       json.RawMessage
}

// eventPayload là payload của việc event.<consumer>: chỉ mã sự kiện.
type eventPayload struct {
	EventID uuid.UUID `json:"eventId"`
}

// Emit ghi e vào domain_events trong tx của bên gọi (tx phải là tenancy.WithTenant của e.TenantID: RLS chặn tenant
// khác) và, với mỗi consumer nghe e.Topic, ghi một việc event.<consumer> cũng trong tx. Trả mã sự kiện và số việc mới
// ghi. tx lùi thì không còn sự kiện lẫn việc.
func (r *Registry) Emit(ctx context.Context, tx pgx.Tx, q queue.JobQueue, e Event) (uuid.UUID, int, error) {
	if e.TenantID == uuid.Nil || e.AggregateType == "" || e.AggregateID == "" || e.Topic == "" || e.Seq < 1 {
		return uuid.Nil, 0, fmt.Errorf("%w: cần TenantID, AggregateType, AggregateID, Topic và Seq > 0", ErrInvalidEvent)
	}
	payload := []byte("{}")
	if e.Payload != nil {
		b, err := json.Marshal(e.Payload)
		if err != nil {
			return uuid.Nil, 0, fmt.Errorf("%w: mã hóa payload: %w", ErrInvalidEvent, err)
		}
		payload = b
	}
	id, err := store.New(tx).InsertDomainEvent(ctx, store.InsertDomainEventParams{
		TenantID: e.TenantID, AggregateType: e.AggregateType, AggregateID: e.AggregateID, Seq: e.Seq, Topic: e.Topic,
		Payload: payload,
	})
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" && pgErr.ConstraintName == aggregateSeqConstraint {
			return uuid.Nil, 0, fmt.Errorf("ghi sự kiện %s seq %d: %w", e.Topic, e.Seq, ErrDuplicateEvent)
		}
		return uuid.Nil, 0, fmt.Errorf("ghi sự kiện %s: %w", e.Topic, err)
	}
	jobs := 0
	for _, c := range r.byTopic[e.Topic] {
		_, created, err := q.Enqueue(ctx, tx, ConsumerKind(c.Name()), queue.NewJob{
			TenantID:  &e.TenantID,
			Payload:   eventPayload{EventID: id},
			DedupeKey: id.String(),
			OrderKey:  c.Name() + ":" + e.AggregateType + ":" + e.AggregateID,
			OrderSeq:  e.Seq,
		})
		if err != nil {
			return uuid.Nil, 0, fmt.Errorf("ghi việc của consumer %s: %w", c.Name(), err)
		}
		if created {
			jobs++
		}
	}
	return id, jobs, nil
}
