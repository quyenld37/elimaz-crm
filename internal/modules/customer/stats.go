package customer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Chỉ số khách (kế hoạch M2, Task M2-25; Q19, Q-X2, 05 mục 1): luôn tính lại toàn bộ từ dữ liệu gốc — đơn qua
// ord.orders_v, phiếu trả qua ord.returns_v của mọi hồ sơ trong nhóm — không cộng dồn theo sự kiện, nên sự kiện tới sai
// thứ tự, tới hai lần hay gộp/hoàn tác đều cho cùng số cuối. Ghi vào hồ sơ gốc; hồ sơ con về 0.

const (
	// StatsConsumerName là tên consumer chỉ số khách (việc event.customer_stats); đổi tên làm mất tiến độ.
	StatsConsumerName = "customer_stats"
	// TopicOrderDeliveryReversed là sự kiện hãng đảo trạng thái "đã giao" (M3 phát, C3; trước M3 không có sự kiện này).
	TopicOrderDeliveryReversed = "order.delivery_reversed"

	// statsAttempts: số lần khóa lại khi gốc hay nhóm đổi giữa lúc đọc và lúc khóa (gộp, hoàn tác song song).
	statsAttempts = 3
)

// errGroupRetry chỉ dùng để lùi savepoint (nhả khóa) khi gốc hay nhóm đổi giữa lúc đọc và lúc khóa; không ra khỏi gói.
var errGroupRetry = errors.New("customer: nhóm đổi khi đang khóa")

// lockedGroups là các nhóm đã khóa trong tx: gốc (theo id tăng dần) và mọi hồ sơ của từng gốc (gồm gốc, theo id).
type lockedGroups struct {
	tenantID uuid.UUID
	roots    []uuid.UUID
	members  map[uuid.UUID][]uuid.UUID
}

// lockGroups khóa mọi hồ sơ của các nhóm chứa ids (mỗi mã là gốc hay hồ sơ con) trong một câu, theo id tăng dần (ADR-46
// nhóm 5, như gộp), rồi đọc lại gốc và nhóm dưới khóa; lệch (gộp, hoàn tác vừa commit) → lùi savepoint để nhả khóa rồi
// làm lại, tối đa 3 lần. Mã không có → ErrNotFound.
func lockGroups(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (lockedGroups, error) {
	for range statsAttempts {
		var out lockedGroups
		err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
			var err error
			out, err = lockGroupsOnce(ctx, sp, ids)
			return err
		})
		switch {
		case errors.Is(err, errGroupRetry):
			continue
		case err != nil:
			return lockedGroups{}, err //nolint:wrapcheck // lỗi của lockGroupsOnce đã bọc; lỗi savepoint do db bọc
		}
		return out, nil
	}
	return lockedGroups{}, errRootChurn
}

func lockGroupsOnce(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (lockedGroups, error) {
	g := lockedGroups{members: map[uuid.UUID][]uuid.UUID{}}
	owner := map[uuid.UUID]uuid.UUID{}
	var all []uuid.UUID
	for _, id := range ids {
		root, err := RootOf(ctx, tx, id)
		if err != nil {
			return lockedGroups{}, err
		}
		if _, ok := g.members[root]; ok {
			continue
		}
		members, err := GroupMembers(ctx, tx, root)
		if err != nil {
			return lockedGroups{}, err
		}
		g.members[root] = members
		g.roots = append(g.roots, root)
		for _, m := range members {
			owner[m] = root
		}
		all = append(all, members...)
	}
	locked, err := store.New(tx).LockCustomers(ctx, all)
	if err != nil {
		return lockedGroups{}, fmt.Errorf("khóa nhóm khách: %w", err)
	}
	if len(locked) != len(all) || slices.ContainsFunc(locked, func(r store.LockCustomersRow) bool {
		return r.RootID != owner[r.ID]
	}) {
		return lockedGroups{}, errGroupRetry
	}
	for root, members := range g.members {
		again, err := GroupMembers(ctx, tx, root)
		if err != nil {
			return lockedGroups{}, err
		}
		if !slices.Equal(again, members) { // hồ sơ mới vừa gộp vào gốc này
			return lockedGroups{}, errGroupRetry
		}
	}
	g.tenantID = locked[0].TenantID
	g.roots = sortIDs(g.roots)
	return g, nil
}

// RecomputeStats tính lại chỉ số của nhóm chứa customerID (gốc hay hồ sơ con; tính cho gốc **hiện tại**) trong tx của
// bên gọi, từ ord.orders_v và ord.returns_v của mọi hồ sơ trong nhóm: order_count = đơn không draft, không cancelled;
// delivered_count = delivered; returned_count = returned (hoàn qua hãng); total_spent = Σ grand_total đơn delivered −
// Σ refund_amount phiếu customer_return/exchange đã restocked hoặc closed (không âm); last_order_at = created_at lớn
// nhất của đơn được đếm. Ghi vào gốc, đặt hồ sơ con về 0; báo hiệu customer.changed. Khóa cả nhóm (ADR-46 nhóm 5).
// Không áp quy tắc nhãn (consumer customer_stats áp ngay sau). Không có → ErrNotFound.
func (w Writer) RecomputeStats(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, now time.Time) error {
	_, _, err := w.recompute(ctx, tx, []uuid.UUID{customerID}, now)
	return err
}

// recompute khóa các nhóm chứa ids, tính lại chỉ số từng gốc và trả các nhóm đã khóa kèm chỉ số mới.
func (w Writer) recompute(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, now time.Time,
) (lockedGroups, map[uuid.UUID]Stats, error) {
	g, err := lockGroups(ctx, tx, ids)
	if err != nil {
		return lockedGroups{}, nil, err
	}
	now = now.UTC()
	q := store.New(tx)
	out := make(map[uuid.UUID]Stats, len(g.roots))
	for _, root := range g.roots {
		members := g.members[root]
		s, err := q.ComputeGroupStats(ctx, members)
		if err != nil {
			return lockedGroups{}, nil, fmt.Errorf("tính chỉ số khách: %w", err)
		}
		last := pgtype.Timestamptz{}
		if s.OrderCount > 0 {
			last = pgtype.Timestamptz{Time: s.LastOrderAt, Valid: true}
		}
		if err := q.SetRootStats(ctx, store.SetRootStatsParams{
			OrderCount: s.OrderCount, DeliveredCount: s.DeliveredCount, ReturnedCount: s.ReturnedCount,
			TotalSpent: s.TotalSpent, LastOrderAt: last, Now: now, ID: root,
		}); err != nil {
			return lockedGroups{}, nil, fmt.Errorf("ghi chỉ số khách: %w", err)
		}
		if children := slices.DeleteFunc(slices.Clone(members), func(id uuid.UUID) bool { return id == root }); len(children) > 0 {
			if err := q.ZeroMemberStats(ctx, store.ZeroMemberStatsParams{Now: now, Ids: children}); err != nil {
				return lockedGroups{}, nil, fmt.Errorf("đặt chỉ số hồ sơ con về 0: %w", err)
			}
		}
		out[root] = statsOf(s.OrderCount, s.DeliveredCount, s.ReturnedCount, s.TotalSpent, last)
		// Gốc còn lead mà nhóm đã có đơn (gộp một nhóm đã mua vào gốc tạo tay): lead → customer như ở đơn đầu tiên
		// (review độc lập M2, A-03). promote không làm gì khi gốc đã là customer.
		if s.OrderCount > 0 {
			if err := w.promote(ctx, tx, root, Actor{Type: ActorSystem}, now); err != nil {
				return lockedGroups{}, nil, err
			}
		}
		if err := w.signal(ctx, tx, g.tenantID, root); err != nil {
			return lockedGroups{}, nil, err
		}
	}
	return g, out, nil
}

// StatsFor trả chỉ số của **gốc hiện tại** của khách customerID (kể cả khi customerID là hồ sơ con), gồm ReturnRateBP.
// Đây là chỗ duy nhất các module khác đọc tỷ lệ nhận/hoàn (Q-X2, B14, C3; M3 chỉ thêm ngưỡng của mình). Không có (hay
// của tenant khác) → ErrNotFound.
func StatsFor(ctx context.Context, tx pgx.Tx, customerID uuid.UUID) (Stats, error) {
	root, err := RootOf(ctx, tx, customerID)
	if err != nil {
		return Stats{}, err
	}
	r, err := store.New(tx).GetStats(ctx, root)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stats{}, ErrNotFound
	}
	if err != nil {
		return Stats{}, fmt.Errorf("đọc chỉ số khách: %w", err)
	}
	return statsOf(r.OrderCount, r.DeliveredCount, r.ReturnedCount, r.TotalSpent, r.LastOrderAt), nil
}

// StatsConsumer tính lại chỉ số khách rồi áp quy tắc nhãn cho gốc hiện tại (events.Consumer; module.go đăng ký). Nghe
// đơn đổi trạng thái (order.*: khách là customerId của payload) và gộp, hoàn tác (customer.merged, merge_undone: tính
// lại cả hai gốc). Việc event.customer_stats chạy thứ tự nghiêm theo đơn; hai đơn của một khách chạy song song thì nối
// tiếp ở khóa nhóm, và mỗi lần tính từ dữ liệu đã commit. Payload hỏng hay khách không có → queue.ErrPermanent.
type StatsConsumer struct {
	W Writer
	// Now là đồng hồ (nil: time.Now); số ngày từ đơn cuối của quy tắc nhãn tính theo giờ này.
	Now func() time.Time
}

// Name trả StatsConsumerName.
func (StatsConsumer) Name() string { return StatsConsumerName }

// Topics trả các sự kiện đổi chỉ số: order.created, confirmed, cancelled, delivered, returned, partially_returned,
// delivery_reversed (tên của module order, không nhập Go order), customer.merged, customer.merge_undone.
func (StatsConsumer) Topics() []string {
	return []string{
		"order.created", "order.confirmed", "order.cancelled", "order.delivered", "order.returned",
		"order.partially_returned", TopicOrderDeliveryReversed, TopicCustomerMerged, TopicCustomerMergeUndone,
	}
}

// statsSubject là phần payload consumer cần: khách của sự kiện order.* hoặc hai gốc của MergeEvent.
type statsSubject struct {
	CustomerID     uuid.UUID `json:"customerId"`
	SurvivorRootID uuid.UUID `json:"survivorRootId"`
	MergedRootID   uuid.UUID `json:"mergedRootId"`
}

// Handle tính lại chỉ số và áp quy tắc nhãn cho gốc của khách trong sự kiện e, trong tx của worker.
func (c StatsConsumer) Handle(ctx context.Context, e events.StoredEvent, tx pgx.Tx) error {
	var p statsSubject
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("sự kiện %s: payload không hợp lệ: %w", e.ID, queue.ErrPermanent)
	}
	ids := []uuid.UUID{p.CustomerID}
	if e.Topic == TopicCustomerMerged || e.Topic == TopicCustomerMergeUndone {
		ids = []uuid.UUID{p.SurvivorRootID, p.MergedRootID}
	}
	if slices.Contains(ids, uuid.Nil) {
		return fmt.Errorf("sự kiện %s: thiếu mã khách: %w", e.ID, queue.ErrPermanent)
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	g, stats, err := c.W.recompute(ctx, tx, ids, now)
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("sự kiện %s: khách không có: %w", e.ID, queue.ErrPermanent)
	}
	if err != nil {
		return fmt.Errorf("tính lại chỉ số khách: %w", err)
	}
	rules, err := loadTagRules(ctx, tx)
	if err != nil {
		return err
	}
	for _, root := range g.roots {
		if err := c.W.applyRules(ctx, tx, g.tenantID, root, g.members[root], stats[root], rules, now); err != nil {
			return err
		}
	}
	return nil
}
