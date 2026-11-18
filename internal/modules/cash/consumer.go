package cash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash/internal/store"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ConsumerName là tên consumer của sổ COD (việc event.cash); đổi tên làm mất tiến độ.
const ConsumerName = "cash"

// Consumer ghi sổ COD theo shipment.status_changed (events.Consumer; cmd/crm đăng ký). Vận đơn COD X > 0:
//
//   - shipping → bút toán expected: to_collect +X, sales_clearing −X.
//   - delivered → expected (nếu chưa có: hãng bỏ qua tín hiệu lấy hàng) rồi collected: to_collect −X,
//     carrier_holding +X.
//
// Trạng thái khác, hay COD 0, không ghi gì. X đọc qua fulfillment.shipments_v. Mỗi loại chỉ một bút toán cho một vận
// đơn: đã có thì bỏ qua (sự kiện giao lại hay sự kiện khác cùng trạng thái); khóa chống trùng "evt:<eventID>:<loại>".
// Việc event.cash chạy thứ tự nghiêm theo vận đơn nên hai sự kiện của một vận đơn không chạy cùng lúc. Payload hỏng
// hay vận đơn không có → queue.ErrPermanent.
type Consumer struct{}

// Name trả ConsumerName.
func (Consumer) Name() string { return ConsumerName }

// Topics trả shipment.status_changed.
func (Consumer) Topics() []string { return []string{fulfillment.TopicShipmentStatusChanged} }

// Handle ghi bút toán của sự kiện e trong tx của worker.
func (Consumer) Handle(ctx context.Context, e events.StoredEvent, tx pgx.Tx) error {
	var p fulfillment.StatusChanged
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.ShipmentID == uuid.Nil {
		return fmt.Errorf("sự kiện %s: payload không hợp lệ: %w", e.ID, queue.ErrPermanent)
	}
	var want []string
	switch p.Status {
	case fulfillment.StatusShipping:
		want = []string{KindExpected}
	case fulfillment.StatusDelivered:
		want = []string{KindExpected, KindCollected}
	default:
		return nil
	}
	q := store.New(tx)
	cod, err := q.ShipmentCOD(ctx, p.ShipmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("vận đơn %s không có: %w", p.ShipmentID, queue.ErrPermanent)
	}
	if err != nil {
		return fmt.Errorf("đọc COD của vận đơn: %w", err)
	}
	if cod == 0 {
		return nil
	}
	done, err := q.TransferKinds(ctx, p.ShipmentID)
	if err != nil {
		return fmt.Errorf("đọc bút toán của vận đơn: %w", err)
	}
	for _, kind := range want {
		if slices.Contains(done, kind) {
			continue
		}
		t := Transfer{
			ShipmentID: p.ShipmentID, Kind: kind, IdempotencyKey: "evt:" + e.ID.String() + ":" + kind,
			Actor: Actor{Type: ActorCarrier}, Entries: entries(kind, cod),
		}
		if _, err := Post(ctx, tx, t); err != nil {
			return fmt.Errorf("ghi bút toán %s: %w", kind, err)
		}
	}
	return nil
}

// entries là các vế của bút toán kind (expected hay collected) cho COD x.
func entries(kind string, x int64) []Entry {
	if kind == KindExpected {
		return []Entry{{Account: AccountToCollect, Amount: x}, {Account: AccountSalesClearing, Amount: -x}}
	}
	return []Entry{{Account: AccountToCollect, Amount: -x}, {Account: AccountCarrierHolding, Amount: x}}
}
