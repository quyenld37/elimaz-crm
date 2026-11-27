package webcache_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/wiring/webcache"
)

func stored(t *testing.T, tenant uuid.UUID, topic, aggType, aggID string, payload any) events.StoredEvent {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return events.StoredEvent{
		ID: uuid.New(), TenantID: tenant, AggregateType: aggType, AggregateID: aggID, Seq: 1, Topic: topic, Payload: b,
	}
}

func TestConsumer_NameTopics(t *testing.T) {
	c := webcache.NewConsumer(&realtimetest.RecordingBus{}, nil)
	if c.Name() != "webcache" {
		t.Fatalf("Name = %q", c.Name())
	}
	want := []string{
		"product.updated", "price.changed", "stock.changed", "flash_sale.opened", "flash_sale.closed",
		"flash_sale.changed", "order.cancelled", "carrier.web_settings_changed", "promotion.web_campaign.changed",
		"order.web_intake.changed", "content.banners.changed", "content.looks.changed",
	}
	if got := c.Topics(); !slices.Equal(got, want) {
		t.Fatalf("Topics = %v, muốn %v", got, want)
	}
	if err := events.NewRegistry().Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

type payloadCase struct {
	name   string
	e      events.StoredEvent
	want   []realtime.Entity
	badLog bool
}

// Từng chủ đề → các báo hiệu hệ thống (A4, payload của M1 bản 2); payload sai → ghi log, không báo hiệu, không lỗi.
func TestConsumer_Payloads(t *testing.T) {
	tenant := uuid.New()
	for _, c := range payloadCases(t, tenant) {
		t.Run(c.name, func(t *testing.T) { checkHandle(t, tenant, c.e, c.want, c.badLog) })
	}
}

// payloadCases: các ca của TestConsumer_Payloads (sự kiện đã lưu của tenant → báo hiệu mong đợi).
func payloadCases(t *testing.T, tenant uuid.UUID) []payloadCase {
	t.Helper()
	p, v1, v2 := uuid.New(), uuid.New(), uuid.New()
	prod := realtime.Entity{Type: "product", ID: p.String()}
	ent := func(typ string, id uuid.UUID) realtime.Entity { return realtime.Entity{Type: typ, ID: id.String()} }
	fs := uuid.NewString()
	return []payloadCase{
		{"product.updated", stored(t, tenant, catalog.TopicProductUpdated, "product", p.String(),
			catalog.ProductUpdated{ProductID: p, Version: 3}), []realtime.Entity{prod}, false},
		{
			"price.changed hai biến thể", stored(t, tenant, catalog.TopicPriceChanged, "variant", v1.String(),
				catalog.PriceChanged{ProductID: p, VariantIDs: []uuid.UUID{v1, v2}, Price: 100}),
			[]realtime.Entity{prod, ent("variant", v1), ent("variant", v2)},
			false,
		},
		{
			"price.changed trùng biến thể", stored(t, tenant, catalog.TopicPriceChanged, "variant", v1.String(),
				catalog.PriceChanged{ProductID: p, VariantIDs: []uuid.UUID{v1, v1}}),
			[]realtime.Entity{prod, ent("variant", v1)},
			false,
		},
		{"stock.changed", stored(t, tenant, inventory.TopicStockChanged, "balance", v1.String()+":w",
			inventory.StockChanged{VariantIDs: []uuid.UUID{v2, v1}}), []realtime.Entity{ent("variant", v2), ent("variant", v1)}, false},
		{
			"flash_sale.opened", stored(t, tenant, "flash_sale.opened", "flash_sale", fs, map[string]any{}),
			[]realtime.Entity{{Type: "flash_sale", ID: fs}},
			false,
		},
		{
			"flash_sale.closed", stored(t, tenant, "flash_sale.closed", "flash_sale", fs, nil),
			[]realtime.Entity{{Type: "flash_sale", ID: fs}},
			false,
		},
		{
			"flash_sale.changed", stored(t, tenant, "flash_sale.changed", "flash_sale", fs, nil),
			[]realtime.Entity{{Type: "flash_sale", ID: fs}},
			false,
		},
		{
			"carrier.web_settings_changed", stored(t, tenant, "carrier.web_settings_changed", "carrier_settings", "ghn", nil),
			[]realtime.Entity{{Type: "carrier_settings", ID: "ghn"}},
			false,
		},
		{
			"promotion.web_campaign.changed [WB06]", stored(t, tenant, promotion.TopicWebCampaignChanged, "web_campaign", fs,
				promotion.WebCampaignChanged{CampaignID: p, Revision: 2}),
			[]realtime.Entity{{Type: "web_campaign", ID: fs}},
			false,
		},
		{
			"order.web_intake.changed [WB01]", stored(t, tenant, order.TopicWebIntakeChanged, "web_intake", tenant.String(),
				order.WebIntakeChanged{Paused: true, Version: 2}),
			[]realtime.Entity{{Type: "web_intake", ID: tenant.String()}},
			false,
		},
		{"content.banners.changed [KM04]", stored(t, tenant, content.TopicBannersChanged, "home_banners", tenant.String(),
			content.BannersChanged{Revision: 3}), []realtime.Entity{ent("home_banners", tenant)}, false},
		{"content.looks.changed [KM04]", stored(t, tenant, content.TopicLooksChanged, "home_looks", tenant.String(),
			content.LooksChanged{Revision: 2}), []realtime.Entity{ent("home_looks", tenant)}, false},
		{"order.cancelled thiếu orderId", stored(t, tenant, "order.cancelled", "order", uuid.NewString(),
			map[string]any{"status": "cancelled"}), nil, true},
		{"chủ đề lạ", stored(t, tenant, "x.y", "x", "1", nil), nil, false},
		{"price.changed thiếu variantIds", stored(t, tenant, catalog.TopicPriceChanged, "variant", v1.String(),
			map[string]any{"productId": p}), nil, true},
		{"price.changed variantIds rỗng", stored(t, tenant, catalog.TopicPriceChanged, "variant", v1.String(),
			map[string]any{"productId": p, "variantIds": []string{}}), nil, true},
		{"price.changed thiếu productId", stored(t, tenant, catalog.TopicPriceChanged, "variant", v1.String(),
			map[string]any{"variantIds": []uuid.UUID{v1}}), nil, true},
		{"stock.changed thiếu variantIds", stored(t, tenant, inventory.TopicStockChanged, "balance", "x",
			map[string]any{"warehouseId": uuid.New()}), nil, true},
		{"stock.changed mã rỗng", stored(t, tenant, inventory.TopicStockChanged, "balance", "x",
			map[string]any{"variantIds": []uuid.UUID{uuid.Nil}}), nil, true},
		{"product.updated thiếu productId", stored(t, tenant, catalog.TopicProductUpdated, "product", "x",
			map[string]any{"version": 1}), nil, true},
		{"product.updated không phải JSON đối tượng", stored(t, tenant, catalog.TopicProductUpdated, "product", "x",
			[]int{1}), nil, true},
	}
}

// checkHandle chạy Handle với bus ghi lại; báo hiệu phải đúng want (hệ thống, cache.invalidate, đúng tenant), có log
// "payload sai" (kèm mã sự kiện) đúng khi badLog.
func checkHandle(t *testing.T, tenant uuid.UUID, e events.StoredEvent, want []realtime.Entity, badLog bool) {
	t.Helper()
	bus := &realtimetest.RecordingBus{}
	var logs bytes.Buffer
	con := webcache.NewConsumer(bus, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err := con.Handle(t.Context(), e, nil); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var got, staff []realtime.Entity
	for _, s := range bus.Signals() {
		if !s.System && s.Topic == "flash_sale.changed" && s.Requires == "promotion.flash.read" && s.TenantID == tenant &&
			s.Scope == nil {
			staff = append(staff, s.Entity)
			continue
		}
		if !s.System || s.Topic != "cache.invalidate" || s.TenantID != tenant || s.Requires != "" || s.Scope != nil {
			t.Fatalf("báo hiệu sai dạng: %+v", s)
		}
		got = append(got, s.Entity)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("báo hiệu %v, muốn %v", got, want)
	}
	// Sự kiện đợt flash sale: thêm đúng một báo hiệu cho màn quản lý đợt (Task 200); chủ đề khác: không có.
	var wantStaff []realtime.Entity
	if strings.HasPrefix(e.Topic, "flash_sale.") {
		wantStaff = []realtime.Entity{{Type: "flash_sale", ID: e.AggregateID}}
	}
	if !slices.Equal(staff, wantStaff) {
		t.Fatalf("báo hiệu màn đợt %v, muốn %v", staff, wantStaff)
	}
	if hasLog := strings.Contains(logs.String(), "payload sai"); hasLog != badLog {
		t.Fatalf("log payload sai = %v, muốn %v: %s", hasLog, badLog, logs.String())
	}
	if badLog && !strings.Contains(logs.String(), e.ID.String()) {
		t.Fatalf("log thiếu mã sự kiện: %s", logs.String())
	}
}

// errBus: Publish lỗi → Handle trả lỗi (worker chạy lại theo lịch).
type errBus struct{ realtimetest.RecordingBus }

var errPublish = errors.New("notify hỏng")

func (*errBus) Publish(context.Context, pgx.Tx, realtime.Signal) error { return errPublish }

func TestConsumer_PublishError(t *testing.T) {
	p := uuid.New()
	e := stored(t, uuid.New(), catalog.TopicProductUpdated, "product", p.String(), catalog.ProductUpdated{ProductID: p})
	if err := webcache.NewConsumer(&errBus{}, nil).Handle(t.Context(), e, nil); !errors.Is(err, errPublish) {
		t.Fatalf("Handle: %v, muốn errPublish", err)
	}
}

// Đổi giá ba biến thể của P (ba price.changed, aggregate variant) và một product.updated: báo hiệu cho P và v1, v2, v3
// tới người đăng ký chỉ khi transaction của consumer commit; transaction lùi → không báo hiệu nào.
func TestConsumer_SignalsOnlyOnCommit(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	tenant := e.staff.Tenant(t, "A")
	ch := e.sink(t, "cache.")
	con := webcache.NewConsumer(e.hub, nil)
	p := uuid.New()
	vs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	evs := make([]events.StoredEvent, 0, 4)
	for _, v := range vs {
		evs = append(evs, stored(t, tenant, catalog.TopicPriceChanged, "variant", v.String(),
			catalog.PriceChanged{ProductID: p, VariantIDs: []uuid.UUID{v}, Price: 1000}))
	}
	evs = append(evs, stored(t, tenant, catalog.TopicProductUpdated, "product", p.String(), catalog.ProductUpdated{ProductID: p}))
	handleAll(t, e, con, tenant, evs, true)
	handleAll(t, e, con, tenant, evs, false)
	// Báo hiệu đánh dấu phát sau cùng: mọi báo hiệu tới trước nó là của transaction đã commit.
	marker := uuid.New()
	if err := tenancy.WithTenant(ctx, e.staff.App, tenant, func(tx pgx.Tx) error {
		return e.hub.Publish(ctx, tx, realtime.Signal{
			TenantID: tenant, Topic: "cache.invalidate", System: true,
			Entity: realtime.Entity{Type: "product", ID: marker.String()},
		})
	}); err != nil {
		t.Fatal(err)
	}
	counts := collectUntil(t, ch, tenant, marker.String())
	// Bốn lần Publish cho P trong một transaction: PostgreSQL gộp NOTIFY trùng payload của cùng transaction thành một.
	want := map[realtime.Entity]int{{Type: "product", ID: p.String()}: 1}
	for _, v := range vs {
		want[realtime.Entity{Type: "variant", ID: v.String()}] = 1
	}
	if len(counts) != len(want) {
		t.Fatalf("báo hiệu %v, muốn %v (bản lùi không được tới)", counts, want)
	}
	for k, n := range want {
		if counts[k] != n {
			t.Fatalf("báo hiệu %v, muốn %v (bản lùi không được tới)", counts, want)
		}
	}
}

var errRollback = errors.New("lùi")

// handleAll chạy Handle cho mọi sự kiện trong một transaction tenant; rollback thì lùi transaction sau đó.
func handleAll(t *testing.T, e *env, con *webcache.Consumer, tenant uuid.UUID, evs []events.StoredEvent, rollback bool) {
	t.Helper()
	ctx := t.Context()
	err := tenancy.WithTenant(ctx, e.staff.App, tenant, func(tx pgx.Tx) error {
		for _, ev := range evs {
			if err := con.Handle(ctx, ev, tx); err != nil {
				return fmt.Errorf("handle: %w", err)
			}
		}
		if rollback {
			return errRollback
		}
		return nil
	})
	if err != nil && (!rollback || !errors.Is(err, errRollback)) {
		t.Fatal(err)
	}
}

// collectUntil đếm các đối tượng nhận được tới báo hiệu đánh dấu markerID (không tính nó).
func collectUntil(t *testing.T, ch chan realtime.Signal, tenant uuid.UUID, markerID string) map[realtime.Entity]int {
	t.Helper()
	counts := map[realtime.Entity]int{}
	for {
		s := next(t, ch)
		if s.Entity.ID == markerID {
			return counts
		}
		if s.TenantID != tenant {
			t.Fatalf("tenant sai: %+v", s)
		}
		counts[s.Entity]++
	}
}
