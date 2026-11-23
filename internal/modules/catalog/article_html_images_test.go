package catalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
)

// countingTx đếm số lần câu ListPublicVariants của kho tệp chạy qua tx (files.PublicVariants gọi đúng câu này; sqlc
// ghi tên câu trong chú thích đầu chuỗi SQL).
type countingTx struct {
	pgx.Tx
	n *int
}

//nolint:ireturn // hiện thực pgx.Tx
func (c countingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "ListPublicVariants") {
		*c.n++
	}
	return c.Tx.Query(ctx, sql, args...) //nolint:wrapcheck // test
}

// articleHTML gọi catalog.ArticleHTML trong tx tenant của e và trả HTML cùng số lần gọi files.PublicVariants.
func (e *catalogEnv) articleHTML(raw string, view catalog.ImageView) (string, int, error) {
	var html string
	n := 0
	err := e.app(func(tx pgx.Tx) error {
		var err error
		html, err = catalog.ArticleHTML(e.t.Context(), countingTx{Tx: tx, n: &n}, json.RawMessage(raw), view)
		return err //nolint:wrapcheck // test
	})
	return html, n, err
}

func imageBlock(id uuid.UUID, caption string) string {
	return fmt.Sprintf(`{"type":"image","fileId":%q,"caption":%q}`, id.String(), caption)
}

var (
	urlAttr = regexp.MustCompile(`(?:src|srcset)="([^"]*)"`)
	v1URL   = regexp.MustCompile(`^` + regexp.QuoteMeta(testBaseURL) + `/img/[0-9a-f]{64}/v1/`)
)

// Khối image chỉ nhận tệp ready, cùng tenant, mục đích product_image, chưa xóa; ngược lại ErrInvalidArticle (422
// invalid_article). Sửa bài viết mà giữ khối ảnh cũ có tệp đã mất vẫn lưu được (chỉ tệp mới được kiểm).
func TestArticleImages_SaveChecksFile(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	b := e.badFiles()
	bad := map[string]uuid.UUID{
		"tệp đang xử lý": b.processing, "tệp nhập Excel": b.imported, "tệp đã xóa": b.deleted, "tệp tenant khác": b.foreign,
		"tệp không có": uuid.New(),
	}
	for name, f := range bad {
		in := dam01(e.seller)
		in.Article = json.RawMessage("[" + imageBlock(f, "x") + "]")
		if _, err := e.create(in); !errors.Is(err, catalog.ErrInvalidArticle) {
			t.Errorf("tạo, %s: mong ErrInvalidArticle, có %v", name, err)
		}
	}
	ready := e.image()
	in := dam01(e.seller)
	in.Article = json.RawMessage(`[{"type":"paragraph","text":"Lụa"},` + imageBlock(ready, "Mặt trước") + "]")
	p, err := e.create(in)
	if err != nil {
		t.Fatalf("tạo với tệp ready: %v", err)
	}
	for name, f := range bad {
		if _, err := e.writerUpdateArticle(p.ID, "["+imageBlock(f, "")+"]"); !errors.Is(err, catalog.ErrInvalidArticle) {
			t.Errorf("sửa, %s: mong ErrInvalidArticle, có %v", name, err)
		}
	}
	// Tệp của khối đã lưu bị mất: sửa tên và sửa bài viết giữ khối đó vẫn được; thêm khối trỏ tệp mất khác thì không.
	e.must(func(tx pgx.Tx) error { filestest.MarkLost(t, tx, ready); return nil })
	if _, err := e.writerUpdateArticle(p.ID, `[{"type":"heading","text":"Mới","level":2},`+imageBlock(ready, "Mặt trước")+"]"); err != nil {
		t.Errorf("giữ khối ảnh cũ có tệp đã mất: %v", err)
	}
	lost := e.image()
	e.must(func(tx pgx.Tx) error { filestest.MarkLost(t, tx, lost); return nil })
	if _, err := e.writerUpdateArticle(p.ID, "["+imageBlock(lost, "")+"]"); !errors.Is(err, catalog.ErrInvalidArticle) {
		t.Errorf("thêm khối trỏ tệp mất: mong ErrInvalidArticle, có %v", err)
	}
}

// TestArticleHTML_WithImages (R7): khối ảnh trỏ tệp ready → đúng một <picture>, mọi đường dẫn là
// FILES_PUBLIC_BASE_URL/img/<sha>/v1/…; tệp mất → bỏ khối, phần còn lại giữ nguyên từng byte; IMAGE_PRESET_VERSION=2
// khi chưa có v2 → v1.
func TestArticleHTML_WithImages(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	f := e.image()
	head := `{"type":"heading","text":"Chất liệu","level":2}`
	para := `{"type":"paragraph","text":"Lụa <b>mềm</b>"}`
	raw := "[" + head + "," + imageBlock(f, "Mặt trước") + "," + para + "]"

	html, n, err := e.articleHTML(raw, testView())
	if err != nil || n != 1 {
		t.Fatalf("ArticleHTML: %v, %d lần PublicVariants", err, n)
	}
	if c := strings.Count(html, "<picture>"); c != 1 || !strings.Contains(html, "<figcaption>Mặt trước</figcaption>") {
		t.Fatalf("mong một <picture> có chú thích: %s", html)
	}
	checkV1URLs(t, html)
	if html2, _, _ := e.articleHTML(raw, catalog.ImageView{BaseURL: testBaseURL, Preset: 2}); html2 != html {
		t.Errorf("IMAGE_PRESET_VERSION=2 khi chưa có v2 phải dùng v1:\n%s\n%s", html2, html)
	}
	// Tệp mất → bằng đúng HTML của cùng bài viết không có khối ảnh.
	plain, err := catalog.DecodeArticle(json.RawMessage("[" + head + "," + para + "]"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := catalog.RenderArticleHTML(plain)
	if err != nil {
		t.Fatal(err)
	}
	e.must(func(tx pgx.Tx) error { filestest.MarkLost(t, tx, f); return nil })
	if lostHTML, _, err := e.articleHTML(raw, testView()); err != nil || lostHTML != want {
		t.Errorf("tệp mất: mong bỏ khối ảnh, phần còn lại giữ nguyên:\n%q\n%q (%v)", lostHTML, want, err)
	}
	if _, _, err := e.articleHTML(`[{"type":"html"}]`, testView()); !errors.Is(err, catalog.ErrInvalidArticle) {
		t.Errorf("bài viết sai lược đồ: mong ErrInvalidArticle, có %v", err)
	}
}

// checkV1URLs: html có 4 thuộc tính src/srcset (2 source, img src, img srcset), mọi đường dẫn là ảnh công khai v1.
func checkV1URLs(t *testing.T, html string) {
	t.Helper()
	attrs := urlAttr.FindAllStringSubmatch(html, -1)
	if len(attrs) != 4 {
		t.Fatalf("mong 4 thuộc tính src/srcset, có %d: %s", len(attrs), html)
	}
	for _, m := range attrs {
		for part := range strings.SplitSeq(m[1], ", ") {
			if u := strings.Fields(part)[0]; !v1URL.MatchString(u) {
				t.Errorf("đường dẫn sai: %s", u)
			}
		}
	}
}

// D4-5: 3 khối ảnh → một lần files.PublicVariants; không khối ảnh hay bài viết rỗng → 0 lần, cả ArticleHTML lẫn
// ResolveArticleImages.
func TestArticleHTML_PublicVariantsCalls(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	three := "[" + imageBlock(e.image(), "a") + "," + imageBlock(e.image(), "") + "," + imageBlock(e.image(), "c") + "]"
	if html, n, err := e.articleHTML(three, testView()); err != nil || n != 1 || strings.Count(html, "<picture>") != 3 {
		t.Errorf("3 khối ảnh: %d lần PublicVariants, %d <picture>, %v", n, strings.Count(html, "<picture>"), err)
	}
	noImage := `[{"type":"heading","text":"A","level":2},{"type":"paragraph","text":"B"}]`
	for name, r := range map[string]string{"không khối ảnh": noImage, "rỗng": "", "mảng rỗng": "[]"} {
		if _, n, err := e.articleHTML(r, testView()); err != nil || n != 0 {
			t.Errorf("ArticleHTML %s: mong 0 lần PublicVariants, có %d (%v)", name, n, err)
		}
		blocks, err := catalog.DecodeArticle(json.RawMessage(r))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		err = e.app(func(tx pgx.Tx) error {
			return catalog.ResolveArticleImages(t.Context(), countingTx{Tx: tx, n: &n}, blocks, testView())
		})
		if err != nil || n != 0 {
			t.Errorf("ResolveArticleImages %s: mong 0 lần, có %d (%v)", name, n, err)
		}
	}
}

// Mutation testing (Task 403): sửa bài viết có khối ảnh cũ đứng trước — mọi tệp mới phía sau vẫn được kiểm, kể cả
// khi giữa chúng có một tệp mới hợp lệ.
func TestArticleImages_SaveChecksEveryNewFile(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	bad := e.badFiles().processing
	old := e.image()
	in := dam01(e.seller)
	in.Article = json.RawMessage("[" + imageBlock(old, "cũ") + "]")
	p, err := e.create(in)
	if err != nil {
		t.Fatalf("tạo: %v", err)
	}
	for name, raw := range map[string]string{
		"cũ rồi tệp hỏng":          "[" + imageBlock(old, "") + "," + imageBlock(bad, "") + "]",
		"cũ, tệp tốt rồi tệp hỏng": "[" + imageBlock(old, "") + "," + imageBlock(e.image(), "") + "," + imageBlock(bad, "") + "]",
	} {
		if _, err := e.writerUpdateArticle(p.ID, raw); !errors.Is(err, catalog.ErrInvalidArticle) {
			t.Errorf("%s: mong ErrInvalidArticle, có %v", name, err)
		}
	}
}

// Mutation testing (Task 403): tệp không còn bản công khai → Image = nil (không phải ảnh rỗng).
func TestResolveArticleImages_MissingFileLeavesNil(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	blocks, err := catalog.DecodeArticle(json.RawMessage("[" + imageBlock(uuid.New(), "") + "]"))
	if err != nil {
		t.Fatal(err)
	}
	e.must(func(tx pgx.Tx) error { return catalog.ResolveArticleImages(t.Context(), tx, blocks, testView()) })
	if blocks[0].Image != nil {
		t.Fatalf("Image = %+v, mong nil", blocks[0].Image)
	}
}
