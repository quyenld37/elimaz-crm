package catalog

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Sự kiện công bố của catalog (docs/views.md mục "Sự kiện công bố"); payload chỉ mang mã và số (A16).
const (
	TopicProductUpdated = "product.updated"
	TopicPriceChanged   = "price.changed"

	aggregateProduct = "product"
	aggregateVariant = "variant"
	signalTopic      = "catalog.product"
	permRead         = identity.Permission("catalog.read")
)

// ProductUpdated là payload của product.updated (aggregate product, Seq = version mới của mẫu).
type ProductUpdated struct {
	ProductID uuid.UUID `json:"productId"`
	SellerID  uuid.UUID `json:"sellerId"`
	Version   int64     `json:"version"`
}

// PriceChanged là payload của price.changed (aggregate variant, Seq = version mới của biến thể).
type PriceChanged struct {
	ProductID  uuid.UUID   `json:"productId"`
	SellerID   uuid.UUID   `json:"sellerId"`
	VariantIDs []uuid.UUID `json:"variantIds"`
	Price      int64       `json:"price"`
}

// productChanged ghi product.updated và báo hiệu catalog.product cho mẫu vừa đổi tới version.
func (w Writer) productChanged(ctx context.Context, tx pgx.Tx, tenantID, sellerID, productID uuid.UUID, version int64) error {
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: tenantID, AggregateType: aggregateProduct, AggregateID: productID.String(), Seq: version,
		Topic: TopicProductUpdated, Payload: ProductUpdated{ProductID: productID, SellerID: sellerID, Version: version},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", TopicProductUpdated, err)
	}
	if w.Realtime == nil {
		return nil
	}
	if err := w.Realtime.Publish(ctx, tx, realtime.Signal{
		TenantID: tenantID, Topic: signalTopic, Requires: permRead,
		Entity: realtime.Entity{Type: aggregateProduct, ID: productID.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu %s: %w", signalTopic, err)
	}
	return nil
}

// priceChanged ghi price.changed của một biến thể vừa đổi giá tới version.
func (w Writer) priceChanged(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, v Variant) error {
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: tenantID, AggregateType: aggregateVariant, AggregateID: v.ID.String(), Seq: v.Version,
		Topic:   TopicPriceChanged,
		Payload: PriceChanged{ProductID: v.ProductID, SellerID: v.SellerID, VariantIDs: []uuid.UUID{v.ID}, Price: v.Price},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", TopicPriceChanged, err)
	}
	return nil
}

// audit ghi một dòng audit của nhân viên actor (không có dữ liệu cá nhân: chỉ mã, trạng thái, giá).
func (w Writer) audit(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, action, entityType string, id uuid.UUID,
	before, after map[string]any,
) error {
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: actor.TenantID, ActorType: "staff", ActorID: actor.UserID.String(), Action: action,
		EntityType: entityType, EntityID: id.String(), Before: before, After: after, RequestID: w.RequestID,
	}); err != nil {
		return fmt.Errorf("ghi audit %s: %w", action, err)
	}
	return nil
}

func productAudit(p Product) map[string]any {
	return map[string]any{
		"code": p.Code, "name": p.Name, "status": string(p.Status), "version": p.Version,
		"descriptionLength": len([]rune(p.Description)), "articleBlocks": len(p.Article), "variants": len(p.Variants),
	}
}

func variantAudit(v Variant) map[string]any {
	m := map[string]any{
		"sku": v.SKU, "barcode": v.Barcode, "color": v.Color, "colorHex": v.ColorHex, "size": v.Size,
		"price": v.Price, "costPrice": nil, "status": v.Status, "version": v.Version,
	}
	if v.CostPrice != nil {
		m["costPrice"] = *v.CostPrice
	}
	if v.WeightGrams != nil {
		m["weightGrams"] = *v.WeightGrams
	}
	return m
}
