package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
)

// InboundKind là loại việc của một sự kiện vào từ nguồn source: inbound.<source>, chính sách mặc định (không
// BlockOnDead: một sự kiện dead không chặn sự kiện sau, spec 04 mục 5.1).
func InboundKind(source string) queue.Kind {
	return queue.Kind{Name: "inbound." + source, Policy: queue.DefaultPolicy}
}

// Inbound là một sự kiện vào giao cho InboundHandler.
type Inbound struct {
	Type       string
	OccurredAt time.Time
	// Data là InboundEvent.Payload, đọc lại từ bản nhận lúc xử lý (payload của việc không mang nội dung).
	Data     json.RawMessage
	JobID    uuid.UUID
	TenantID uuid.UUID
}

// InboundHandler xử lý sự kiện vào của một nguồn (module nghiệp vụ, adapter kênh).
type InboundHandler interface {
	// Source là Key của nguồn handler nhận.
	Source() string
	// Handle chạy trong tx có ngữ cảnh tenant của sự kiện (tenancy.WithTenant). Như queue.Handler: chạy lại được
	// (chống trùng tầng 2 bằng khóa tự nhiên của bảng nghiệp vụ), không gọi mạng; trả lỗi thì thay đổi bị lùi và việc
	// được hẹn chạy lại (bọc queue.ErrPermanent để dead ngay).
	Handle(ctx context.Context, e Inbound, tx pgx.Tx) error
}

// InboundJobHandlers trả mỗi InboundHandler một queue.Handler của InboundKind(Source()). sources là các nguồn để đọc
// lại sự kiện từ bản nhận (cùng danh sách với SplitHandler). Handler của nguồn không có trong sources, hai handler
// cùng nguồn: ErrInvalidOptions.
func InboundJobHandlers(sources []Source, hs []InboundHandler) ([]queue.Handler, error) {
	sm, err := sourceMap(sources)
	if err != nil {
		return nil, err
	}
	out := make([]queue.Handler, 0, len(hs))
	seen := map[string]bool{}
	for _, h := range hs {
		src := h.Source()
		if _, ok := sm[src]; !ok || seen[src] {
			return nil, fmt.Errorf("%w: handler sự kiện vào của nguồn %q không có nguồn hoặc trùng", ErrInvalidOptions, src)
		}
		seen[src] = true
		out = append(out, inboundJobHandler{h: h, sources: sm})
	}
	return out, nil
}

// inboundJobHandler đọc lại sự kiện từ bản nhận theo payload của việc rồi giao cho InboundHandler.
type inboundJobHandler struct {
	h       InboundHandler
	sources map[string]Source
}

func (ih inboundJobHandler) Kind() queue.Kind { return InboundKind(ih.h.Source()) }

func (ih inboundJobHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	var p inboundPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.DeliveryID == uuid.Nil || j.TenantID == nil {
		return nil, fmt.Errorf("việc %s: payload hoặc tenant không hợp lệ: %w", j.ID, queue.ErrPermanent)
	}
	src, res, err := loadPacket(ctx, tx, ih.sources, p.DeliveryID)
	if err != nil {
		return nil, fmt.Errorf("việc %s: %w", j.ID, err)
	}
	if src != ih.h.Source() || p.Index < 0 || p.Index >= len(res.Events) ||
		eventDedupeKey(res.Events[p.Index], p.DeliveryID, p.Index) != p.DedupeKey {
		return nil, fmt.Errorf("việc %s: sự kiện %d không khớp bản nhận %s: %w", j.ID, p.Index, p.DeliveryID, queue.ErrPermanent)
	}
	e := res.Events[p.Index]
	in := Inbound{Type: e.Type, OccurredAt: e.OccurredAt.UTC(), Data: e.Payload, JobID: j.ID, TenantID: *j.TenantID}
	if err := ih.h.Handle(ctx, in, tx); err != nil {
		return nil, fmt.Errorf("sự kiện vào %s, việc %s: %w", in.Type, j.ID, err)
	}
	return nil, nil
}
