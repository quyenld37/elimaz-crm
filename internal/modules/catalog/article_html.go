package catalog

import (
	"cmp"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"slices"
	"strconv"
	"strings"

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
