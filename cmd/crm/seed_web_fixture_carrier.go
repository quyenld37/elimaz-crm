package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
)

// webCarrierRef là mã ở hãng của tài khoản hãng giả cho e2e website (riêng với fake-1 của seed-dev, để không đổi tên
// tài khoản dev).
const webCarrierRef = "web-e2e"

// errWebCarrierNotWired: lệnh bật hãng trên web mà seeder không có fulfillment.Service (lỗi nối trong cmd/crm).
var errWebCarrierNotWired = errors.New("seed-web-fixture: chưa nối fulfillment.Service cho bước hãng trên web")

// webCarrierSeed là cờ --web-carrier, --web-carrier-fee: tên hãng hiện trên website và phí (đồng) làm phí dự phòng.
// Name rỗng: không làm bước hãng.
type webCarrierSeed struct {
	Name string
	Fee  int64
}

// webCarrier bật tài khoản hãng giả (adapter fake của M3; máy chủ tools/fakecarrier với FAKECARRIER_QUOTE_FEE = phí
// báo đúng phí cố định) hiện trên website: tài khoản web-e2e ở kho mặc định (chèn nếu chưa có), đúng tên, show_on_web,
// mã địa giới thử của tệp cho hãng giả (mã hãng = mã CRM: báo phí đòi địa chỉ đã khớp), là ưu tiên trên web, phí dự
// phòng = Fee (khi hãng giả không chạy, web vẫn báo được phí). Qua public API của fulfillment trong tx của bên gọi;
// audit như màn hình (cùng tên hành động); so rồi mới ghi. Thứ tự khóa ADR-46 nhóm 8: carrier_accounts →
// carrier_locations → channel_carrier_settings (kho: chỉ chèn nếu thiếu, không khóa dòng có sẵn).
func (a *fixtureApply) webCarrier(ctx context.Context, c webCarrierSeed, svc *fulfillment.Service, ls []fixtureLocation) error {
	if c.Name == "" {
		return nil
	}
	if svc == nil {
		return errWebCarrierNotWired
	}
	wh, err := inventory.EnsureDefaultWarehouse(ctx, a.tx, a.actor.TenantID, a.seller)
	if err != nil {
		return err //nolint:wrapcheck // inventory đã bọc
	}
	id, err := fulfillment.EnsureCarrierAccount(ctx, a.tx, fulfillment.SeedAccountInput{
		SellerID: a.seller, WarehouseID: wh, CarrierKey: fake.CarrierKey, Name: c.Name, ExternalAccountRef: webCarrierRef,
	})
	if err != nil {
		return err //nolint:wrapcheck // fulfillment đã bọc
	}
	actor := fulfillment.Actor{TenantID: a.actor.TenantID, UserID: a.actor.UserID}
	before, err := svc.GetAccount(ctx, a.tx, id)
	if err != nil {
		return fmt.Errorf("đọc tài khoản hãng giả: %w", err)
	}
	if before.Status != fulfillment.AccountActive {
		return fmt.Errorf("tài khoản hãng giả %s đang %s: %w", webCarrierRef, before.Status, errFixtureDrift)
	}
	if before.Name != c.Name || !before.ShowOnWeb {
		after, err := svc.UpdateAccount(ctx, a.tx, id, fulfillment.AccountPatch{Name: &c.Name, ShowOnWeb: new(true)}, before.Version, actor)
		if err != nil {
			return fmt.Errorf("bật hãng giả trên web: %w", err)
		}
		if err := a.audit(ctx, "fulfillment.carrier_account.update", "carrier_account", id, before.AuditFields(), after.AuditFields()); err != nil {
			return err
		}
		a.rep.Updated["carrier_account"]++
	}
	var locs []fulfillment.SeedLocation
	for _, l := range ls {
		locs = append(locs, fulfillment.SeedLocation{Code: l.Code, Name: l.Name})
		for _, w := range l.Wards {
			locs = append(locs, fulfillment.SeedLocation{Code: w.Code, Name: w.Name, Ward: true})
		}
	}
	n, err := fulfillment.EnsureSeedLocations(ctx, a.tx, fake.CarrierKey, locs)
	if err != nil {
		return err //nolint:wrapcheck // fulfillment đã bọc
	}
	if n > 0 {
		a.rep.Updated["carrier_location"] += n
	}
	return a.webCarrierSettings(ctx, id, c.Fee, svc, actor)
}

// webCarrierSettings đặt tài khoản id là ưu tiên trên web và phí dự phòng fee khi khác cài đặt đang có.
func (a *fixtureApply) webCarrierSettings(ctx context.Context, id uuid.UUID, fee int64, svc *fulfillment.Service, actor fulfillment.Actor) error {
	cur, err := fulfillment.GetWebCarrierSettings(ctx, a.tx)
	if err != nil {
		return err //nolint:wrapcheck // fulfillment đã bọc
	}
	if cur.PreferredAccountID != nil && *cur.PreferredAccountID == id && cur.FallbackFee != nil && *cur.FallbackFee == fee {
		return nil
	}
	ch, err := svc.SaveWebCarrierSettings(ctx, a.tx,
		fulfillment.WebCarrierSettings{PreferredAccountID: &id, FallbackFee: &fee, Version: cur.Version}, actor)
	if err != nil {
		return fmt.Errorf("lưu cài đặt hãng trên web: %w", err)
	}
	if err := a.audit(ctx, "fulfillment.web_carrier_settings.update", "fulfillment.web_carrier_settings", a.actor.TenantID,
		ch.Before.AuditFields(), ch.After.AuditFields()); err != nil {
		return err
	}
	a.rep.Updated["web_carrier_settings"]++
	return nil
}

// audit ghi một dòng audit của nhân viên --staff-email (fulfillment để bên gọi ghi audit, như lớp HTTP).
func (a *fixtureApply) audit(ctx context.Context, action, entity string, id uuid.UUID, before, after map[string]any) error {
	if err := identity.RecordAudit(ctx, a.tx, identity.AuditEntry{
		TenantID: a.actor.TenantID, ActorType: "staff", ActorID: a.actor.UserID.String(), Action: action,
		EntityType: entity, EntityID: id.String(), Before: before, After: after, RequestID: seedFixtureCommand,
	}); err != nil {
		return fmt.Errorf("ghi audit %s: %w", action, err)
	}
	return nil
}
