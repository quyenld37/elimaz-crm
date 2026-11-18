package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Cài đặt đơn và nguồn đơn (kế hoạch M2, Task M2-15). Đọc cần order.read, sửa cần order.settings.manage (bảng route).
// Audit ghi ở đây, cùng tx (order không nhập identity).

// GetOrderSettings (GET /api/v1/order-settings, order.read).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetOrderSettings(ctx context.Context) (gen.GetOrderSettingsRes, error) {
	var s order.Settings
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		s, err = order.LoadSettings(ctx, tx)
		return err //nolint:wrapcheck // order đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc cài đặt đơn: %w", err)
	}
	return orderSettingsOut(s), nil
}

// UpdateOrderSettings (PATCH /api/v1/order-settings, order.settings.manage): sửa khi version khớp; ngoài khoảng → 422
// setting_out_of_range kèm field; version cũ → 409.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateOrderSettings(ctx context.Context, req *gen.UpdateOrderSettingsRequest, _ gen.UpdateOrderSettingsParams) (gen.UpdateOrderSettingsRes, error) {
	p := order.SettingsPatch{
		DraftTTLMinutes:             optInt32(req.DraftTtlMinutes),
		DuplicateWindowMinutes:      optInt32(req.DuplicateWindowMinutes),
		DiscountApprovalThresholdBP: optInt32(req.DiscountApprovalThresholdBp),
		ReturnWarningDays:           optInt32(req.ReturnWarningDays),
	}
	out, err := h.orderSettingsWrite(ctx, http.StatusOK, func(tx pgx.Tx, w order.Writer, u identity.AuthUser) (any, error) {
		ch, err := w.UpdateSettings(ctx, tx, req.Version, p, order.Actor{TenantID: u.TenantID, UserID: u.UserID})
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của order
		}
		return orderSettingsOut(ch.After), h.audit(ctx, tx, u, "order.settings.update", "order.settings", u.TenantID,
			ch.Before.AuditFields(), ch.After.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.OrderSettings), nil //nolint:forcetypeassert // fn trên trả *gen.OrderSettings
}

// ListTrafficSources (GET /api/v1/traffic-sources, order.read): mọi nguồn (kể cả đã tắt) theo vị trí.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListTrafficSources(ctx context.Context) (gen.ListTrafficSourcesRes, error) {
	var ss []order.Source
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		ss, err = order.ListSources(ctx, tx)
		return err //nolint:wrapcheck // order đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc nguồn đơn: %w", err)
	}
	out := &gen.TrafficSourceList{Items: make([]gen.TrafficSource, 0, len(ss))}
	for _, s := range ss {
		out.Items = append(out.Items, *trafficSourceOut(s))
	}
	return out, nil
}

// CreateTrafficSource (POST /api/v1/traffic-sources, order.settings.manage) → 201; tên trùng → 409 already_exists.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateTrafficSource(ctx context.Context, req *gen.CreateTrafficSourceRequest, _ gen.CreateTrafficSourceParams) (gen.CreateTrafficSourceRes, error) {
	out, err := h.orderSettingsWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w order.Writer, u identity.AuthUser) (any, error) {
		s, err := w.CreateSource(ctx, tx, req.Name)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của order
		}
		return trafficSourceOut(s), h.audit(ctx, tx, u, "order.traffic_source.create", "order.traffic_source", s.ID, nil, s.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.TrafficSource), nil //nolint:forcetypeassert // fn trên trả *gen.TrafficSource
}

// UpdateTrafficSource (PATCH /api/v1/traffic-sources/{sourceId}, order.settings.manage): đổi tên, bật tắt, vị trí khi
// version khớp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateTrafficSource(ctx context.Context, req *gen.UpdateTrafficSourceRequest, params gen.UpdateTrafficSourceParams) (gen.UpdateTrafficSourceRes, error) {
	p := order.SourcePatch{Name: optString(req.Name.Set, req.Name.Value)}
	if v, ok := req.Active.Get(); ok {
		p.Active = &v
	}
	if v, ok := req.Position.Get(); ok {
		p.Position = &v
	}
	out, err := h.orderSettingsWrite(ctx, http.StatusOK, func(tx pgx.Tx, w order.Writer, u identity.AuthUser) (any, error) {
		ch, err := w.UpdateSource(ctx, tx, params.SourceId, req.Version, p)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của order
		}
		return trafficSourceOut(ch.After), h.audit(ctx, tx, u, "order.traffic_source.update", "order.traffic_source",
			ch.After.ID, ch.Before.AuditFields(), ch.After.AuditFields())
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.TrafficSource), nil //nolint:forcetypeassert // fn trên trả *gen.TrafficSource
}

// orderSettingsWrite chạy change trong refusableWrite: lỗi nghiệp vụ của order là câu trả lời không đổi gì (không lưu
// vào khóa chống trùng).
func (h *handler) orderSettingsWrite(ctx context.Context, status int,
	change func(tx pgx.Tx, w order.Writer, u identity.AuthUser) (any, error),
) (any, error) {
	return h.refusableWrite(ctx, status, isOrderSettingsRefusal, "ghi cài đặt đơn", func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		return change(tx, order.Writer{}, u)
	})
}

func isOrderSettingsRefusal(err error) bool {
	_, _, ok := orderError(err)
	return ok
}

func optInt32(v gen.OptInt32) *int {
	if x, ok := v.Get(); ok {
		n := int(x)
		return &n
	}
	return nil
}

func orderSettingsOut(s order.Settings) *gen.OrderSettings {
	//nolint:gosec // mọi giá trị trong khoảng CHECK của bảng (≤ 10080)
	return &gen.OrderSettings{
		DraftTtlMinutes: int32(s.DraftTTL / time.Minute), DuplicateWindowMinutes: int32(s.DuplicateWindow / time.Minute),
		DiscountApprovalThresholdBp: int32(s.DiscountApprovalThresholdBP), ReturnWarningDays: int32(s.ReturnWarningDays),
		Version: s.Version,
	}
}

func trafficSourceOut(s order.Source) *gen.TrafficSource {
	out := &gen.TrafficSource{ID: s.ID, Name: s.Name, Active: s.Active, Position: s.Position, Version: s.Version}
	if s.Key != "" {
		out.Key = gen.NewOptString(s.Key)
	}
	return out
}
