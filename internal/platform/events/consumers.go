package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ErrInvalidConsumer báo consumer có tên rỗng, sai mẫu, trùng tên, hoặc không nghe topic nào.
var ErrInvalidConsumer = errors.New("events: consumer không hợp lệ")

var consumerName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Consumer là một module nghe sự kiện.
type Consumer interface {
	// Name là tên cố định (mẫu [a-z][a-z0-9_]*): việc của consumer có kind event.<Name>; đổi tên làm mất tiến độ.
	Name() string
	// Topics là các topic consumer nghe.
	Topics() []string
	// Handle chạy trong tx của worker (WithTenant của sự kiện); trả lỗi thì thay đổi trong tx bị lùi và việc được hẹn
	// chạy lại theo lịch của hàng đợi (bọc queue.ErrPermanent để dead ngay). Như queue.Handler: chạy lại được, không
	// gọi mạng, không gửi gì ra ngoài database.
	Handle(ctx context.Context, e StoredEvent, tx pgx.Tx) error
}

// Registry là sổ consumer của tiến trình, nạp một lần lúc khởi động (cmd/crm), giống nhau ở api (Emit) và worker
// (JobHandlers). Không an toàn khi Register chạy đồng thời với Emit.
type Registry struct {
	consumers []Consumer
	byTopic   map[string][]Consumer
}

// NewRegistry trả sổ rỗng.
func NewRegistry() *Registry { return &Registry{byTopic: map[string][]Consumer{}} }

// Register thêm c vào sổ.
func (r *Registry) Register(c Consumer) error {
	name := c.Name()
	switch {
	case !consumerName.MatchString(name):
		return fmt.Errorf("%w: tên %q", ErrInvalidConsumer, name)
	case slices.ContainsFunc(r.consumers, func(o Consumer) bool { return o.Name() == name }):
		return fmt.Errorf("%w: trùng tên %q", ErrInvalidConsumer, name)
	case len(c.Topics()) == 0:
		return fmt.Errorf("%w: %q không nghe topic nào", ErrInvalidConsumer, name)
	}
	r.consumers = append(r.consumers, c)
	for _, topic := range c.Topics() {
		if !slices.Contains(r.byTopic[topic], c) {
			r.byTopic[topic] = append(r.byTopic[topic], c)
		}
	}
	return nil
}

// ConsumerKind là loại việc của consumer name: event.<name>, chính sách mặc định với thứ tự nghiêm OrderStrict (việc
// đầu hàng dead chặn việc sau của đúng đối tượng đó, spec 04 mục 5.1).
func ConsumerKind(name string) queue.Kind {
	p := queue.DefaultPolicy
	p.OrderMode = queue.OrderStrict
	return queue.Kind{Name: "event." + name, Policy: p}
}

// JobHandlers trả mỗi consumer một queue.Handler (đăng ký vào queue.NewWorker của vai trò worker).
func (r *Registry) JobHandlers() []queue.Handler {
	hs := make([]queue.Handler, 0, len(r.consumers))
	for _, c := range r.consumers {
		hs = append(hs, consumerHandler{c: c})
	}
	return hs
}

// consumerHandler đọc sự kiện theo eventId của việc (trong tenant của việc) rồi giao cho consumer.
type consumerHandler struct{ c Consumer }

func (h consumerHandler) Kind() queue.Kind { return ConsumerKind(h.c.Name()) }

func (h consumerHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	var p eventPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.EventID == uuid.Nil || j.TenantID == nil {
		return nil, fmt.Errorf("việc %s: payload hoặc tenant không hợp lệ: %w", j.ID, queue.ErrPermanent)
	}
	row, err := store.New(tx).GetDomainEvent(ctx, store.GetDomainEventParams{TenantID: *j.TenantID, ID: p.EventID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("sự kiện %s không có: %w", p.EventID, queue.ErrPermanent)
	}
	if err != nil {
		return nil, fmt.Errorf("đọc sự kiện %s: %w", p.EventID, err)
	}
	e := StoredEvent{
		ID: row.ID, TenantID: row.TenantID, AggregateType: row.AggregateType, AggregateID: row.AggregateID,
		Seq: row.Seq, Topic: row.Topic, Payload: row.Payload,
	}
	if err := h.c.Handle(ctx, e, tx); err != nil {
		return nil, fmt.Errorf("consumer %s, sự kiện %s: %w", h.c.Name(), e.ID, err)
	}
	return nil, nil
}
