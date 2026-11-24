package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/httpapi/web"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Hãng giao hàng trên website (kế hoạch M5, Task 198). Nhân viên: đọc cần fulfillment.carrier_accounts.read, lưu cần
// fulfillment.carrier_accounts.manage (bảng route; quyền của M3, A9). Website (service_key, web.shipping.quote): danh
// sách hãng, báo phí, ước thời gian giao — mọi việc ở web.Shipping. Audit ghi ở đây, cùng tx (fulfillment không nhập
// identity).

// GetWebCarrierSettings (GET /api/v1/web-carrier-settings).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetWebCarrierSettings(ctx context.Context) (gen.GetWebCarrierSettingsRes, error) {
	var out *gen.WebCarrierSettings
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		set, err := fulfillment.GetWebCarrierSettings(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // fulfillment đã bọc
		}
		out, err = webCarrierSettingsOut(ctx, tx, set, h)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc cài đặt hãng trên web: %w", err)
	}
	return out, nil
}

// SaveWebCarrierSettings (PUT /api/v1/web-carrier-settings): ghi cả bộ khi version khớp (409); ưu tiên không hiện trên
// web → 422 carrier_not_on_web; audit fulfillment.web_carrier_settings.update; sự kiện carrier.web_settings_changed
// làm mới bộ đệm website.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SaveWebCarrierSettings(ctx context.Context, req *gen.SaveWebCarrierSettingsRequest, _ gen.SaveWebCarrierSettingsParams) (gen.SaveWebCarrierSettingsRes, error) {
	s, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	in := fulfillment.WebCarrierSettings{Version: int(req.Version)}
	if v, ok := req.PreferredAccountId.Get(); ok {
		in.PreferredAccountID = &v
	}
	if v, ok := req.FallbackFee.Get(); ok {
		in.FallbackFee = new(int64(v))
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isWebCarrierRefusal, "lưu cài đặt hãng trên web",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			ch, err := s.SaveWebCarrierSettings(ctx, tx, in, fulfillment.Actor{TenantID: u.TenantID, UserID: u.UserID})
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			out, err := webCarrierSettingsOut(ctx, tx, ch.After, h)
			if err != nil {
				return nil, err
			}
			return out, h.audit(ctx, tx, u, "fulfillment.web_carrier_settings.update", "fulfillment.web_carrier_settings",
				u.TenantID, ch.Before.AuditFields(), ch.After.AuditFields())
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCarrierSettings), nil //nolint:forcetypeassert // fn trên trả *gen.WebCarrierSettings
}

// isWebCarrierRefusal: lỗi nghiệp vụ của SaveWebCarrierSettings — trả lời, không lưu theo khóa chống trùng.
func isWebCarrierRefusal(err error) bool {
	return errors.Is(err, fulfillment.ErrCarrierNotOnWeb) || errors.Is(err, fulfillment.ErrVersionConflict) ||
		errors.Is(err, fulfillment.ErrSettingOutOfRange)
}

// ReorderWebCarriers (PUT /api/v1/web-carrier-settings/order, Task 488): đổi thứ tự cả danh sách hãng trên web khi
// version khớp và tập tài khoản đúng tập đang hiện (không thì 409); audit fulfillment.web_carrier_order.update.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ReorderWebCarriers(ctx context.Context, req *gen.ReorderWebCarriersRequest, _ gen.ReorderWebCarriersParams) (gen.ReorderWebCarriersRes, error) {
	s, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isWebCarrierRefusal, "đổi thứ tự hãng trên web",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			ch, err := s.ReorderWebCarriers(ctx, tx, req.AccountIds, int(req.Version), fulfillment.Actor{TenantID: u.TenantID, UserID: u.UserID})
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			out, err := webCarrierSettingsOut(ctx, tx, ch.Settings, h)
			if err != nil {
				return nil, err
			}
			return out, h.audit(ctx, tx, u, "fulfillment.web_carrier_order.update", "fulfillment.web_carrier_settings",
				u.TenantID, ch.AuditFields(ch.Before), ch.AuditFields(ch.After))
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebCarrierSettings), nil //nolint:forcetypeassert // fn trên trả *gen.WebCarrierSettings
}

// webCarrierSettingsOut là cài đặt của hợp đồng nhân viên: người, giờ lưu cuối (kèm tên), tài khoản hiện trên web và
// tài khoản chưa hiện (trừ revoked, xếp theo tên; Task 488), mỗi tài khoản kèm số vận đơn 30 ngày.
func webCarrierSettingsOut(ctx context.Context, tx pgx.Tx, set fulfillment.WebCarrierSettings, h *handler,
) (*gen.WebCarrierSettings, error) {
	stats, err := fulfillment.WebCarrierStats(ctx, tx, set, h.now())
	if err != nil {
		return nil, err //nolint:wrapcheck // fulfillment đã bọc
	}
	out := &gen.WebCarrierSettings{
		Version:  int32(set.Version), //nolint:gosec // G115: version int4 của bảng
		Carriers: []gen.WebCarrierStat{}, HiddenCarriers: []gen.WebCarrierStat{},
	}
	if err := stampInto(ctx, tx, set.UpdatedBy, set.UpdatedAt, &out.UpdatedBy, &out.UpdatedByName, &out.UpdatedAt); err != nil {
		return nil, err
	}
	if set.PreferredAccountID != nil {
		out.PreferredAccountId = gen.NewOptUUID(*set.PreferredAccountID)
	}
	if set.FallbackFee != nil {
		out.FallbackFee = gen.NewOptMoney(gen.Money(*set.FallbackFee))
	}
	for _, c := range stats {
		g := gen.WebCarrierStat{
			AccountId: c.AccountID, CarrierKey: c.CarrierKey, Name: c.Name, Status: c.Status,
			WebSort: int32(c.WebSort), Preferred: c.Preferred, Shipments30d: int32(c.Shipments30d), //nolint:gosec // G115: web_sort ≤ 1000, số đếm int4
		}
		if c.ShowOnWeb {
			out.Carriers = append(out.Carriers, g)
		} else {
			out.HiddenCarriers = append(out.HiddenCarriers, g)
		}
	}
	slices.SortStableFunc(out.HiddenCarriers, func(a, b gen.WebCarrierStat) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// WebShippingCarriers (GET /api/v1/web/shipping/carriers) → 200.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebShippingCarriers(ctx context.Context) (gen.WebShippingCarriersRes, error) {
	sh, u, err := h.webShipping(ctx)
	if err != nil {
		return nil, err
	}
	return sh.Carriers(ctx, u.TenantID) //nolint:wrapcheck // lỗi của web, đổi mã ở errors*.go
}

// WebShippingQuote (POST /api/v1/web/shipping/quote) → 200; không hãng nào báo được và không phí dự phòng → 503.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebShippingQuote(ctx context.Context, req *gen.WebShippingQuoteRequest) (gen.WebShippingQuoteRes, error) {
	sh, u, err := h.webShipping(ctx)
	if err != nil {
		return nil, err
	}
	return sh.Quote(ctx, u.TenantID, req) //nolint:wrapcheck // như trên
}

// WebDeliveryEstimates (GET /api/v1/web/shipping/estimates) → 200.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebDeliveryEstimates(ctx context.Context) (gen.WebDeliveryEstimatesRes, error) {
	sh, u, err := h.webShipping(ctx)
	if err != nil {
		return nil, err
	}
	return sh.Estimates(ctx, u.TenantID) //nolint:wrapcheck // như trên
}

// webShipping trả phần hãng của website (Deps.Fulfillment nil → lỗi 500 như route M3) và khóa dịch vụ đang gọi.
func (h *handler) webShipping(ctx context.Context) (*web.Shipping, identity.AuthUser, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, identity.AuthUser{}, errNoAuthUser
	}
	if h.d.webShipping == nil {
		_, err := h.fulfillmentService()
		return nil, u, err
	}
	return h.d.webShipping, u, nil
}
