package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Chỉ số của bước tách gói (đếm theo lần chạy: việc tách chạy lại thì đếm lại).
const (
	// metricUnrouted: sự kiện không tìm thấy tenant (nhãn: nguồn); không xử lý. Bản nhận nguyên văn còn 30 ngày; M0
	// chưa có công cụ chạy lại việc tách đã done (nếu có, chống trùng tầng 1 giữ không tạo việc trùng).
	metricUnrouted = "inbound_unrouted"
	// metricSkipped: sự kiện loại lạ nguồn bỏ qua (nhãn: "<nguồn>:<loại>").
	metricSkipped = "inbound_skipped"
)

// SplitOptions cấu hình SplitHandler.
type SplitOptions struct {
	// Sources: các nguồn worker biết tách; bản nhận của nguồn khác thì việc tách dead.
	Sources []Source
	// Metrics nil: không ghi chỉ số.
	Metrics *metrics.Registry
}

// SplitHandler là handler của việc SplitKind (không tenant): đọc bản nhận theo deliveryId, tách gói bằng
// Source.Split, tìm tenant của từng sự kiện qua platform.tenant_routes và ghi mỗi sự kiện một việc InboundKind có
// tenant, chống trùng theo DedupeKey, thứ tự theo OrderKey và OccurredAt. Mọi việc ghi trong transaction của việc tách:
// chạy lại không ghi thêm việc nào.
type SplitHandler struct {
	sources map[string]Source
	m       *metrics.Registry
	q       queue.JobQueue
}

// NewSplitHandler dựng SplitHandler; hai nguồn cùng Key hoặc Key rỗng là ErrInvalidOptions.
func NewSplitHandler(o SplitOptions) (*SplitHandler, error) {
	sources, err := sourceMap(o.Sources)
	if err != nil {
		return nil, err
	}
	return &SplitHandler{sources: sources, m: o.Metrics, q: queue.NewQueue()}, nil
}

// Kind trả SplitKind.
func (*SplitHandler) Kind() queue.Kind { return SplitKind }

// splitResult là kết quả của việc tách (cột result): chỉ số đếm, không nội dung.
type splitResult struct {
	Events   int `json:"events"`
	Enqueued int `json:"enqueued"`
	Unrouted int `json:"unrouted"`
	Skipped  int `json:"skipped"`
}

// inboundPayload là payload của việc InboundKind: chỉ mang mã (spec 04 mục 11), không mang nội dung sự kiện. Handler
// đọc lại nội dung từ bản nhận (Index là vị trí trong SplitResult.Events của bản nhận DeliveryID). ReceivedAt, Via
// (Task M2-33) trống ở việc ghi trước đó.
type inboundPayload struct {
	Type       string    `json:"type"`
	DeliveryID uuid.UUID `json:"deliveryId"`
	Index      int       `json:"index"`
	DedupeKey  string    `json:"dedupeKey"`
	OccurredAt time.Time `json:"occurredAt"`
	ReceivedAt time.Time `json:"receivedAt"`
	Via        string    `json:"via"`
}

// Handle tách gói của việc j. Payload sai, bản nhận không còn, nguồn lạ hay thân không đọc được là lỗi vĩnh viễn
// (queue.ErrPermanent); lỗi database thì thử lại.
func (h *SplitHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	var p splitPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.DeliveryID == uuid.Nil {
		return nil, fmt.Errorf("việc tách %s: payload không hợp lệ: %w", j.ID, queue.ErrPermanent)
	}
	pk, err := loadPacket(ctx, tx, h.sources, p.DeliveryID)
	if err != nil {
		return nil, fmt.Errorf("việc tách %s: %w", j.ID, err)
	}
	srcKey, res := pk.source, pk.res
	out := splitResult{Events: len(res.Events)}
	for typ, n := range res.Skipped {
		out.Skipped += n
		h.inc(metricSkipped, srcKey+":"+typ, n)
	}
	routes := map[Route]*uuid.UUID{}
	for i, e := range res.Events {
		tenant, err := h.resolve(ctx, tx, routes, e.Route)
		if err != nil {
			return nil, fmt.Errorf("việc tách %s, sự kiện %d: %w", j.ID, i, err)
		}
		if tenant == nil {
			out.Unrouted++
			h.inc(metricUnrouted, srcKey, 1)
			continue
		}
		created, err := enqueueEvent(ctx, tx, h.q, *tenant, srcKey, e,
			packetRef{DeliveryID: p.DeliveryID, Index: i, ReceivedAt: pk.receivedAt, Via: ViaWebhook})
		if err != nil {
			return nil, fmt.Errorf("việc tách %s, sự kiện %d: %w", j.ID, i, err)
		}
		if created {
			out.Enqueued++
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("việc tách %s: mã hóa kết quả: %w", j.ID, err)
	}
	return b, nil
}

// resolve tìm tenant của r (nhớ trong một gói); nil khi không có định tuyến.
func (*SplitHandler) resolve(ctx context.Context, tx pgx.Tx, cache map[Route]*uuid.UUID, r Route) (*uuid.UUID, error) {
	if t, ok := cache[r]; ok {
		return t, nil
	}
	id, ok, err := tenancy.ResolveTx(ctx, tx, r.Kind, r.ExternalKey)
	if err != nil {
		return nil, fmt.Errorf("định tuyến: %w", err)
	}
	var t *uuid.UUID
	if ok {
		t = &id
	}
	cache[r] = t
	return t, nil
}

func (h *SplitHandler) inc(name, label string, n int) {
	if h.m != nil {
		h.m.Inc(name, label, int64(n))
	}
}

// eventDedupeKey là khóa chống trùng của sự kiện; nguồn không cho khóa thì dùng vị trí trong bản nhận (chỉ chống
// được giao lại cùng bản nhận).
func eventDedupeKey(e InboundEvent, deliveryID uuid.UUID, i int) string {
	if e.DedupeKey != "" {
		return e.DedupeKey
	}
	return deliveryID.String() + ":" + strconv.Itoa(i)
}

// packet là một bản nhận đã đọc lại và tách: nguồn, các sự kiện, giờ nhận.
type packet struct {
	source     string
	res        SplitResult
	receivedAt time.Time
}

// loadPacket đọc bản nhận id trong tx và tách: thân gốc có route_key bằng SplitRouted của nguồn (RoutedSource), thân
// gốc khác bằng Split, sự kiện ghi bằng EnqueueInbound (body_kind event) thành đúng một sự kiện. Bản nhận không còn,
// nguồn lạ hay không phải RoutedSource khi có route_key, thân không đọc được là queue.ErrPermanent.
func loadPacket(ctx context.Context, tx pgx.Tx, sources map[string]Source, id uuid.UUID) (packet, error) {
	row, err := store.New(tx).GetWebhookDelivery(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return packet{}, fmt.Errorf("bản nhận %s không còn: %w", id, queue.ErrPermanent)
	}
	if err != nil {
		return packet{}, fmt.Errorf("đọc bản nhận %s: %w", id, err)
	}
	src, ok := sources[row.Source]
	if !ok {
		return packet{}, fmt.Errorf("bản nhận %s: nguồn %q không có: %w", id, row.Source, queue.ErrPermanent)
	}
	var res SplitResult
	switch {
	case row.BodyKind == bodyKindEvent:
		res, err = decodeStoredEvent(row.Body)
	case row.RouteKey.Valid:
		rs, routed := src.(RoutedSource)
		if !routed {
			return packet{}, fmt.Errorf("bản nhận %s: nguồn %q không nhận route key: %w", id, row.Source, queue.ErrPermanent)
		}
		res, err = rs.SplitRouted(row.Body, row.RouteKey.String)
	default:
		res, err = src.Split(row.Body)
	}
	if err != nil {
		// Không bọc err của Split: có thể trích nội dung thân.
		return packet{}, fmt.Errorf("bản nhận %s: thân không đọc được: %w", id, queue.ErrPermanent)
	}
	return packet{source: row.Source, res: res, receivedAt: row.ReceivedAt}, nil
}

func sourceMap(ss []Source) (map[string]Source, error) {
	m := make(map[string]Source, len(ss))
	for _, s := range ss {
		if _, dup := m[s.Key()]; dup || s.Key() == "" {
			return nil, fmt.Errorf("%w: khóa nguồn %q rỗng hoặc trùng", ErrInvalidOptions, s.Key())
		}
		m[s.Key()] = s
	}
	return m, nil
}
