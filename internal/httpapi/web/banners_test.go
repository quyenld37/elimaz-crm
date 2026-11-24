package web_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
)

const bannersPath = "/api/v1/web/content/banners"

// bannerImages tạo n ảnh banner đã xử lý (bộ banner-v1) và một ảnh đang xử lý của tenant của e.
func (e *env) bannerImages(n int) (ready []uuid.UUID, processing uuid.UUID) {
	e.t.Helper()
	content.RegisterFilePurposes()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		for range n {
			ready = append(ready, filestest.Image(e.t, tx, e.tenant, content.BannerPurposeKey))
		}
		processing = filestest.Uploaded(e.t, tx, e.tenant, content.BannerPurposeKey)
		return nil
	})
	return ready, processing
}

func (e *env) saveBanners(list []content.Banner) {
	e.t.Helper()
	e.app(func(tx pgx.Tx) error {
		_, err := content.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}.SaveBanners(e.t.Context(), tx, list, 0,
			content.Actor{TenantID: e.tenant, UserID: e.owner.UserID}, time.Now())
		return err //nolint:wrapcheck // test
	})
}

// [KM04] Banner cho website: chỉ banner đang hiện theo thứ tự (không banner tắt, hết giờ, chưa tới giờ, ảnh đang xử lý,
// nguồn không có mẫu), kèm đường dẫn WebP 2400/1200 của bộ banner-v1, ảnh điện thoại riêng; ETag, If-None-Match → 304;
// bộ đệm SpaceBanners 60 s; khóa thiếu web.catalog.read → 403.
func TestWebHomeBanners(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	imgs, processing := e.bannerImages(3)
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	image := func(enabled bool, desktop, mobile uuid.UUID, from, to *time.Time) content.Banner {
		return content.Banner{
			ID: uuid.New(), Enabled: enabled, Kind: content.KindImage, ImageDesktop: desktop, ImageMobile: mobile,
			Alt: "Flash sale", LinkKind: content.LinkOffers, StartsAt: from, EndsAt: to,
		}
	}
	first := image(true, imgs[0], imgs[1], &past, &future)
	second := image(true, imgs[2], uuid.Nil, nil, nil)
	e.saveBanners([]content.Banner{
		image(false, imgs[0], uuid.Nil, nil, nil), // tắt
		first, // hiện, ảnh điện thoại riêng
		image(true, imgs[0], uuid.Nil, nil, &past),             // hết giờ
		image(true, imgs[0], uuid.Nil, &future, nil),           // chưa tới giờ
		image(true, imgs[0], processing, nil, nil),             // ảnh điện thoại đang xử lý
		{ID: uuid.New(), Enabled: true, Kind: content.KindNew}, // không mẫu nào có ảnh
		second, // hiện, điện thoại dùng ảnh máy tính
	})
	rec := e.get(e.key, bannersPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("mong 200, có %d %s", rec.Code, rec.Body.String())
	}
	var l struct {
		Items []struct {
			ID      uuid.UUID `json:"id"`
			Kind    string    `json:"kind"`
			Picture struct{ Desktop, DesktopSmall, Mobile, Alt string }
			Link    struct{ Kind string }
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	if len(l.Items) != 2 || l.Items[0].ID != first.ID || l.Items[1].ID != second.ID {
		t.Fatalf("banner hiện: %s", rec.Body.String())
	}
	a, b := l.Items[0], l.Items[1]
	if !strings.HasPrefix(a.Picture.Desktop, testBase+"/img/") || !strings.HasSuffix(a.Picture.Desktop, "/v1001/2400.webp") ||
		!strings.HasSuffix(a.Picture.DesktopSmall, "/v1001/1200.webp") || a.Picture.Mobile == a.Picture.DesktopSmall ||
		!strings.HasSuffix(a.Picture.Mobile, "/v1001/1200.webp") || a.Picture.Alt != "Flash sale" || a.Link.Kind != "offers" {
		t.Fatalf("banner đầu: %+v", a)
	}
	if b.Picture.Mobile != b.Picture.DesktopSmall {
		t.Fatalf("không ảnh điện thoại phải dùng ảnh máy tính nhỏ: %+v", b)
	}
	if ttl := e.rc.ttl(cache.SpaceBanners); ttl != 60*time.Second {
		t.Fatalf("hạn sống SpaceBanners: %s", ttl)
	}
	tag := rec.Header().Get("ETag")
	if !etagRe.MatchString(tag) {
		t.Fatalf("ETag: %q", tag)
	}
	if rec := e.get(e.key, bannersPath, "If-None-Match", tag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("If-None-Match: %d %q", rec.Code, rec.Body.String())
	}
	if rec := e.get(e.otherKey, bannersPath); rec.Code != http.StatusForbidden {
		t.Fatalf("khóa thiếu quyền: %d", rec.Code)
	}
}
