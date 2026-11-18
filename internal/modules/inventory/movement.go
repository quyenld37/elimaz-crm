package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Cách ghi dùng chung của mọi đường đổi tồn (kế hoạch Task M1-28; Task M1-29 dùng lại): applyDelta là một câu UPDATE
// có điều kiện cố định (store.ApplyDelta) rồi INSERT một dòng sổ, trong tx của bên gọi. ADR-46 điểm 5: không đọc số
// dư ra Go rồi ghi lại.

// bucket là một ngăn của số dư (cột của stock_balances, cột bucket của sổ).
type bucket string

const (
	bucketOnHand    bucket = "on_hand"
	bucketHeld      bucket = "held"
	bucketInTransit bucket = "in_transit"
	bucketDamaged   bucket = "damaged"
)

// cond là điều kiện cố định của câu đổi số dư, so với q = |delta|.
type cond string

const (
	condNone      cond = "none"
	condAvailable cond = "available" // on_hand − damaged − held − in_transit ≥ q
	condOnHand    cond = "on_hand"   // on_hand ≥ q (xuất hàng lỗi, Task M1-29)
	condDamaged   cond = "damaged"   // damaged ≥ q
	condHeld      cond = "held"      // held ≥ q
	condInTransit cond = "in_transit"
	condOnShelf   cond = "on_shelf" // on_hand − in_transit ≥ q (kiểm kho giảm, Task M1-35)
)

// doc là chứng từ gây ra thay đổi: doc_type, doc_id của sổ.
type doc struct {
	Type string // import | export | order | return | stocktake | adjust | damage
	ID   uuid.UUID
}

const docOrder = "order"

// balanceRow là dòng số dư vừa đổi: tenant, người bán, version mới.
type balanceRow struct {
	TenantID, SellerID, VariantID, WarehouseID uuid.UUID
	Version                                    int64
}

// applyDelta cộng delta vào ngăn b của số dư (variantID, warehouseID) khi c đúng, rồi ghi một dòng sổ (doc, step,
// actor). ok = false khi không dòng nào khớp (chưa có dòng số dư, hoặc điều kiện sai): không ghi gì.
func applyDelta(ctx context.Context, tx pgx.Tx, variantID, warehouseID uuid.UUID, b bucket, delta int32, c cond,
	d doc, step string, a Actor,
) (balanceRow, bool, error) {
	q := store.New(tx)
	row, err := q.ApplyDelta(ctx, store.ApplyDeltaParams{
		Bucket: string(b), Delta: delta, VariantID: variantID, WarehouseID: warehouseID, Cond: string(c), Q: abs(delta),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return balanceRow{}, false, nil
	}
	if err != nil {
		return balanceRow{}, false, fmt.Errorf("đổi số dư %s: %w", b, err)
	}
	if err := q.InsertLedger(ctx, store.InsertLedgerParams{
		TenantID: row.TenantID, VariantID: variantID, WarehouseID: warehouseID, Bucket: string(b), Delta: delta,
		DocType: d.Type, DocID: d.ID, Step: step, ActorType: a.Type,
		ActorID: pgtype.UUID{Bytes: a.ID, Valid: a.ID != uuid.Nil},
	}); err != nil {
		return balanceRow{}, false, fmt.Errorf("ghi sổ tồn: %w", err)
	}
	return balanceRow{
		TenantID: row.TenantID, SellerID: row.SellerID, VariantID: variantID, WarehouseID: warehouseID,
		Version: row.Version,
	}, true, nil
}

func abs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// Sự kiện và báo hiệu công bố (docs/views.md "Sự kiện công bố"); payload chỉ mang mã (A16).
const (
	TopicStockChanged = "stock.changed"

	signalTopic = "inventory.balance"
	permRead    = "inventory.read" // identity.Permission; inventory không nhập identity (04 mục 3)
)

var errNotWired = errors.New("inventory: Writer thiếu Events hoặc Queue")

// StockChanged là payload của stock.changed (aggregate "balance", mã "<variant>:<warehouse>", Seq = version của số dư).
type StockChanged struct {
	VariantIDs  []uuid.UUID `json:"variantIds"`
	WarehouseID uuid.UUID   `json:"warehouseId"`
	SellerID    uuid.UUID   `json:"sellerId"`
}

// changed ghi stock.changed và báo hiệu inventory.balance (quyền inventory.read theo kho) cho dòng số dư r.
// Writer thiếu Events hay Queue → lỗi (bên gọi lùi transaction: đổi tồn không bao giờ thiếu sự kiện).
func (w Writer) changed(ctx context.Context, tx pgx.Tx, r balanceRow) error {
	if w.Events == nil || w.Queue == nil {
		return errNotWired
	}
	key := r.VariantID.String() + ":" + r.WarehouseID.String()
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: r.TenantID, AggregateType: "balance", AggregateID: key, Seq: r.Version,
		Topic:   TopicStockChanged,
		Payload: StockChanged{VariantIDs: []uuid.UUID{r.VariantID}, WarehouseID: r.WarehouseID, SellerID: r.SellerID},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", TopicStockChanged, err)
	}
	if w.Realtime == nil {
		return nil
	}
	if err := w.Realtime.Publish(ctx, tx, realtime.Signal{
		TenantID: r.TenantID, Topic: signalTopic, Requires: permRead,
		Scope:  realtime.WarehouseScope(r.WarehouseID.String()),
		Entity: realtime.Entity{Type: "balance", ID: key},
	}); err != nil {
		return fmt.Errorf("báo hiệu %s: %w", signalTopic, err)
	}
	return nil
}
