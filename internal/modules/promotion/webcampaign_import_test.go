package promotion_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/promotion"
)

func (e *env) importCampaign(in promotion.ImportedCampaign) (promotion.WebCampaign, bool, error) {
	var (
		c       promotion.WebCampaign
		created bool
	)
	err := e.app(func(tx pgx.Tx) error {
		var err error
		c, created, err = campaignWriter().ImportCampaign(e.t.Context(), tx, e.actor(), e.shop.Seller, in)
		return err //nolint:wrapcheck // test
	})
	return c, created, err
}

// Nhập chiến dịch từ website (Task 467): giữ id (link_key = 8 hex đầu như website), trạng thái paused, ngày tạo, đích
// danh mục; một sự kiện promotion.web_campaign.changed; nhập lại cùng id → không tạo, không thêm sự kiện; trạng thái
// enabled → ErrInvalidInput (bên gọi đổi sang paused trước, QĐ35); vượt trần → ErrPercentTooHigh; id khác mà trùng
// 8 hex đầu với chiến dịch có sẵn → ErrInvalidInput, không ghi.
func TestImportCampaign(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	cat := e.category("ao-so-mi")
	created := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	in := promotion.ImportedCampaign{
		ID: uuid.MustParse("3f2a9c1e-0b7d-4c55-9e21-6a8b0c4d2e1f"), State: promotion.StatePaused, CreatedAt: created,
		Input: promotion.WebCampaignInput{
			Name: " Sale áo ", Percent: 30, Scope: promotion.ScopeCategories, TargetIDs: []uuid.UUID{cat},
			StartsAt: created, EndsAt: created.Add(90 * 24 * time.Hour),
		},
	}
	c, ok, err := e.importCampaign(in)
	if err != nil || !ok {
		t.Fatalf("nhập lần đầu: %v %v", ok, err)
	}
	if c.ID != in.ID || c.State != promotion.StatePaused || c.Name != "Sale áo" || c.Revision != 1 ||
		!c.CreatedAt.Equal(created) || c.LinkKey != "3f2a9c1e" || len(c.TargetIDs) != 1 || c.TargetIDs[0] != cat ||
		c.CreatedBy != e.actor().UserID {
		t.Fatalf("chiến dịch đã nhập = %+v", c)
	}
	got := e.mustGet(in.ID)
	if got.State != promotion.StatePaused || !got.CreatedAt.Equal(created) || len(got.TargetIDs) != 1 {
		t.Fatalf("đọc lại = %+v", got)
	}
	events := `SELECT count(*) FROM platform.domain_events WHERE topic = $1`
	if n := e.staff.CountIn(t, e.tenant, events, promotion.TopicWebCampaignChanged); n != 1 {
		t.Fatalf("sự kiện = %d, mong 1", n)
	}

	again, ok, err := e.importCampaign(in)
	if err != nil || ok || again.ID != in.ID {
		t.Fatalf("nhập lại: %v %v %+v", ok, err, again)
	}
	if n := e.staff.CountIn(t, e.tenant, events, promotion.TopicWebCampaignChanged); n != 1 {
		t.Fatalf("nhập lại thêm sự kiện: %d", n)
	}

	bad := in
	bad.ID, bad.State = uuid.New(), promotion.StateEnabled
	_, _, err = e.importCampaign(bad)
	wantErr(t, "trạng thái enabled", err, promotion.ErrInvalidInput)

	bad = in
	bad.ID, bad.Input.Percent = uuid.New(), 60
	_, _, err = e.importCampaign(bad)
	wantErr(t, "vượt trần", err, promotion.ErrPercentTooHigh)

	bad = in
	bad.ID = uuid.MustParse("3f2a9c1e-ffff-4c55-9e21-6a8b0c4d2e1f")
	_, _, err = e.importCampaign(bad)
	wantErr(t, "trùng link_key", err, promotion.ErrInvalidInput)

	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM promotion.web_campaigns`); n != 1 {
		t.Fatalf("số chiến dịch = %d, mong 1", n)
	}
	if !strings.HasPrefix(promotion.CampaignPath(c.ID, c.Name), "/uu-dai/") {
		t.Fatalf("đường dẫn = %s", promotion.CampaignPath(c.ID, c.Name))
	}
}

func (e *env) mustGet(id uuid.UUID) promotion.WebCampaign {
	e.t.Helper()
	var c promotion.WebCampaign
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		c, err = promotion.GetWebCampaign(e.t.Context(), tx, e.shop.Seller, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return c
}
