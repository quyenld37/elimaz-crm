// Package codtest dựng dữ liệu thử của sổ COD cho test của các gói con cash (điều chỉnh, đối chiếu; kế hoạch M3, Task
// M3-33): database thử, tenant, vận đơn thật ở một trạng thái với sổ ghi bằng consumer cash, bảng kê đã chốt ghi
// remitted. Chỉ test nhập gói này.
package codtest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// Env là database thử, tenant A (shiptest.Shop), kế toán User, module cash với đồng hồ giả Now (mặc định
// 2026-11-20 19:45 UTC, ngay sau kỳ đối chiếu 19:30 UTC).
type Env struct {
	T       testing.TB
	Staff   *testkit.Staff
	Tenant  uuid.UUID
	User    uuid.UUID
	Shop    *shiptest.Shop
	Variant uuid.UUID
	Mod     *cash.Module
	Now     time.Time
	seq     int64
}

// New dựng Env.
func New(t testing.TB) *Env {
	t.Helper()
	e := &Env{T: t, Staff: testkit.OpenStaff(t), Now: time.Date(2026, 11, 20, 19, 45, 0, 0, time.UTC)}
	e.Tenant = e.Staff.Tenant(t, "A")
	e.User = e.Staff.User(t, e.Tenant, "ketoan@elimaz.test", "active")
	e.Staff.Exec(t, e.Tenant, func(tx pgx.Tx) error {
		e.Shop = shiptest.NewShop(t, tx, e.Tenant)
		e.Variant = e.Shop.Variant(t, tx, e.Shop.Seller, 250000, 1000)
		return nil
	})
	e.Mod = cash.New(cash.Deps{
		DB: e.Staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Now: func() time.Time { return e.Now },
	})
	return e
}

// App chạy fn bằng crm_app trong tenant.
func (e *Env) App(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.T.Context(), e.Staff.App, e.Tenant, fn) //nolint:wrapcheck // test
}

// MustApp như App, lỗi thì dừng test.
func (e *Env) MustApp(fn func(tx pgx.Tx) error) {
	e.T.Helper()
	if err := e.App(fn); err != nil {
		e.T.Fatal(err)
	}
}

// Ship mô tả vận đơn thử: mã hãng, COD, trạng thái, các bước tác động của sự kiện (mỗi bước một danh sách, như
// StatusChanged.Effects), số hãng báo đã thu (nil: COD), giao một phần, đổi trạng thái lúc nào (rỗng: một ngày trước
// Now).
type Ship struct {
	Tracking     string
	COD          int64
	Status       fulfillment.Status
	Steps        [][]string
	CODCollected *int64
	Partial      bool
	StatusAt     time.Time
}

// Shipment tạo đơn, vận đơn thật (crm_app), đặt (chủ bảng) mã hãng, COD, trạng thái, COD đã thu, giao một phần, lúc đổi
// trạng thái, rồi cho consumer cash ghi sổ theo từng bước (seq 3, 4, …). Trả mã vận đơn.
func (e *Env) Shipment(s Ship) uuid.UUID {
	e.T.Helper()
	if s.StatusAt.IsZero() {
		s.StatusAt = e.Now.Add(-24 * time.Hour)
	}
	var id uuid.UUID
	e.MustApp(func(tx pgx.Tx) error {
		o, err := e.Shop.Order(e.T.Context(), tx, e.Shop.Seller, e.User, 0, order.ItemInput{VariantID: e.Variant, Qty: 1})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		sh, err := e.Shop.Shipment(e.T.Context(), tx, e.Shop.Seller, o.ID, e.User)
		id = sh.ID
		return err //nolint:wrapcheck // test
	})
	e.Staff.Exec(e.T, e.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.T.Context(), `UPDATE fulfillment.shipments SET tracking_code = $2, cod_amount = $3, status = $4,
			cod_collected_amount = $5, partial = $6, last_status_at = $7,
			cancel_kind = CASE WHEN $4 = 'cancelled' THEN 'manual' END WHERE id = $1`,
			id, s.Tracking, s.COD, string(s.Status), s.CODCollected, s.Partial, s.StatusAt)
		return err //nolint:wrapcheck // test
	})
	for _, effects := range s.Steps {
		e.Event(id, s, effects)
	}
	return id
}

// Event giao cho consumer cash một sự kiện shipment.status_changed của vận đơn id với các tác động effects.
func (e *Env) Event(id uuid.UUID, s Ship, effects []string) {
	e.T.Helper()
	e.seq++
	p, err := json.Marshal(fulfillment.StatusChanged{
		ShipmentID: id, Status: s.Status, CODAmount: s.COD, Effects: effects, CODCollected: s.CODCollected,
		Partial: s.Partial,
	})
	if err != nil {
		e.T.Fatal(err)
	}
	ev := events.StoredEvent{
		ID: uuid.New(), TenantID: e.Tenant, AggregateType: "shipment", AggregateID: id.String(), Seq: 2 + e.seq,
		Topic: fulfillment.TopicShipmentStatusChanged, Payload: p,
	}
	e.MustApp(func(tx pgx.Tx) error { return cash.Consumer{}.Handle(e.T.Context(), ev, tx) })
}

// Remit ghi (chủ bảng) bảng kê đã chốt lúc confirmedAt có một dòng của vận đơn id (COD cod, phí fee), rồi (crm_app)
// bút toán remitted của dòng: carrier_holding −cod, shop_received +(cod − fee), carrier_fee +fee. Trả mã bút toán.
func (e *Env) Remit(id uuid.UUID, tracking string, cod, fee int64, confirmedAt time.Time) uuid.UUID {
	e.T.Helper()
	line := uuid.Must(uuid.NewV7())
	e.Staff.Exec(e.T, e.Tenant, func(tx pgx.Tx) error {
		var st uuid.UUID
		if err := tx.QueryRow(e.T.Context(), `INSERT INTO cash.cod_statements (tenant_id, carrier_key, carrier_account_id,
			file_id, file_sha256, format, status, imported_by, confirmed_by, confirmed_at, line_count)
			SELECT $1, carrier_key, id, gen_random_uuid(), encode(sha256(convert_to($4::text, 'UTF8')), 'hex'),
			       'elimaz_v1', 'confirmed', $3, $3, $5, 1
			FROM fulfillment.carrier_accounts WHERE id = $2
			RETURNING id`, e.Tenant, e.Shop.Account, e.User, line.String(), confirmedAt).Scan(&st); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(e.T.Context(), `INSERT INTO cash.cod_statement_lines (id, tenant_id, statement_id, line_no,
			tracking_code, cod_amount, fee_amount, net_amount, match_status, shipment_id)
			VALUES ($1, $2, $3, 1, $4, $5, $6, $7, 'matched', $8)`, line, e.Tenant, st, tracking, cod, fee, cod-fee, id)
		return err //nolint:wrapcheck // test
	})
	var tid uuid.UUID
	e.MustApp(func(tx pgx.Tx) error {
		var err error
		tid, _, err = cash.PostWithID(e.T.Context(), tx, cash.Transfer{
			ShipmentID: id, Kind: cash.KindRemitted, IdempotencyKey: "remit:" + line.String(),
			Actor: cash.Actor{Type: cash.ActorStaff, ID: e.User}, StatementLineID: &line,
			Entries: []cash.Entry{
				{Account: cash.AccountCarrierHolding, Amount: -cod},
				{Account: cash.AccountShopReceived, Amount: cod - fee},
				{Account: cash.AccountCarrierFee, Amount: fee},
			},
		})
		return err //nolint:wrapcheck // test
	})
	return tid
}

// Bal là số dư {to_collect, carrier_holding, shop_received, carrier_fee, sales_clearing} của vận đơn id.
func (e *Env) Bal(id uuid.UUID) [5]int64 {
	e.T.Helper()
	var b cash.Balances
	e.MustApp(func(tx pgx.Tx) error {
		var err error
		b, err = cash.BalancesFor(e.T.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return [5]int64{b.ToCollect, b.CarrierHolding, b.ShopReceived, b.CarrierFee, b.SalesClearing}
}

// Count chạy câu đếm q (chủ bảng) trong tenant.
func (e *Env) Count(q string, args ...any) int {
	e.T.Helper()
	return e.Staff.CountIn(e.T, e.Tenant, q, args...)
}

// TransferOf trả mã bút toán loại kind gần nhất của vận đơn id.
func (e *Env) TransferOf(id uuid.UUID, kind string) uuid.UUID {
	e.T.Helper()
	var tid uuid.UUID
	e.MustApp(func(tx pgx.Tx) error {
		return tx.QueryRow(e.T.Context(), `SELECT id FROM cash.cod_transfers WHERE shipment_id = $1 AND kind = $2
			ORDER BY id DESC LIMIT 1`, id, kind).Scan(&tid)
	})
	return tid
}
