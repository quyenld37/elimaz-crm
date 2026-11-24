package webcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Chủ đề chưa có hằng xuất ở module sở hữu (flash sale ở M5-17); task tạo sự kiện thì thay bằng hằng của module.
const (
	topicFlashOpened    = "flash_sale.opened"
	topicFlashClosed    = "flash_sale.closed"
	topicFlashChanged   = "flash_sale.changed"
	topicOrderCancelled = "order." + order.StatusCancelled
	consumerName        = "webcache"
)

// errBadPayload: payload của sự kiện thiếu mã cần cho bộ đệm (ghi log, không báo hiệu).
var errBadPayload = errors.New("webcache: payload thiếu mã")

// Consumer đổi sự kiện thành báo hiệu hệ thống TopicInvalidate (events.Consumer, chạy ở worker). Không gọi Valkey.
type Consumer struct {
	bus realtime.Bus
	log *slog.Logger
}

// NewConsumer dựng Consumer phát báo hiệu qua bus; log nil thì slog.Default.
func NewConsumer(bus realtime.Bus, log *slog.Logger) *Consumer {
	if log == nil {
		log = slog.Default()
	}
	return &Consumer{bus: bus, log: log}
}

// Name là tên cố định của consumer.
func (c *Consumer) Name() string { return consumerName }

// Topics là các chủ đề consumer nghe.
func (c *Consumer) Topics() []string {
	return []string{
		catalog.TopicProductUpdated, catalog.TopicPriceChanged, inventory.TopicStockChanged,
		topicFlashOpened, topicFlashClosed, topicFlashChanged, string(topicOrderCancelled), fulfillment.TopicWebCarrierSettingsChanged,
		promotion.TopicWebCampaignChanged, order.TopicWebIntakeChanged,
	}
}

// Handle phát một báo hiệu hệ thống cho mỗi đối tượng của sự kiện, trong tx (chỉ tới api khi tx commit). Payload thiếu
// mã: ghi log, không báo hiệu, trả nil (chạy lại không sửa được payload; bộ đệm tự lành theo hạn sống, QĐ-185-4).
func (c *Consumer) Handle(ctx context.Context, e events.StoredEvent, tx pgx.Tx) error {
	ents, err := entitiesOf(e)
	if errors.Is(err, errBadPayload) {
		c.log.LogAttrs(ctx, slog.LevelError, "webcache: bỏ qua sự kiện có payload sai",
			slog.String("topic", e.Topic), slog.String("event_id", e.ID.String()), logging.Err(err))
		return nil
	}
	if err != nil {
		return err
	}
	for _, ent := range ents {
		if err := c.bus.Publish(ctx, tx, realtime.Signal{
			TenantID: e.TenantID, Topic: TopicInvalidate, System: true, Entity: ent,
		}); err != nil {
			return fmt.Errorf("webcache: báo hiệu %s %s: %w", ent.Type, ent.ID, err)
		}
	}
	return nil
}

// entitiesOf đọc các đối tượng cần làm mới từ sự kiện (payload của M1 bản 2, A4).
func entitiesOf(e events.StoredEvent) ([]realtime.Entity, error) {
	switch e.Topic {
	case catalog.TopicProductUpdated:
		var p catalog.ProductUpdated
		if err := decode(e.Payload, &p); err != nil {
			return nil, err
		}
		if p.ProductID == uuid.Nil {
			return nil, fmt.Errorf("%w: productId", errBadPayload)
		}
		return []realtime.Entity{{Type: EntityProduct, ID: p.ProductID.String()}}, nil
	case catalog.TopicPriceChanged:
		var p catalog.PriceChanged
		if err := decode(e.Payload, &p); err != nil {
			return nil, err
		}
		if p.ProductID == uuid.Nil {
			return nil, fmt.Errorf("%w: productId", errBadPayload)
		}
		vs, err := variants(p.VariantIDs)
		if err != nil {
			return nil, err
		}
		return append([]realtime.Entity{{Type: EntityProduct, ID: p.ProductID.String()}}, vs...), nil
	case inventory.TopicStockChanged:
		var p inventory.StockChanged
		if err := decode(e.Payload, &p); err != nil {
			return nil, err
		}
		return variants(p.VariantIDs)
	case topicFlashOpened, topicFlashClosed, topicFlashChanged:
		return []realtime.Entity{{Type: EntityFlashSale, ID: e.AggregateID}}, nil
	case fulfillment.TopicWebCarrierSettingsChanged:
		return []realtime.Entity{{Type: EntityCarrierSettings, ID: e.AggregateID}}, nil
	case promotion.TopicWebCampaignChanged:
		// Danh sách, trang chiến dịch của website (SpaceCampaigns, làm mới cả vùng). Mẫu không mang giá chiến dịch
		// (website tự tính từ danh sách) nên không xóa khóa sản phẩm.
		return []realtime.Entity{{Type: EntityWebCampaign, ID: e.AggregateID}}, nil
	case order.TopicWebIntakeChanged:
		// Trạng thái nhận đơn web của shop (SpaceWebIntake, một khóa mỗi tenant: làm mới cả vùng).
		return []realtime.Entity{{Type: EntityWebIntake, ID: e.AggregateID}}, nil
	}
	// order.cancelled: chỉ đơn có dòng flash sale tạo báo hiệu flash_item, tra bằng order.FlashItemOf của M5-19; trước
	// M5-19 bỏ qua. Chủ đề khác: không nghe.
	return nil, nil
}

// variants trả một đối tượng cho mỗi biến thể (bỏ trùng); mảng rỗng hay thiếu là payload sai.
func variants(ids []uuid.UUID) ([]realtime.Entity, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: variantIds", errBadPayload)
	}
	out := make([]realtime.Entity, 0, len(ids))
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, fmt.Errorf("%w: variantIds có mã rỗng", errBadPayload)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, realtime.Entity{Type: EntityVariant, ID: id.String()})
		}
	}
	return out, nil
}

func decode(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%w: %w", errBadPayload, err)
	}
	return nil
}
