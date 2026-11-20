package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Vùng fulfillment_settings (kế hoạch M3, Task M3-04): cài đặt vận chuyển của shop (platform.settings khóa
// "fulfillment"). Đọc cần fulfillment.read (mọi phạm vi: cài đặt áp cả shop), sửa cần fulfillment.settings.manage
// (bảng route). Audit ghi ở đây, cùng tx (fulfillment không nhập identity).

// GetFulfillmentSettings (GET /api/v1/fulfillment-settings): chưa lưu lần nào → mặc định, version 0.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetFulfillmentSettings(ctx context.Context) (gen.GetFulfillmentSettingsRes, error) {
	var s fulfillment.Settings
	var version int64
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		s, version, err = fulfillment.LoadSettings(ctx, tx)
		return err //nolint:wrapcheck // fulfillment đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc cài đặt vận chuyển: %w", err)
	}
	return fulfillmentSettingsOut(s, version), nil
}

// UpdateFulfillmentSettings (PUT /api/v1/fulfillment-settings): ghi cả bộ khi version khớp (0: chưa lưu lần nào); sai
// luật → 422 setting_out_of_range; version cũ → 409 version_conflict. Từ chối không lưu theo khóa chống trùng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateFulfillmentSettings(ctx context.Context, req *gen.UpdateFulfillmentSettingsRequest, _ gen.UpdateFulfillmentSettingsParams) (gen.UpdateFulfillmentSettingsRes, error) {
	next := fulfillment.Settings{
		StuckHours: intMap(req.StuckHours), CallbackDeadlineHours: int(req.CallbackDeadlineHours),
		PollMinutes: intMap(req.PollMinutes), DefaultWeightGramsPerItem: int(req.DefaultWeightGramsPerItem),
		QuoteValidMinutes: int(req.QuoteValidMinutes),
		ReturnRisk: fulfillment.ReturnRisk{
			WatchMinReturned: int(req.ReturnRisk.WatchMinReturned), HighMinReturned: int(req.ReturnRisk.HighMinReturned),
			HighRateBP: int(req.ReturnRisk.HighRateBp),
		},
		CODOverdueDays: int(req.CodOverdueDays),
		AgedDays:       fulfillment.AgedDays{Held: int(req.AgedDays.Held), InTransit: int(req.AgedDays.InTransit)},
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isShipmentRefusal, "ghi cài đặt vận chuyển",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			before, _, err := fulfillment.LoadSettings(ctx, tx)
			if err != nil {
				return nil, err //nolint:wrapcheck // fulfillment đã bọc
			}
			version, err := fulfillment.SaveSettings(ctx, tx, next, req.Version, fulfillment.Actor{TenantID: u.TenantID, UserID: u.UserID})
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			return fulfillmentSettingsOut(next, version), h.audit(ctx, tx, u, "fulfillment.settings.update",
				"fulfillment.settings", u.TenantID, before.AuditFields(), next.AuditFields())
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.FulfillmentSettings), nil //nolint:forcetypeassert // fn trên trả *gen.FulfillmentSettings
}

func intMap[M ~map[string]int32](m M) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = int(v)
	}
	return out
}

func int32Map[M ~map[string]int32](m map[string]int) M { //nolint:ireturn // trả đúng kiểu map của mã sinh (tham số kiểu)
	out := make(M, len(m))
	for k, v := range m {
		out[k] = int32(v) //nolint:gosec // Validate đã kiểm > 0; giá trị lưu đến từ int32 của hợp đồng
	}
	return out
}

func fulfillmentSettingsOut(s fulfillment.Settings, version int64) *gen.FulfillmentSettings {
	return &gen.FulfillmentSettings{
		StuckHours:                int32Map[gen.FulfillmentSettingsStuckHours](s.StuckHours),
		CallbackDeadlineHours:     int32(s.CallbackDeadlineHours), //nolint:gosec // giá trị đến từ int32 của hợp đồng
		PollMinutes:               int32Map[gen.FulfillmentSettingsPollMinutes](s.PollMinutes),
		DefaultWeightGramsPerItem: int32(s.DefaultWeightGramsPerItem), //nolint:gosec // như trên
		QuoteValidMinutes:         int32(s.QuoteValidMinutes),         //nolint:gosec // như trên
		ReturnRisk: gen.ReturnRiskSettings{
			WatchMinReturned: int32(s.ReturnRisk.WatchMinReturned), //nolint:gosec // như trên
			HighMinReturned:  int32(s.ReturnRisk.HighMinReturned),  //nolint:gosec // như trên
			HighRateBp:       int32(s.ReturnRisk.HighRateBP),       //nolint:gosec // như trên
		},
		CodOverdueDays: int32(s.CODOverdueDays), //nolint:gosec // như trên
		AgedDays: gen.AgedDaysSettings{
			Held: int32(s.AgedDays.Held), InTransit: int32(s.AgedDays.InTransit), //nolint:gosec // như trên
		},
		Version: version,
	}
}
