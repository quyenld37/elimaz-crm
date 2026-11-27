package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// importSystemName là người làm của lệnh nhập: audit actor_type system, actor_id = importSystemID().
const importSystemName = "import-website-data"

// importSystemID là mã cố định của tài khoản hệ thống "nhập dữ liệu website" (không có dòng identity.users; ghi vào
// created_by, updated_by — các cột không có khóa ngoại): UUID v5 của "system:import-website-data".
func importSystemID() uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("elimaz-crm:system:"+importSystemName))
}

// defaultPauseReason là lý do khi website tạm dừng nhận đơn mà không ghi lý do (CRM bắt buộc lý do khi tạm dừng).
const defaultPauseReason = "Tạm dừng từ website (nhập dữ liệu)"

// errDryRunRollback lùi transaction của lần chạy khô.
var errDryRunRollback = errors.New("chạy khô: lùi")

// websiteImporter nhập một data.json vào tenant Tenant. Mỗi loại là một bước chạy trong transaction tenant (apply: mỗi
// bước một transaction; chạy khô: mọi bước trong một transaction lùi ở cuối, mỗi bước một savepoint); mỗi bản ghi một
// savepoint, lỗi của bản ghi không làm hỏng bản ghi khác. Fulfillment nil → loại hãng lỗi. Files, WaitImage chỉ cần
// khi --apply (ảnh banner).
type websiteImporter struct {
	DB          *db.DB
	Tenant      uuid.UUID
	DryRun      bool
	Events      *events.Registry
	Queue       queue.JobQueue
	Bus         realtime.Bus
	Fulfillment *fulfillment.Service
	Files       files.FileStore
	WaitImage   func(ctx context.Context, fileID uuid.UUID) error
	ImageView   catalog.ImageView
	Now         func() time.Time
}

// importStep là một loại dữ liệu chạy trong tx; lỗi trả về là lỗi của cả loại (tx/savepoint của loại lùi).
type importStep struct {
	kind string
	run  func(ctx context.Context, tx pgx.Tx, c *importCount) error
}

// run nhập x (đọc từ thư mục dir) và trả bảng. Lỗi trả về là lỗi hạ tầng (không mở được transaction); lỗi của một
// loại hay bản ghi nằm trong bảng.
func (im *websiteImporter) run(ctx context.Context, dir string, x wxExport, unmatched map[string]int) (*importReport, error) {
	rep := newImportReport(im.DryRun, im.Tenant)
	rep.Unmatched, rep.Birthdays, rep.Declined = unmatched, len(x.Birthdays.Profiles), x.Birthdays.DeclinedLeftOut
	images, err := im.prepareBannerImages(ctx, dir, x, rep.Kinds[kindBanner])
	if err != nil {
		return rep, err
	}
	steps := []importStep{
		{kindMeasure, func(ctx context.Context, tx pgx.Tx, c *importCount) error {
			return im.measures(ctx, tx, x.ProductMeasures, c)
		}},
		{kindFeedback, func(ctx context.Context, tx pgx.Tx, c *importCount) error { return im.feedback(ctx, tx, x.Feedback, c) }},
		{kindPause, func(ctx context.Context, tx pgx.Tx, c *importCount) error {
			return im.pause(ctx, tx, x.CheckoutPause, c)
		}},
		{kindCarriers, func(ctx context.Context, tx pgx.Tx, c *importCount) error { return im.carriers(ctx, tx, x.Carriers, c) }},
		{kindCampaign, func(ctx context.Context, tx pgx.Tx, c *importCount) error {
			return im.campaigns(ctx, tx, x.Campaigns, c)
		}},
		{kindGift, func(ctx context.Context, tx pgx.Tx, c *importCount) error { return im.gift(ctx, tx, x.GiftSettings, c) }},
		{kindBanner, func(ctx context.Context, tx pgx.Tx, c *importCount) error {
			return im.banners(ctx, tx, x.Banners, images, c)
		}},
		{kindLook, func(ctx context.Context, tx pgx.Tx, c *importCount) error { return im.looks(ctx, tx, x.Looks, c) }},
	}
	if im.DryRun {
		initial := cloneKinds(rep.Kinds)
		err := tenancy.WithTenant(ctx, im.DB, im.Tenant, func(tx pgx.Tx) error {
			rep.Kinds = cloneKinds(initial) // tx có thể chạy lại (RetryTx): không cộng dồn
			for _, s := range steps {
				im.runStep(ctx, tx, s, rep.Kinds[s.kind])
			}
			return errDryRunRollback
		})
		if err != nil && !errors.Is(err, errDryRunRollback) {
			return rep, fmt.Errorf("chạy khô: %w", err)
		}
		return rep, nil
	}
	for _, s := range steps {
		c := rep.Kinds[s.kind]
		before := *c
		err := tenancy.WithTenant(ctx, im.DB, im.Tenant, func(tx pgx.Tx) error {
			*c = before // tx có thể chạy lại: không cộng dồn
			c.Notes = slices.Clone(before.Notes)
			return s.run(ctx, tx, c)
		})
		if err != nil {
			*c = before
			c.Notes = slices.Clone(before.Notes)
			c.fail("cả loại (đã lùi)", err)
		}
	}
	return rep, nil
}

// cloneKinds chép sâu bảng đếm từng loại (cả Notes).
func cloneKinds(m map[string]*importCount) map[string]*importCount {
	out := make(map[string]*importCount, len(m))
	for k, c := range m {
		cp := *c
		cp.Notes = slices.Clone(c.Notes)
		out[k] = &cp
	}
	return out
}

// runStep chạy s trong một savepoint của tx (chạy khô); lỗi → lùi savepoint, ghi lỗi của cả loại.
func (im *websiteImporter) runStep(ctx context.Context, tx pgx.Tx, s importStep, c *importCount) {
	before := *c
	before.Notes = slices.Clone(c.Notes)
	if err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error { return s.run(ctx, sp, c) }); err != nil {
		*c = before
		c.fail("cả loại (đã lùi)", err)
	}
}

func (im *websiteImporter) audit(ctx context.Context, tx pgx.Tx, action, entity, id string, before, after map[string]any) error {
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: im.Tenant, ActorType: customer.ActorSystem, ActorID: importSystemID().String(), Action: action,
		EntityType: entity, EntityID: id, Before: before, After: after, RequestID: importSystemName,
	}); err != nil {
		return fmt.Errorf("ghi audit %s: %w", action, err)
	}
	return nil
}

// feedback nhập góp ý (nhóm 5): khóa tự nhiên là id; gắn khách như form góp ý.
func (im *websiteImporter) feedback(ctx context.Context, tx pgx.Tx, list []wxFeedback, c *importCount) error {
	w := customer.Writer{Events: im.Events, Queue: im.Queue, Realtime: im.Bus}
	a := customer.Actor{Type: customer.ActorSystem, ID: importSystemID(), RequestID: importSystemName}
	for _, f := range list {
		in := customer.ImportedFeedback{
			ID: f.ID, State: f.State, Note: f.Note, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt,
			Input: customer.FeedbackInput{
				Topic: f.Topic, Mood: f.Mood, Body: f.Body, Name: f.Name, Contact: f.Contact, OrderRef: f.OrderRef,
				Page: f.Page, Device: f.Device,
			},
		}
		var created bool
		err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
			var err error
			created, err = w.ImportFeedback(ctx, sp, in, a, im.Now())
			return err //nolint:wrapcheck // sentinel của customer
		})
		switch {
		case err != nil:
			c.fail(f.ID.String(), err)
		case created:
			c.Added++
		default:
			c.Skipped++
		}
	}
	return nil
}

// pause nhập trạng thái tạm dừng nhận đơn web (nhóm 7): CRM chưa ai đổi → theo website; đã bằng → bỏ qua; nhân viên
// CRM đã đổi → giữ của CRM.
func (im *websiteImporter) pause(ctx context.Context, tx pgx.Tx, p wxPause, c *importCount) error {
	if p.LeftOut {
		c.Skipped++
		c.note("trong unmatched.json, giữ của CRM")
		return nil
	}
	cur, err := order.GetWebIntake(ctx, tx)
	if err != nil {
		return err //nolint:wrapcheck // order đã bọc
	}
	switch {
	case cur.Paused == p.Paused:
		c.Skipped++
		return nil
	case cur.ChangedBy != nil:
		c.Skipped++
		c.note("nhân viên CRM đã đổi, giữ của CRM (website: tạm dừng %v)", p.Paused)
		return nil
	}
	reason := strings.TrimSpace(p.Reason)
	if p.Paused && reason == "" {
		reason = defaultPauseReason
	}
	w := order.Writer{Events: im.Events, Queue: im.Queue, Realtime: im.Bus}
	ch, err := w.SetWebIntake(ctx, tx, p.Paused, reason, cur.Version,
		order.Actor{TenantID: im.Tenant, UserID: importSystemID(), Type: customer.ActorSystem}, im.Now())
	if err != nil {
		c.fail("tạm dừng", err)
		return nil
	}
	c.Added++
	return im.audit(ctx, tx, "order.web_intake.set", "order.web_intake", im.Tenant.String(),
		ch.Before.AuditFields(), ch.After.AuditFields())
}

// carriers nhập hãng trên web (nhóm 8): website chưa chọn (configured false: mọi hãng hiện) → giữ hãng hiện trên web
// của CRM, chỉ nhập hãng ưu tiên nếu website có (website vẫn xếp hãng đó lên đầu; CRM đòi hãng ưu tiên hiện trên web
// nên bật hiện trên web cho hãng đó như một hãng website bật); mỗi hãng website bật → mọi tài khoản
// đang hoạt động của hãng đó hiện trên web; hãng ưu tiên → đúng một tài khoản đang hoạt động của hãng đó. Không tắt
// hãng.
func (im *websiteImporter) carriers(ctx context.Context, tx pgx.Tx, x wxCarriers, c *importCount) error {
	switch {
	case x.LeftOut:
		c.Skipped++
		c.note("trong unmatched.json, giữ của CRM")
		return nil
	case !x.Configured && x.Preferred == nil:
		c.Skipped++
		c.note("website chưa chọn hãng (mọi hãng hiện), giữ của CRM")
		return nil
	case im.Fulfillment == nil:
		return errors.New("chưa nối fulfillment.Service")
	}
	accounts, _, err := im.Fulfillment.ListAccounts(ctx, tx, fulfillment.AccountListFilter{Status: fulfillment.AccountActive, Limit: 200})
	if err != nil {
		return fmt.Errorf("đọc tài khoản hãng: %w", err)
	}
	byKey := map[string][]fulfillment.AccountView{}
	for _, a := range accounts {
		byKey[a.CarrierKey] = append(byKey[a.CarrierKey], a)
	}
	actor := fulfillment.Actor{TenantID: im.Tenant, UserID: importSystemID()}
	enabled := x.Enabled
	if !x.Configured { // mọi hãng hiện trên website: CRM chỉ cần hãng ưu tiên hiện trên web (luật của fulfillment)
		enabled = []wxCarrier{*x.Preferred}
		c.note("website chưa chọn hãng (mọi hãng hiện): chỉ bật hiện trên web và đặt ưu tiên cho %s", x.Preferred.Code)
	}
	for _, k := range enabled {
		if len(byKey[k.Code]) == 0 {
			c.fail("hãng "+k.Code, errors.New("không có tài khoản hãng đang hoạt động"))
			continue
		}
		im.showOnWeb(ctx, tx, byKey[k.Code], actor, c)
	}
	if x.Preferred == nil {
		return nil
	}
	return im.preferredCarrier(ctx, tx, byKey[x.Preferred.Code], x.Preferred.Code, actor, c)
}

// showOnWeb bật hiện trên web cho mọi tài khoản trong accounts chưa hiện (mỗi tài khoản một savepoint, lỗi chỉ làm
// tài khoản đó lỗi); accounts được thay bằng bản sau khi sửa (version mới cho bước hãng ưu tiên).
func (im *websiteImporter) showOnWeb(ctx context.Context, tx pgx.Tx, accounts []fulfillment.AccountView,
	actor fulfillment.Actor, c *importCount,
) {
	for i, a := range accounts {
		if a.ShowOnWeb {
			c.Skipped++
			continue
		}
		var after fulfillment.AccountView
		err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
			var err error
			after, err = im.Fulfillment.UpdateAccount(ctx, sp, a.ID, fulfillment.AccountPatch{ShowOnWeb: new(true)}, a.Version, actor)
			if err != nil {
				return err //nolint:wrapcheck // sentinel của fulfillment
			}
			return im.audit(ctx, sp, "fulfillment.carrier_account.update", "carrier_account", a.ID.String(),
				a.AuditFields(), after.AuditFields())
		})
		if err != nil {
			c.fail("tài khoản hãng "+a.ID.String(), err)
			continue
		}
		accounts[i] = after
		c.Added++
	}
}

func (im *websiteImporter) preferredCarrier(ctx context.Context, tx pgx.Tx, accounts []fulfillment.AccountView, code string,
	actor fulfillment.Actor, c *importCount,
) error {
	if len(accounts) != 1 {
		c.fail("hãng ưu tiên "+code, fmt.Errorf("có %d tài khoản đang hoạt động, cần đúng 1 (chọn trong CRM)", len(accounts)))
		return nil
	}
	cur, err := fulfillment.GetWebCarrierSettings(ctx, tx)
	if err != nil {
		return err //nolint:wrapcheck // fulfillment đã bọc
	}
	id := accounts[0].ID
	switch {
	case cur.PreferredAccountID != nil && *cur.PreferredAccountID == id:
		c.Skipped++
		return nil
	case cur.UpdatedBy != nil:
		c.Skipped++
		c.note("nhân viên CRM đã lưu cài đặt hãng trên web, giữ ưu tiên của CRM (website: %s)", code)
		return nil
	}
	err = db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		ch, err := im.Fulfillment.SaveWebCarrierSettings(ctx, sp,
			fulfillment.WebCarrierSettings{PreferredAccountID: &id, FallbackFee: cur.FallbackFee, Version: cur.Version}, actor)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của fulfillment
		}
		return im.audit(ctx, sp, "fulfillment.web_carrier_settings.update", "fulfillment.web_carrier_settings",
			im.Tenant.String(), ch.Before.AuditFields(), ch.After.AuditFields())
	})
	if err != nil {
		c.fail("hãng ưu tiên "+code, err)
		return nil
	}
	c.Added++
	return nil
}

// campaigns nhập chiến dịch giảm giá web (nhóm 10) của người bán mặc định: giữ id, ngày tạo; enabled → paused (QĐ35:
// nhân viên xem trước rồi bật lại); danh mục theo đường tên.
func (im *websiteImporter) campaigns(ctx context.Context, tx pgx.Tx, list []wxCampaign, c *importCount) error {
	if len(list) == 0 {
		return nil
	}
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return err //nolint:wrapcheck // catalog đã bọc
	}
	cats, err := loadCategoryPaths(ctx, tx)
	if err != nil {
		return err
	}
	w := promotion.Writer{Events: im.Events, Queue: im.Queue, Realtime: im.Bus}
	a := promotion.Actor{TenantID: im.Tenant, UserID: importSystemID()}
	for _, x := range list {
		in, err := campaignImport(x, cats)
		if err != nil {
			c.fail(x.ID.String(), err)
			continue
		}
		var created bool
		err = db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
			got, ok, err := w.ImportCampaign(ctx, sp, a, seller, in)
			if err != nil || !ok {
				created = false
				return err //nolint:wrapcheck // sentinel của promotion
			}
			created = true
			return im.audit(ctx, sp, "promotion.web_campaign.import", "promotion.web_campaign", got.ID.String(), nil, got.AuditFields())
		})
		switch {
		case err != nil:
			c.fail(x.ID.String(), err)
		case created:
			c.Added++
			if x.State == promotion.StateEnabled {
				c.note("%s đang bật trên website → nhập ở tạm dừng: xem trước rồi bật lại", x.ID)
			}
		default:
			c.Skipped++
		}
	}
	return nil
}

func campaignImport(x wxCampaign, cats categoryPaths) (promotion.ImportedCampaign, error) {
	state := x.State
	switch state {
	case promotion.StateEnabled:
		state = promotion.StatePaused
	case promotion.StatePaused, promotion.StateDraft:
	default:
		return promotion.ImportedCampaign{}, fmt.Errorf("trạng thái %q lạ", x.State)
	}
	created := x.StartsAt
	if x.CreatedAt != nil {
		created = *x.CreatedAt
	}
	in := promotion.WebCampaignInput{
		Name: x.Name, Percent: x.Percent, Scope: x.Scope, AllowCoupon: x.AllowCoupon, Clearance: x.Clearance,
		StartsAt: x.StartsAt, EndsAt: x.EndsAt,
	}
	switch x.Scope {
	case promotion.ScopeProducts:
		in.TargetIDs = x.ProductIDs
	case promotion.ScopeCategories:
		for _, ref := range x.Categories {
			id, err := cats.resolve(ref)
			if err != nil {
				return promotion.ImportedCampaign{}, err
			}
			in.TargetIDs = append(in.TargetIDs, id)
		}
	}
	return promotion.ImportedCampaign{ID: x.ID, State: state, CreatedAt: created, Input: in}, nil
}

// gift nhập cài đặt quà sinh nhật (nhóm 10, sau web_campaigns): đúng enabled và số của website (kể cả khi website
// chưa lưu: website đang chạy bằng mặc định của nó); CRM đã bằng → bỏ qua; nhân viên CRM đã lưu khác → giữ của CRM.
func (im *websiteImporter) gift(ctx context.Context, tx pgx.Tx, g wxGiftSettings, c *importCount) error {
	if g.LeftOut {
		c.Skipped++
		c.note("trong unmatched.json, giữ của CRM")
		return nil
	}
	cur, err := promotion.GetBirthdaySettings(ctx, tx)
	if err != nil {
		return err //nolint:wrapcheck // promotion đã bọc
	}
	want := promotion.BirthdaySettings{
		Enabled: g.Enabled, Amount: g.Amount, MinOrder: g.MinOrder, DaysBefore: g.DaysBefore, ValidDays: g.ValidDays,
		MinLeadDays: g.MinLeadDays,
	}
	switch {
	case cur.Enabled == want.Enabled && cur.Amount == want.Amount && cur.MinOrder == want.MinOrder &&
		cur.DaysBefore == want.DaysBefore && cur.ValidDays == want.ValidDays && cur.MinLeadDays == want.MinLeadDays:
		c.Skipped++
		return nil
	case cur.UpdatedBy != nil:
		c.Skipped++
		c.note("nhân viên CRM đã lưu cài đặt quà khác, giữ của CRM")
		return nil
	}
	err = db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		before, after, err := promotion.Writer{}.SaveBirthdaySettings(ctx, sp, want, cur.Version,
			promotion.Actor{TenantID: im.Tenant, UserID: importSystemID()}, im.Now())
		if err != nil {
			return err //nolint:wrapcheck // sentinel của promotion
		}
		return im.audit(ctx, sp, "promotion.birthday.settings", "promotion.birthday_settings", im.Tenant.String(),
			before.AuditFields(), after.AuditFields())
	})
	if err != nil {
		c.fail("cài đặt quà", err)
		return nil
	}
	c.Added++
	return nil
}

// categoryPaths là danh mục đang dùng của CRM theo đường tên (từ cấp đầu, tên cắt khoảng trắng).
type categoryPaths map[string][]uuid.UUID

func pathKey(names []string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString(strings.TrimSpace(n))
		b.WriteByte(0)
	}
	return b.String()
}

func loadCategoryPaths(ctx context.Context, tx pgx.Tx) (categoryPaths, error) {
	all, err := catalog.ListCategories(ctx, tx)
	if err != nil {
		return nil, err //nolint:wrapcheck // catalog đã bọc
	}
	byID := make(map[uuid.UUID]catalog.Category, len(all))
	for _, cat := range all {
		byID[cat.ID] = cat
	}
	out := categoryPaths{}
	for _, cat := range all {
		if cat.Status != catalog.CategoryActive {
			continue
		}
		var names []string
		ok := true
		for at, depth := cat, 0; ; depth++ {
			names = append([]string{at.Name}, names...)
			if at.ParentID == nil {
				break
			}
			p, found := byID[*at.ParentID]
			if !found || depth > catalog.MaxCategoryDepth {
				ok = false
				break
			}
			at = p
		}
		if ok {
			k := pathKey(names)
			out[k] = append(out[k], cat.ID)
		}
	}
	return out, nil
}

// resolve trả đúng một danh mục CRM có đường tên của ref; 0 hay nhiều → lỗi (không đoán).
func (p categoryPaths) resolve(ref wxCategory) (uuid.UUID, error) {
	ids := p[pathKey(ref.Path)]
	if len(ids) != 1 {
		return uuid.Nil, fmt.Errorf("danh mục Nhanh %d (%d cấp): %d danh mục CRM cùng đường tên, cần đúng 1", ref.NhanhID,
			len(ref.Path), len(ids))
	}
	return ids[0], nil
}
