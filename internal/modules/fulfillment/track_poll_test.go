package fulfillment_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// pollSchedule trả lịch tra bù của module.
func (e *trackEnv) pollSchedule() schedules.Schedule {
	e.t.Helper()
	for _, s := range e.mod.Schedules() {
		if s.Name == fulfillment.PollScheduleName {
			return s
		}
	}
	e.t.Fatalf("module không có lịch %s", fulfillment.PollScheduleName)
	return schedules.Schedule{}
}

// poll chạy lệnh của lịch tra bù cho kỳ slot với việc như Ticker ghi (payload {slot}, tenant A) — gọi thẳng Execute
// (worker sẽ nhận cả lệnh tra đang chờ), trả số việc tra mới ghi.
func (e *trackEnv) poll(slot time.Time) int {
	e.t.Helper()
	s := e.pollSchedule()
	if s.Every != 600*time.Second || !s.PerTenant {
		e.t.Fatalf("lịch tra bù = %v, theo tenant %v; muốn 600 s theo tenant", s.Every, s.PerTenant)
	}
	var h commands.Handler
	for _, c := range e.cmds {
		if c.Kind().Name == s.Kind.Name {
			h = c
		}
	}
	if h == nil {
		e.t.Fatalf("không có lệnh cho lịch %s", s.Name)
	}
	payload, err := json.Marshal(map[string]time.Time{"slot": slot.UTC()})
	if err != nil {
		e.t.Fatal(err)
	}
	out, err := h.Execute(e.t.Context(), queue.Job{ID: uuid.New(), Kind: s.Kind.Name, TenantID: &e.tenant, Payload: payload})
	var res struct {
		Enqueued int `json:"enqueued"`
	}
	if err != nil || out.Status != commands.StatusDone || json.Unmarshal(out.Result, &res) != nil {
		e.t.Fatalf("lệnh tra bù = %+v, %v", out, err)
	}
	return res.Enqueued
}

// clone chèn n vận đơn (mỗi cái một đơn chép từ đơn của tmpl) cùng tài khoản hãng, trạng thái status; vận đơn thứ g có
// last_synced_at = synced − g phút (synced nil → NULL), delivered_at = deliveredAt. Mã đơn, mã vận đơn, mã hãng theo
// tiền tố prefix (một chữ số) để nhiều lần gọi không trùng. Chạy trong ownerBulk (giới hạn thời gian riêng của bước nạp).
func (e *trackEnv) clone(tmpl fulfillment.Shipment, prefix, n int, status string, synced *time.Time, deliveredAt *time.Time) {
	e.t.Helper()
	e.ownerBulk(func(tx pgx.Tx) error {
		ctx := e.t.Context()
		cols := func(schema, table string, skip ...string) (string, error) {
			var c string
			err := tx.QueryRow(ctx, `SELECT string_agg(column_name, ', ' ORDER BY ordinal_position)
				FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND is_generated = 'NEVER'
				AND column_name <> ALL ($3)`, schema, table, skip).Scan(&c)
			return c, err //nolint:wrapcheck // test
		}
		// Đơn chép không có dòng hàng: mọi tổng tiền 0 (CHECK tổng dòng = subtotal).
		oc, err := cols("ord", "orders", "id", "code", "idempotency_key", "current_shipment_id", "subtotal",
			"discount_total", "shipping_fee_charged", "grand_total", "cod_amount")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO ord.orders (id, code, idempotency_key, subtotal,
				discount_total, shipping_fee_charged, grand_total, cod_amount, %[1]s)
			SELECT gen_random_uuid(), 'DH%[2]d' || lpad(g::text, 7, '0'), gen_random_uuid(), 0, 0, 0, 0, 0, %[1]s
			FROM ord.orders, generate_series(1, $2) g WHERE id = $1`, oc, prefix), tmpl.OrderID, n); err != nil {
			return err //nolint:wrapcheck // test
		}
		sc, err := cols("fulfillment", "shipments", "id", "code", "order_id", "tracking_code", "status",
			"last_synced_at", "delivered_at", "picked_up_at", "last_carrier_event_at")
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO fulfillment.shipments (id, code, order_id, tracking_code, status,
				last_synced_at, delivered_at, %[1]s)
			SELECT gen_random_uuid(), 'E%[2]d' || lpad(g::text, 11, '0'), o.id, 'PT%[2]d' || g, $3,
				$4::timestamptz - g * interval '1 minute', $5, %[3]s
			FROM fulfillment.shipments s, generate_series(1, $2) g
			JOIN ord.orders o ON o.code = 'DH%[2]d' || lpad(g::text, 7, '0')
			WHERE s.id = $1`, sc, prefix, prefixed("s.", sc)), tmpl.ID, n, status, synced, deliveredAt)
		return err //nolint:wrapcheck // test
	})
}

// prefixed thêm tiền tố bảng p vào từng cột của danh sách cols ("a, b" → "s.a, s.b").
func prefixed(p, cols string) string {
	out := ""
	for i, c := range splitCols(cols) {
		if i > 0 {
			out += ", "
		}
		out += p + c
	}
	return out
}

func splitCols(cols string) []string {
	var out []string
	cur := ""
	for _, r := range cols {
		switch r {
		case ',':
			out, cur = append(out, cur), ""
		case ' ':
		default:
			cur += string(r)
		}
	}
	return append(out, cur)
}

// VC05: hãng đổi V1 sang PickedUp; V1 last_synced_at cũ 3 giờ → lượt tra bù ghi một việc tra, chạy → V1 shipping, tồn
// in_transit, last_synced_at = giờ chạy (đồng hồ của Service).
func TestPoll_PickedUp(t *testing.T) {
	e := newTrackEnv(t)
	_, v, s := e.shipped(2)
	e.push(s.TrackingCode, fake.RawPickedUp)
	e.setSynced(s.ID, e.clock.Now().Add(-3*time.Hour))
	e.clock.add(time.Minute)
	if n := e.poll(e.clock.Now()); n != 1 {
		t.Fatalf("việc tra = %d, muốn 1", n)
	}
	e.drain()
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusShipping {
		t.Fatalf("vận đơn = %s, muốn shipping", got.Status)
	}
	e.stockIs(v, [3]int32{5, 0, 2})
	if at, code := e.syncState(s.ID); at == nil || !at.Equal(e.clock.Now()) || code != "" {
		t.Fatalf("last_synced_at = %v %q, muốn %v", at, code, e.clock.Now())
	}
	// Vừa đồng bộ (mới hơn PollMinutes[shipping] = 60 phút) → lượt sau không chọn.
	e.clock.add(10 * time.Minute)
	if n := e.poll(e.clock.Now()); n != 0 {
		t.Fatalf("lượt sau = %d việc, muốn 0", n)
	}
}

// 450 vận đơn mở của một tài khoản, tất cả cũ → lượt đầu đúng 200 việc, ưu tiên chưa đồng bộ rồi cũ nhất; lượt thứ hai
// cùng kỳ → 0 việc mới; vận đơn delivered 8 ngày trước, vận đơn đã đồng bộ gần đây không được chọn. Gauge
// shipments_unsynced_over_threshold của tenant = 450.
func TestPoll_BudgetPerAccount(t *testing.T) {
	e := newTrackEnv(t)
	_, _, tmpl := e.shipped(1)
	old := e.clock.Now().Add(-3 * time.Hour)
	e.setSynced(tmpl.ID, e.clock.Now()) // vừa đồng bộ: không chọn
	e.clone(tmpl, 1, 1, "awaiting_pickup", nil, nil)
	e.clone(tmpl, 2, 449, "shipping", &old, nil)
	delivered := e.clock.Now().Add(-8 * 24 * time.Hour)
	e.clone(tmpl, 3, 1, "delivered", &old, &delivered)
	slot := e.clock.Now().Truncate(10 * time.Minute)
	if n := e.poll(slot); n != 200 {
		t.Fatalf("lượt đầu = %d việc, muốn 200", n)
	}
	if n := e.poll(slot); n != 0 {
		t.Fatalf("lượt hai cùng kỳ = %d việc, muốn 0", n)
	}
	picked := `SELECT count(*) FROM platform.jobs j JOIN fulfillment.shipments s ON s.id = (j.payload->>'shipmentId')::uuid
		WHERE j.kind = 'fulfillment.refresh_status' AND j.dedupe_key LIKE 'poll:%'`
	if n := e.count(picked); n != 200 {
		t.Fatalf("việc tra bù = %d, muốn 200", n)
	}
	if n := e.count(picked + ` AND s.code = 'E100000000001'`); n != 1 {
		t.Fatal("vận đơn chưa đồng bộ lần nào phải được chọn")
	}
	if n := e.count(picked+` AND (s.status = 'delivered' OR s.id = $1)`, tmpl.ID); n != 0 {
		t.Fatal("vận đơn delivered, vận đơn vừa đồng bộ không được chọn")
	}
	// 199 vận đơn shipping cũ nhất: g = 449 … 251 (last_synced_at = old − g phút).
	if n := e.count(picked + ` AND s.status = 'shipping' AND s.code < 'E200000000251'`); n != 0 {
		t.Fatalf("%d vận đơn mới hơn được chọn trước vận đơn cũ hơn", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'
		AND dedupe_key LIKE 'poll:%:' || $1`, slot.UTC().Format(time.RFC3339)); n != 200 {
		t.Fatalf("khóa chống trùng phải theo kỳ: %d", n)
	}
	found := false
	for _, r := range e.metrics.Drain() {
		if r.Name == "shipments_unsynced_over_threshold" && r.Label == e.tenant.String() {
			found = true
			if r.Sum != 450 {
				t.Fatalf("gauge = %v, muốn 450", r.Sum)
			}
		}
	}
	if !found {
		t.Fatal("thiếu gauge shipments_unsynced_over_threshold")
	}
}

// Tài khoản không active hay mode manual → lịch không chọn.
func TestPoll_SkipsUnavailableAccounts(t *testing.T) {
	e := newTrackEnv(t)
	_, _, s := e.shipped(1)
	e.setSynced(s.ID, e.clock.Now().Add(-5*time.Hour))
	e.ownerExec(`UPDATE fulfillment.carrier_accounts SET status = 'paused' WHERE id = $1`, e.account)
	if n := e.poll(e.clock.Now()); n != 0 {
		t.Fatalf("tài khoản paused: %d việc", n)
	}
	e.ownerExec(`UPDATE fulfillment.carrier_accounts SET status = 'active', mode = 'manual' WHERE id = $1`, e.account)
	if n := e.poll(e.clock.Now()); n != 0 {
		t.Fatalf("tài khoản manual: %d việc", n)
	}
}

func mustRegistry(t *testing.T, a ...fulfillment.CarrierAdapter) *fulfillment.Registry {
	t.Helper()
	r, err := fulfillment.NewRegistry(a...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
