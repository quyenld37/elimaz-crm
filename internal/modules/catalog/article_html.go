package catalog

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/article"
)

// Bộ dựng HTML bài viết (R7): chỗ duy nhất trong CRM sinh HTML từ bài viết dạng khối; website (M5) dùng cùng hàm.
// Thẻ được phép: p, br, h2, h3, ul, li, figure, picture, source, img, figcaption; thuộc tính: source[type, srcset,
// sizes], img[src, srcset, sizes, alt, loading, decoding]. Mọi chữ qua html/template (thoát theo ngữ cảnh).

// articleTemplates: khuôn cố định cho từng loại khối. Không có chỗ nào chèn chuỗi người dùng ngoài {{…}}.
const articleTemplates = `
{{- define "paragraph"}}<p>{{range $i, $l := .}}{{if $i}}<br>{{end}}{{$l}}{{end}}</p>{{end -}}
{{- define "h2"}}<h2>{{.}}</h2>{{end -}}
{{- define "h3"}}<h3>{{.}}</h3>{{end -}}
{{- define "list"}}<ul>{{range .}}<li>{{.}}</li>{{end}}</ul>{{end -}}
{{- define "image"}}<figure><picture>
{{- if .AVIF}}<source type="image/avif" srcset="{{.AVIF}}" sizes="{{.Sizes}}">{{end}}
{{- if .WebP}}<source type="image/webp" srcset="{{.WebP}}" sizes="{{.Sizes}}">{{end -}}
<img src="{{.Src}}" srcset="{{.JPEG}}" sizes="{{.Sizes}}" alt="{{.Alt}}" loading="lazy" decoding="async"></picture>
{{- if .Alt}}<figcaption>{{.Alt}}</figcaption>{{end}}</figure>{{end -}}
`

const (
	imageSizes   = "(max-width: 640px) 100vw, 640px"
	defaultWidth = 640
	maxWidth     = 10000
)

type imageData struct {
	AVIF, WebP, JPEG, Src, Sizes, Alt string
}

// DecodeArticle đọc cột article (hay thân yêu cầu) thành các khối, chỉ kiểm dạng (article.Validate). Sai →
// ErrInvalidArticle.
func DecodeArticle(raw json.RawMessage) ([]ArticleBlock, error) {
	return article.Validate(raw) //nolint:wrapcheck // ErrInvalidArticle là lỗi của gói article
}

// ArticleHTML dựng HTML bài viết cho màn hình và website từ cột article (raw): DecodeArticle → ResolveArticleImages →
// RenderArticleHTML, trong tx tenant. Bài viết không có khối image → không đọc kho tệp. Lỗi: ErrInvalidArticle.
func ArticleHTML(ctx context.Context, tx pgx.Tx, raw json.RawMessage, view ImageView) (string, error) {
	blocks, err := DecodeArticle(raw)
	if err != nil {
		return "", err
	}
	if err := ResolveArticleImages(ctx, tx, blocks, view); err != nil {
		return "", err
	}
	return RenderArticleHTML(blocks)
}

// ResolveArticleImages điền Image cho các khối image của blocks (tại chỗ) bằng **một** lời gọi files.PublicVariants
// cho mọi fileId, chọn bộ kích thước như ảnh sản phẩm (view.Preset, files.SelectPreset). Tệp không ready, đã xóa, mất,
// riêng tư hay của tenant khác → Image = nil (RenderArticleHTML bỏ khối). Không có khối image → không đọc kho tệp
// (D4-5: trang nhiều sản phẩm không tốn thêm câu SQL cho bài viết không có ảnh).
func ResolveArticleImages(ctx context.Context, tx pgx.Tx, blocks []ArticleBlock, view ImageView) error {
	ids := articleFileIDs(blocks)
	for i := range blocks {
		blocks[i].Image = nil
	}
	if len(ids) == 0 {
		return nil
	}
	picked, err := publicImageVariants(ctx, tx, ids, view)
	if err != nil {
		return err
	}
	for i := range blocks {
		if blocks[i].Type != article.TypeImage || blocks[i].FileID == nil {
			continue
		}
		if vs := picked[*blocks[i].FileID]; len(vs) > 0 {
			blocks[i].Image = &ArticleImage{Variants: vs}
		}
	}
	return nil
}

// articleFileIDs: fileId khác nhau của các khối image theo thứ tự xuất hiện.
func articleFileIDs(blocks []ArticleBlock) []uuid.UUID {
	var ids []uuid.UUID
	for _, b := range blocks {
		if b.Type == article.TypeImage && b.FileID != nil && !slices.Contains(ids, *b.FileID) {
			ids = append(ids, *b.FileID)
		}
	}
	return ids
}

// checkArticleFiles kiểm tệp của các khối image trong blocks mà prev (bài viết đang lưu, nil khi tạo) chưa có: tệp
// phải có trong tenant, chưa xóa, ready, mục đích product_image (luật của gắn ảnh sản phẩm). Sai → ErrInvalidArticle.
// Tệp đã có trong prev không kiểm lại: tệp bị mất sau khi lưu không chặn nhân viên sửa phần khác (khối đó bị bỏ lúc
// dựng HTML).
func checkArticleFiles(ctx context.Context, tx pgx.Tx, blocks, prev []ArticleBlock) error {
	old := articleFileIDs(prev)
	for _, id := range articleFileIDs(blocks) {
		if slices.Contains(old, id) {
			continue
		}
		err := checkImageFile(ctx, tx, id)
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrFileNotReady) {
			return ErrInvalidArticle
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// RenderArticleHTML dựng HTML của bài viết: hàm thuần (không mạng, không database), cùng đầu vào → cùng chuỗi byte.
// paragraph → <p> (xuống dòng thành <br>); heading → <h2>/<h3>; list → <ul><li>; image có Image → <figure><picture>
// (source AVIF, WebP; img là JPEG rộng 640 hay bản JPEG gần nhất; srcset theo chiều rộng); image chưa có Image hay
// không có bản JPEG → bỏ khối. Kiểm lại phòng thủ (luật của article.Validate, URL https — http chỉ cho localhost,
// 127.0.0.1 —, không userinfo, định dạng avif|webp|jpeg, chiều rộng 1..10000): sai → ErrInvalidArticle và chuỗi rỗng.
// Bài viết rỗng → "".
func RenderArticleHTML(blocks []ArticleBlock) (string, error) {
	if len(blocks) == 0 {
		return "", nil
	}
	if err := article.Check(blocks); err != nil {
		return "", err //nolint:wrapcheck // ErrInvalidArticle
	}
	tmpl, err := template.New("article").Parse(articleTemplates)
	if err != nil {
		return "", fmt.Errorf("dựng khuôn bài viết: %w", err)
	}
	var b strings.Builder
	for _, blk := range blocks {
		var name string
		var data any
		switch blk.Type {
		case article.TypeParagraph:
			name, data = "paragraph", strings.Split(blk.Text, "\n")
		case article.TypeHeading:
			name, data = "h"+strconv.Itoa(blk.Level), blk.Text
		case article.TypeList:
			name, data = "list", blk.Items
		case article.TypeImage:
			img, ok, err := imageFor(blk)
			if err != nil {
				return "", err
			}
			if !ok {
				continue
			}
			name, data = "image", img
		}
		if err := tmpl.ExecuteTemplate(&b, name, data); err != nil {
			return "", fmt.Errorf("dựng khối %s: %w", blk.Type, err)
		}
	}
	return b.String(), nil
}

// imageFor chuẩn bị dữ liệu của khối image; ok = false khi chưa có ảnh hay không có bản JPEG (bỏ khối).
func imageFor(blk ArticleBlock) (imageData, bool, error) {
	if blk.Image == nil {
		return imageData{}, false, nil
	}
	byFormat := map[string][]ArticleImageVariant{}
	for _, v := range blk.Image.Variants {
		if v.Width < 1 || v.Width > maxWidth || !safeImageURL(v.URL) {
			return imageData{}, false, ErrInvalidArticle
		}
		switch v.Format {
		case "avif", "webp", "jpeg":
			byFormat[v.Format] = append(byFormat[v.Format], v)
		default:
			return imageData{}, false, ErrInvalidArticle
		}
	}
	jpegs := byFormat["jpeg"]
	if len(jpegs) == 0 {
		return imageData{}, false, nil
	}
	src := slices.MinFunc(jpegs, func(a, b ArticleImageVariant) int {
		return cmp.Or(cmp.Compare(distance(a.Width), distance(b.Width)), cmp.Compare(a.Width, b.Width))
	})
	return imageData{
		AVIF: srcset(byFormat["avif"]), WebP: srcset(byFormat["webp"]), JPEG: srcset(jpegs), Src: src.URL,
		Sizes: imageSizes, Alt: blk.Caption,
	}, true, nil
}

func distance(w int) int {
	if w > defaultWidth {
		return w - defaultWidth
	}
	return defaultWidth - w
}

// srcset: "<url> <rộng>w" theo chiều rộng tăng dần (giữ thứ tự đầu vào khi bằng nhau), nối bằng ", ".
func srcset(vs []ArticleImageVariant) string {
	sorted := slices.Clone(vs)
	slices.SortStableFunc(sorted, func(a, b ArticleImageVariant) int { return cmp.Compare(a.Width, b.Width) })
	parts := make([]string, 0, len(sorted))
	for _, v := range sorted {
		parts = append(parts, v.URL+" "+strconv.Itoa(v.Width)+"w")
	}
	return strings.Join(parts, ", ")
}

// safeImageURL: chỉ ký tự ASCII in được, không dấu phẩy, nháy, khoảng trắng, ngoặc nhọn, gạch ngược (srcset tách theo
// dấu phẩy và khoảng trắng); https có máy chủ, hoặc http tới localhost, 127.0.0.1; không userinfo.
func safeImageURL(s string) bool {
	if s == "" || strings.ContainsFunc(s, func(r rune) bool {
		return r <= 0x20 || r >= 0x7f || strings.ContainsRune(`,"'<>\`+"`", r)
	}) {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.User != nil || u.Opaque != "" || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Hostname() != ""
	case "http":
		h := u.Hostname()
		return h == "localhost" || h == "127.0.0.1"
	}
	return false
}
