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
	"elimaz.com.vn/crm/internal/platform/queue"
)

const looksPath = "/api/v1/web/content/looks"

// lookPiece tạo mẫu đang bán (3 cỡ, 1 ngừng bán) có một ảnh sản phẩm đã xử lý; stock: nạp tồn cho cỡ đầu ở kho MAIN.
func (e *env) lookPiece(code string, stock bool) (product, photo uuid.UUID) {
	e.t.Helper()
	p := e.product(code, "[]")
	photo = e.image()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position)
			VALUES ($1, $2, $3, 1)`, e.tenant, p.ID, photo)
		return err //nolint:wrapcheck // test
	})
	if stock {
		e.stock(p.Variants[0].ID, 5)
	}
	return p.ID, photo
}

func (e *env) saveLooks(list []content.Look, rev int) {
	e.t.Helper()
	e.app(func(tx pgx.Tx) error {
		_, err := content.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}.SaveLooks(e.t.Context(), tx, list, rev,
			content.Actor{TenantID: e.tenant, UserID: e.owner.UserID}, time.Now())
		return err //nolint:wrapcheck // test
	})
}

type webLook struct {
	ID          uuid.UUID `json:"id"`
	LinkKey     string    `json:"linkKey"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Title       string    `json:"title"`
	Model       string    `json:"model"`
	OnHome      bool      `json:"onHome"`
	CategoryIDs []string  `json:"categoryIds"`
	Photo       struct {
		FileID   uuid.UUID `json:"fileId"`
		Alt      string    `json:"alt"`
		Variants []struct {
			URL string `json:"url"`
		} `json:"variants"`
	} `json:"photo"`
	Items []struct {
		ProductID uuid.UUID `json:"productId"`
		Color     string    `json:"color"`
		X         float64   `json:"x"`
		Y         float64   `json:"y"`
	} `json:"items"`
}

type webLookList struct {
	Items  []webLook `json:"items"`
	Hidden []struct {
		ID          uuid.UUID `json:"id"`
		Reason      string    `json:"reason"`
		ProductID   uuid.UUID `json:"productId"`
		ProductName string    `json:"productName"`
	} `json:"hidden"`
}

func (e *env) webLooks(home string) (webLookList, http.Header) {
	e.t.Helper()
	rec := e.get(e.key, looksPath+"?home="+home)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("home=%s: mong 200, có %d %s", home, rec.Code, rec.Body.String())
	}
	var out webLookList
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		e.t.Fatal(err)
	}
	return out, rec.Header()
}

func ids(ls []webLook) []uuid.UUID {
	out := make([]uuid.UUID, len(ls))
	for i, l := range ls {
		out[i] = l.ID
	}
	return out
}

// [KM04] Bộ phối cho website: home=true chỉ bộ đang hiện trên trang chủ theo vị trí (≤ 6), kèm tiêu đề tự động, dòng
// người mẫu, ảnh (bộ v1), link; hidden gồm bộ trang chủ bị ẩn vì hết hàng (website báo Telegram), không gồm bộ hết giờ;
// home=false mọi bộ mở được bằng link (kể cả bộ không lên trang chủ); ETag → 304; bộ đệm SpaceLooks 60 s; khóa thiếu
// quyền 403.
func TestWebHomeLooks(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	pA, photoA := e.lookPiece("LKA", true)
	pB, _ := e.lookPiece("LKB", true)
	pOut, photoOut := e.lookPiece("LKO", false)
	past := time.Now().Add(-time.Hour)
	mk := func(home bool, name string, photo uuid.UUID, ps ...uuid.UUID) content.Look {
		l := content.Look{ID: uuid.New(), OnHome: home, Name: name, PhotoFileID: photo}
		for i, p := range ps {
			l.Items = append(l.Items, content.LookItem{ProductID: p, Color: "Đen", X: 40, Y: float64(20 + 40*i)})
		}
		return l
	}
	shown := mk(true, "Đi làm ngày thường", photoA, pA, pB)
	offHome := mk(false, "Hẹn sau giờ làm", photoA, pA)
	expired := mk(true, "Hết giờ", photoA, pA)
	expired.EndsAt = &past
	soldOut := mk(true, "Cuối tuần", photoOut, pOut)
	titled := mk(true, "Họp quan trọng", photoA, pA)
	titled.Title, titled.ModelHeightCM, titled.ModelSize = "Sơ mi và quần", 162, "S"
	e.saveLooks([]content.Look{shown, offHome, expired, soldOut, titled}, 0)

	home, hdr := e.webLooks("true")
	if got := ids(home.Items); len(got) != 2 || got[0] != shown.ID || got[1] != titled.ID {
		t.Fatalf("trang chủ: %v", got)
	}
	a := home.Items[0]
	if a.Title != "Đầm LKA, đầm LKB" || a.Model != "" || a.LinkKey != content.LookLinkKey(shown.ID) ||
		a.Slug != "di-lam-ngay-thuong" || !a.OnHome || a.Photo.FileID != photoA || a.Photo.Alt != "Đi làm ngày thường: Đầm LKA, đầm LKB" ||
		len(a.Photo.Variants) == 0 || !v1URL.MatchString(a.Photo.Variants[0].URL) || len(a.Items) != 2 ||
		a.Items[0].ProductID != pA || a.Items[0].Color != "Đen" || a.Items[1].Y != 60 || a.CategoryIDs == nil {
		t.Fatalf("bộ đầu: %+v", a)
	}
	if b := home.Items[1]; b.Title != "Sơ mi và quần" || b.Model != "Mẫu 1m62 · size S" {
		t.Fatalf("bộ có tiêu đề: %+v", b)
	}
	if len(home.Hidden) != 1 || home.Hidden[0].ID != soldOut.ID || home.Hidden[0].Reason != content.LookHiddenSoldOut ||
		home.Hidden[0].ProductID != pOut || home.Hidden[0].ProductName != "Đầm LKO" {
		t.Fatalf("bộ trang chủ bị ẩn: %+v", home.Hidden)
	}
	if ttl := e.rc.ttl(cache.SpaceLooks); ttl != 60*time.Second {
		t.Fatalf("hạn sống SpaceLooks: %s", ttl)
	}
	tag := hdr.Get("ETag")
	if !etagRe.MatchString(tag) {
		t.Fatalf("ETag: %q", tag)
	}
	if rec := e.get(e.key, looksPath+"?home=true", "If-None-Match", tag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("If-None-Match: %d %q", rec.Code, rec.Body.String())
	}
	all, _ := e.webLooks("false")
	if got := ids(all.Items); len(got) != 3 || got[0] != shown.ID || got[1] != offHome.ID || got[2] != titled.ID ||
		all.Items[1].OnHome || len(all.Hidden) != 0 {
		t.Fatalf("mọi bộ mở bằng link: %v, hidden %+v", got, all.Hidden)
	}
	if rec := e.get(e.otherKey, looksPath+"?home=true"); rec.Code != http.StatusForbidden {
		t.Fatalf("khóa thiếu quyền: %d", rec.Code)
	}
}

// [KM04] Trang /bo-phoi: bộ không lên trang chủ vẫn 200 (link key chữ hoa cũng được); bộ hết giờ, mã lạ → 404; bộ đã xóa
// khỏi danh sách → 404.
func TestWebLook(t *testing.T) {
	t.Parallel()
	e := newEnv(t, nil)
	pA, photoA := e.lookPiece("LKA", true)
	past := time.Now().Add(-time.Hour)
	off := content.Look{ID: uuid.New(), Name: "Đứng lớp cả ngày", PhotoFileID: photoA, Items: []content.LookItem{{ProductID: pA, X: 50, Y: 50}}}
	expired := off
	expired.ID, expired.EndsAt = uuid.New(), &past
	e.saveLooks([]content.Look{off, expired}, 0)

	key := content.LookLinkKey(off.ID)
	for _, k := range []string{key, strings.ToUpper(key)} {
		rec := e.get(e.key, looksPath+"/"+k)
		if rec.Code != http.StatusOK {
			t.Fatalf("bộ không lên trang chủ %s: %d %s", k, rec.Code, rec.Body.String())
		}
		var l webLook
		if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		if l.ID != off.ID || l.OnHome || l.Slug != "dung-lop-ca-ngay" {
			t.Fatalf("bộ: %+v", l)
		}
	}
	for name, k := range map[string]string{"hết giờ": content.LookLinkKey(expired.ID), "mã lạ": "0badbeef"} {
		if rec := e.get(e.key, looksPath+"/"+k); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found"`) {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	e.saveLooks(nil, 1)
	if rec := e.get(e.key, looksPath+"/"+key); rec.Code != http.StatusNotFound {
		t.Fatalf("bộ đã xóa: %d", rec.Code)
	}
	if rec := e.get(e.otherKey, looksPath+"/"+key); rec.Code != http.StatusForbidden {
		t.Fatalf("khóa thiếu quyền: %d", rec.Code)
	}
}
