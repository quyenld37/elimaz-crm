package web

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Cài đặt quà sinh nhật cho website (Task 512): đúng các số CRM dùng khi phát quà (promotion.birthday_settings, Task
// 468), để thẻ quà trên trang kết quả đơn không lệch với quà thật. Bộ đệm SpaceBirthdaySettings 60 s, chỉ hết hạn: lưu
// cài đặt không phát sự kiện, cài đặt đổi hiếm và chỉ dùng để hiện chữ.
const (
	birthdaySettingsTTL = 60 * time.Second
	birthdaySettingsKey = "current"
)

// BirthdaySettings trả cài đặt quà sinh nhật (chưa lưu → promotion.DefaultBirthdaySettings), có ETag; If-None-Match
// khớp → 304. Không có version, người sửa.
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) BirthdaySettings(ctx context.Context, tenant uuid.UUID, p gen.WebBirthdaySettingsParams,
) (gen.WebBirthdaySettingsRes, error) {
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceBirthdaySettings, birthdaySettingsKey, birthdaySettingsTTL,
		func(ctx context.Context) ([]byte, error) {
			var s promotion.BirthdaySettings
			err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
				var err error
				s, err = promotion.GetBirthdaySettings(ctx, tx)
				return err //nolint:wrapcheck // promotion đã bọc
			}, db.TxAPI)
			if err != nil {
				return nil, fmt.Errorf("đọc cài đặt quà sinh nhật cho website: %w", err)
			}
			out := gen.BirthdaySettingsFields{
				Enabled: s.Enabled, Amount: s.Amount, MinOrder: s.MinOrder,
				DaysBefore: int32(s.DaysBefore), ValidDays: int32(s.ValidDays), //nolint:gosec // G115: CHECK ≤ 60
				MinLeadDays: int32(s.MinLeadDays), //nolint:gosec // G115: CHECK ≤ 120
			}
			return out.MarshalJSON()
		})
	if err != nil {
		return nil, err //nolint:wrapcheck // ReadCache đã bọc; ErrBusy giữ nguyên để đổi 503
	}
	tag := etag(raw)
	if matches(p.IfNoneMatch, tag) {
		return &gen.WebBirthdaySettingsNotModified{}, nil
	}
	var out gen.BirthdaySettingsFields
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc cài đặt quà sinh nhật trong bộ đệm: %w", err)
	}
	return &gen.BirthdaySettingsFieldsHeaders{ETag: gen.NewOptString(tag), Response: out}, nil
}
