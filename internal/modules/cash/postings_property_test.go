package cash_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// journey là hành trình của một vận đơn chia lô theo thứ tự thời gian; collected là tiền hãng báo đã thu, gắn vào lô
// có bước giao xong (nil: không báo).
type journey struct {
	lots      [][]fulfillment.CarrierEvent
	collected *int64
}

// journeyDay là ngày của mọi sự kiện hành trình (UTC).
func journeyDay() time.Time { return time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC) }

// carrierEvent là sự kiện hãng thứ n của hành trình (khóa duy nhất, mỗi sự kiện sau một phút).
func carrierEvent(n int, c fulfillment.Canonical) fulfillment.CarrierEvent {
	at := journeyDay().Add(8 * time.Hour).Add(time.Duration(n) * time.Minute)
	return fulfillment.CarrierEvent{Key: "j" + strconv.Itoa(n), RawStatus: "raw-" + string(c), Canonical: c, OccurredAt: at}
}

// movingCanonicals là các tin "đang đi" (lấy hàng, đang chuyển, đang giao).
func movingCanonicals() []fulfillment.Canonical {
	return []fulfillment.Canonical{fulfillment.CanonPickedUp, fulfillment.CanonInTransit, fulfillment.CanonOutForDelivery}
}

// drawJourney sinh hành trình như Task 98/99 (fulfillment apply_property_test): có thể báo chờ lấy, hủy trước lấy hàng;
// lấy hàng, thất bại và giao lại vài lần; kết thúc giao xong (đủ hay một phần), đang hoàn (có thể kèm đã hoàn về
// shop), giao xong rồi hãng sửa thành hoàn, hay dừng giữa đường. Chia 1–3 lô liên tiếp.
func drawJourney(rt *rapid.T, cod int64) journey {
	var evs []fulfillment.CarrierEvent
	add := func(c fulfillment.Canonical) { evs = append(evs, carrierEvent(len(evs), c)) }
	moving := rapid.SampledFrom(movingCanonicals())
	var j journey
	if rapid.Bool().Draw(rt, "báo chờ lấy") {
		add(fulfillment.CanonAwaitingPickup)
	}
	if rapid.IntRange(0, 9).Draw(rt, "hủy trước lấy") == 0 {
		add(fulfillment.CanonCancelled)
	} else {
		add(fulfillment.CanonPickedUp)
		for range rapid.IntRange(0, 2).Draw(rt, "số lần thất bại") {
			add(fulfillment.CanonDeliveryFailed)
			if rapid.Bool().Draw(rt, "giao lại") {
				add(moving.Draw(rt, "giao lại bằng"))
			}
		}
		switch rapid.IntRange(0, 4).Draw(rt, "kết thúc") {
		case 0:
			add(fulfillment.CanonDelivered)
		case 1:
			add(fulfillment.CanonDelivered)
			add(rapid.SampledFrom([]fulfillment.Canonical{
				fulfillment.CanonReturning, fulfillment.CanonReturnedToShop,
			}).Draw(rt, "hãng sửa thành"))
		case 2:
			add(fulfillment.CanonReturning)
			if rapid.Bool().Draw(rt, "đã hoàn về shop") {
				add(fulfillment.CanonReturnedToShop)
			}
		case 3:
		default:
			add(fulfillment.CanonReturnedToShop)
		}
	}
	if rapid.Bool().Draw(rt, "hãng báo tiền đã thu") {
		j.collected = new(rapid.Int64Range(0, cod).Draw(rt, "đã thu"))
	}
	cuts := []int{0}
	for range rapid.IntRange(0, min(2, len(evs)-1)).Draw(rt, "số chỗ cắt") {
		cuts = append(cuts, rapid.IntRange(cuts[len(cuts)-1], len(evs)).Draw(rt, "chỗ cắt"))
	}
	cuts = append(cuts, len(evs))
	for i := 1; i < len(cuts); i++ {
		if cuts[i] > cuts[i-1] {
			j.lots = append(j.lots, evs[cuts[i-1]:cuts[i]])
		}
	}
	return j
}

// targetBalances là bảng đích theo trạng thái vận đơn (kế hoạch Task M3-33 targets.go, chưa có hành trình đã có
// tiền về): chưa lấy, hủy → 0; đang giao, giao thất bại → to_collect = X; giao xong → hãng giữ C (= tiền hãng báo đã
// thu, mặc định X); đang hoàn, đã hoàn → 0. COD 0 → 0.
func targetBalances(st fulfillment.Status, cod int64, collected *int64) cash.Balances {
	if cod == 0 {
		return cash.Balances{}
	}
	switch st {
	case fulfillment.StatusShipping, fulfillment.StatusDeliveryFailed:
		return cash.Balances{ToCollect: cod, SalesClearing: -cod}
	case fulfillment.StatusDelivered:
		c := cod
		if collected != nil {
			c = *collected
		}
		return cash.Balances{CarrierHolding: c, SalesClearing: -c}
	default:
		return cash.Balances{}
	}
}

// statusEvents đọc các sự kiện shipment.status_changed của vận đơn id theo Seq (chủ bảng, trong tenant); cột theo
// đúng thứ tự trường của events.StoredEvent.
func (e *codEnv) statusEvents(id uuid.UUID) []events.StoredEvent {
	e.t.Helper()
	var out []events.StoredEvent
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) (err error) {
		rows, _ := tx.Query(e.t.Context(), `SELECT id, tenant_id, aggregate_type, aggregate_id, seq, topic, payload
			FROM platform.domain_events WHERE aggregate_type = 'shipment' AND aggregate_id = $1 AND topic = $2
			ORDER BY seq`, id.String(), fulfillment.TopicShipmentStatusChanged)
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[events.StoredEvent])
		return err //nolint:wrapcheck // test
	})
	return out
}

var errCrash = errors.New("worker chết trước commit")

// propRun là một lượt của quy luật: vận đơn s, dịch vụ fulfillment và các sự kiện đã giao cho consumer.
type propRun struct {
	e    *codEnv
	rt   *rapid.T
	svc  *fulfillment.Service
	s    uuid.UUID
	done []events.StoredEvent
}

// applyLot áp một lô sự kiện hãng qua ApplyTracking (tiền hãng báo đã thu gắn vào lô có bước giao xong) rồi giao các
// sự kiện status_changed mới cho consumer.
func (p *propRun) applyLot(lot []fulfillment.CarrierEvent, collected *int64) {
	tr := fulfillment.Tracking{Events: lot}
	for _, ev := range lot {
		if ev.Canonical == fulfillment.CanonDelivered {
			tr.CODCollected = collected
		}
	}
	if err := p.e.app(func(tx pgx.Tx) error {
		_, err := p.svc.ApplyTracking(p.e.t.Context(), tx, fulfillment.ApplyInput{
			ShipmentID: p.s, Tracking: tr, Source: fulfillment.SrcCarrierAPI,
			Actor:    order.Actor{TenantID: p.e.tenant, Type: inventory.ActorCarrier},
			SyncedAt: journeyDay().Add(23 * time.Hour),
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		p.rt.Fatalf("ApplyTracking: %v", err)
	}
	for _, ev := range p.e.statusEvents(p.s)[len(p.done):] {
		p.deliver(ev)
	}
}

// deliver giao ev cho consumer như worker gặp sự cố: có thể chết trước commit (tx lùi), rồi giao 1–3 lần, đôi khi giao
// lại một sự kiện cũ hơn.
func (p *propRun) deliver(ev events.StoredEvent) {
	ctx := p.e.t.Context()
	if rapid.IntRange(0, 3).Draw(p.rt, "chết trước commit") == 0 {
		if err := p.e.app(func(tx pgx.Tx) error {
			if err := (cash.Consumer{}).Handle(ctx, ev, tx); err != nil {
				return err //nolint:wrapcheck // test
			}
			return errCrash
		}); !errors.Is(err, errCrash) {
			p.rt.Fatalf("lần chết, seq %d: %v", ev.Seq, err)
		}
	}
	for range 1 + rapid.IntRange(0, 2).Draw(p.rt, "giao lại") {
		if err := p.e.handle(ev); err != nil {
			p.rt.Fatalf("Handle seq %d: %v", ev.Seq, err)
		}
	}
	p.done = append(p.done, ev)
	if rapid.IntRange(0, 4).Draw(p.rt, "giao lại sự kiện cũ") == 0 {
		old := p.done[rapid.IntRange(0, len(p.done)-1).Draw(p.rt, "sự kiện cũ")]
		if err := p.e.handle(old); err != nil {
			p.rt.Fatalf("Handle lại seq %d: %v", old.Seq, err)
		}
	}
}

// check: số dư cuối khớp bảng đích theo trạng thái vận đơn; tổng mọi dòng sổ của tenant 0; mọi bút toán do hãng.
func (p *propRun) check(cod int64, collected *int64) {
	var st fulfillment.Status
	if err := p.e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(p.e.t.Context(), "SELECT status FROM fulfillment.shipments_v WHERE id = $1", p.s).Scan(&st)
	}); err != nil {
		p.rt.Fatal(err)
	}
	if got, want := p.e.bal(p.s), targetBalances(st, cod, collected); got != want {
		p.rt.Fatalf("vận đơn %s (COD %d, đã thu %v): số dư %+v, muốn %+v", st, cod, collected, got, want)
	}
	if got := p.e.total(); got != 0 {
		p.rt.Fatalf("tổng mọi dòng sổ = %d", got)
	}
	if n := p.e.count("SELECT count(*) FROM cash.cod_transfers WHERE actor_type <> 'carrier'"); n != 0 {
		p.rt.Fatalf("%d bút toán không do hãng", n)
	}
}

// Quy luật sổ COD theo hành trình thật (rapid, số lượt của gói ≥ 200): hành trình ngẫu nhiên (thất bại, giao lại, hoàn,
// giao một phần, hãng sửa "đã giao" thành hoàn, hủy) áp qua fulfillment.ApplyTracking theo lô; mỗi sự kiện
// status_changed giao cho consumer cash theo Seq, có lần chết trước commit (tx lùi), giao lại ngay và giao lại sự kiện
// cũ hơn → tổng mọi ngăn của tenant bằng 0, mọi bút toán do hãng, số dư cuối khớp bảng đích theo trạng thái vận đơn.
func TestPostings_PropertyApplyTrackingJourneys(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	svc := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: &testkit.RecordingBus{}, Events: events.NewRegistry(),
	}).Service
	rapid.Check(t, func(rt *rapid.T) {
		cod := rapid.OneOf(rapid.Just(int64(0)), rapid.Just(int64(v1COD)), rapid.Int64Range(1, 1<<45)).Draw(rt, "cod")
		p := &propRun{e: e, rt: rt, svc: svc, s: e.shipment(cod)}
		if err := e.app(func(tx pgx.Tx) error {
			_, err := svc.AttachTrackingCode(t.Context(), tx, p.s, "FK"+strings.ToUpper(uuid.NewString()[:12]), nil)
			return err //nolint:wrapcheck // test
		}); err != nil {
			rt.Fatalf("gắn mã hãng: %v", err)
		}
		j := drawJourney(rt, cod)
		for _, lot := range j.lots {
			p.applyLot(lot, j.collected)
		}
		p.check(cod, j.collected)
	})
}
