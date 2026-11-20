package catalog_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/catalog"
)

func fileID() *uuid.UUID {
	id := uuid.MustParse("0193a4b2-7c3d-7e4f-8a9b-0c1d2e3f4a5b")
	return &id
}

// fullImage: đủ 12 biến thể (4 chiều rộng × avif, webp, jpeg), xếp lộn xộn để kiểm thứ tự đầu ra.
func fullImage() *catalog.ArticleImage {
	img := &catalog.ArticleImage{}
	for _, f := range []string{"jpeg", "webp", "avif"} {
		for _, w := range []int{1600, 320, 1080, 640} {
			img.Variants = append(img.Variants, catalog.ArticleImageVariant{
				Width: w, Format: f, URL: fmt.Sprintf("https://cdn.elimaz.test/img/abc/v1/%d.%s", w, f),
			})
		}
	}
	return img
}

func goldenBlocks() []catalog.ArticleBlock {
	return []catalog.ArticleBlock{
		{Type: "heading", Text: "Chất liệu & form", Level: 2},
		{Type: "paragraph", Text: "Lụa mềm, mặc mát.\nGiặt tay nước lạnh."},
		{Type: "list", Items: []string{"Màu đỏ", "Màu đen", "Size S–L"}},
		{Type: "image", FileID: fileID(), Caption: "Mặt trước \"đầm\"", Image: fullImage()},
		{Type: "heading", Text: "Bảo quản", Level: 3},
	}
}

func TestRenderArticleHTML_Golden(t *testing.T) {
	got, err := catalog.RenderArticleHTML(goldenBlocks())
	if err != nil {
		t.Fatalf("RenderArticleHTML: %v", err)
	}
	path := filepath.Join("testdata", "article", "golden.html")
	if os.Getenv("UPDATE_GOLDEN") != "" { // UPDATE_GOLDEN=1 go test -run Golden ghi lại tệp
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("HTML khác tệp golden:\n có: %s\nmong: %s", got, want)
	}
	again, err := catalog.RenderArticleHTML(goldenBlocks())
	if err != nil || again != got {
		t.Errorf("gọi lần hai phải ra đúng chuỗi cũ (err %v)", err)
	}

	noImage := []catalog.ArticleBlock{{Type: "paragraph", Text: "x"}, {Type: "image", FileID: fileID(), Caption: "chưa có ảnh"}}
	out, err := catalog.RenderArticleHTML(noImage)
	if err != nil || out != "<p>x</p>" {
		t.Errorf("khối image chưa có Image phải bị bỏ: %q, %v", out, err)
	}
	empty, err := catalog.RenderArticleHTML(nil)
	if err != nil || empty != "" {
		t.Errorf("bài viết rỗng phải ra \"\": %q, %v", empty, err)
	}
	onlyWebp := []catalog.ArticleBlock{{Type: "image", FileID: fileID(), Image: &catalog.ArticleImage{Variants: []catalog.ArticleImageVariant{
		{Width: 640, Format: "webp", URL: "https://cdn.elimaz.test/a.webp"},
	}}}}
	if out, err := catalog.RenderArticleHTML(onlyWebp); err != nil || strings.Contains(out, "<figure") {
		t.Errorf("không có bản JPEG thì bỏ khối: %q, %v", out, err)
	}
	nearest := []catalog.ArticleBlock{{Type: "image", FileID: fileID(), Image: &catalog.ArticleImage{Variants: []catalog.ArticleImageVariant{
		{Width: 320, Format: "jpeg", URL: "https://cdn.elimaz.test/320.jpeg"},
		{Width: 1080, Format: "jpeg", URL: "https://cdn.elimaz.test/1080.jpeg"},
	}}}}
	out, err = catalog.RenderArticleHTML(nearest)
	if err != nil || !strings.Contains(out, `<img src="https://cdn.elimaz.test/320.jpeg"`) {
		t.Errorf("thiếu JPEG 640 thì lấy bản JPEG gần nhất (320 cách 320, 1080 cách 440): %q, %v", out, err)
	}
	local := []catalog.ArticleBlock{{Type: "image", FileID: fileID(), Image: &catalog.ArticleImage{Variants: []catalog.ArticleImageVariant{
		{Width: 640, Format: "jpeg", URL: "http://localhost:8333/a.jpeg"},
	}}}}
	if _, err := catalog.RenderArticleHTML(local); err != nil {
		t.Errorf("http chỉ cho localhost: %v", err)
	}
}

func TestRenderArticleHTML_EscapesInjection(t *testing.T) {
	attacks := []string{
		"<script>alert(1)</script>",
		"<img src=x onerror=alert(1)>",
		`"><svg onload=alert(1)>`,
	}
	var blocks []catalog.ArticleBlock
	for _, a := range attacks {
		blocks = append(blocks,
			catalog.ArticleBlock{Type: "paragraph", Text: a},
			catalog.ArticleBlock{Type: "heading", Text: a, Level: 2})
	}
	blocks = append(blocks,
		catalog.ArticleBlock{Type: "list", Items: []string{"</li><script>", "ok"}},
		catalog.ArticleBlock{Type: "image", FileID: fileID(), Caption: `" onerror="alert(1)`, Image: fullImage()})
	out, err := catalog.RenderArticleHTML(blocks)
	if err != nil {
		t.Fatalf("RenderArticleHTML: %v", err)
	}
	lower := strings.ToLower(out)
	// Thẻ thật không có; "onerror=" chỉ còn là chữ đã thoát (trong văn bản hay trong giá trị alt), còn tên thuộc
	// tính của mọi thẻ do checkAllowed kiểm theo danh sách cho phép.
	for _, bad := range []string{"<script", "<svg", "<img src=x", " onerror=\"", " onload=\""} {
		if strings.Contains(lower, bad) {
			t.Errorf("đầu ra chứa %q:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("chữ phải hiện dạng đã thoát:\n%s", out)
	}
	if !strings.Contains(out, `alt="&#34; onerror=&#34;alert(1)"`) {
		t.Errorf("alt không được thoát khỏi dấu nháy:\n%s", out)
	}
	checkAllowed(t, out)

	badURL := func(u, format string) []catalog.ArticleBlock {
		return []catalog.ArticleBlock{{Type: "image", FileID: fileID(), Image: &catalog.ArticleImage{Variants: []catalog.ArticleImageVariant{
			{Width: 640, Format: format, URL: u},
		}}}}
	}
	rejects := map[string][]catalog.ArticleBlock{
		"javascript:":     badURL("javascript:alert(1)", "jpeg"),
		"data:":           badURL("data:image/svg+xml,<svg onload=alert(1)>", "jpeg"),
		"không có scheme": badURL("//evil.example/a.jpg", "jpeg"),
		"userinfo":        badURL("https://u:p@cdn.example/a.jpg", "jpeg"),
		"http ngoài máy":  badURL("http://cdn.example/a.jpg", "jpeg"),
		"định dạng svg":   badURL("https://cdn.example/a.svg", "svg"),
		"dấu phẩy":        badURL("https://cdn.example/a,b.jpg", "jpeg"),
		"khoảng trắng":    badURL("https://cdn.example/a b.jpg", "jpeg"),
		"chiều rộng 0": {{Type: "image", FileID: fileID(), Image: &catalog.ArticleImage{Variants: []catalog.ArticleImageVariant{
			{Width: 0, Format: "jpeg", URL: "https://cdn.example/a.jpg"},
		}}}},
		"level 4":   {{Type: "heading", Text: "x", Level: 4}},
		"type html": {{Type: "html", Text: "<b>x</b>"}},
		"NUL":       {{Type: "paragraph", Text: "a\u0000b"}},
		"201 khối":  slices.Repeat([]catalog.ArticleBlock{{Type: "paragraph", Text: "x"}}, 201),
	}
	for name, b := range rejects {
		out, err := catalog.RenderArticleHTML(b)
		if !errors.Is(err, catalog.ErrInvalidArticle) || out != "" {
			t.Errorf("%s: mong ErrInvalidArticle và chuỗi rỗng, có %q, %v", name, out, err)
		}
	}
}

// fataler: phần chung của *testing.T và *rapid.T mà checkAllowed cần.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

// allowedAttrs: thẻ được phép → thuộc tính được phép (kế hoạch R7).
func allowedAttrs() map[string][]string {
	return map[string][]string{
		"p": nil, "br": nil, "h2": nil, "h3": nil, "ul": nil, "li": nil, "figure": nil, "picture": nil, "figcaption": nil,
		"source": {"type", "srcset", "sizes"},
		"img":    {"src", "srcset", "sizes", "alt", "loading", "decoding"},
	}
}

// checkAllowed là bộ tách thẻ nhỏ: mọi '<' mở một thẻ (chữ đã thoát nên không có '<' trong chữ), thẻ thuộc danh sách
// cho phép, thuộc tính thuộc danh sách của thẻ, giá trị thuộc tính trong dấu nháy kép không chứa '"', '<', '>'.
func checkAllowed(t fataler, out string) {
	t.Helper()
	allow := allowedAttrs()
	rest := out
	for {
		i := strings.IndexByte(rest, '<')
		if i < 0 {
			break
		}
		if strings.ContainsRune(rest[:i], '>') {
			t.Fatalf("'>' ngoài thẻ: %q", out)
		}
		j := strings.IndexByte(rest[i:], '>')
		if j < 0 {
			t.Fatalf("thẻ không đóng: %q", out)
		}
		tag := rest[i+1 : i+j]
		rest = rest[i+j+1:]
		name, attrs, _ := strings.Cut(strings.TrimPrefix(tag, "/"), " ")
		okAttrs, ok := allow[name]
		if !ok {
			t.Fatalf("thẻ %q ngoài danh sách cho phép: %q", name, out)
		}
		if strings.HasPrefix(tag, "/") && attrs != "" {
			t.Fatalf("thẻ đóng có thuộc tính: %q", tag)
		}
		checkAttrs(t, name, tag, attrs, okAttrs)
	}
	if strings.ContainsRune(rest, '>') {
		t.Fatalf("'>' ngoài thẻ: %q", out)
	}
}

// checkAttrs: mỗi thuộc tính dạng key="value", key thuộc okAttrs, value không có '"', '<', '>'.
func checkAttrs(t fataler, name, tag, attrs string, okAttrs []string) {
	t.Helper()
	for attrs != "" {
		key, after, found := strings.Cut(attrs, `="`)
		if !found {
			t.Fatalf("thuộc tính không có giá trị trong nháy kép: %q", tag)
		}
		if !slices.Contains(okAttrs, key) {
			t.Fatalf("thuộc tính %q của <%s> ngoài danh sách: %q", key, name, tag)
		}
		val, rest, found := strings.Cut(after, `"`)
		if !found || strings.ContainsAny(val, "<>") {
			t.Fatalf("giá trị thuộc tính hỏng: %q", tag)
		}
		attrs = strings.TrimPrefix(rest, " ")
	}
}

func genBlock() *rapid.Generator[catalog.ArticleBlock] {
	anyText := rapid.OneOf(rapid.String(), rapid.SampledFrom([]string{
		"<script>alert(1)</script>", `"><svg onload=alert(1)>`, "</li><script>", "a\nb", "\u0000", "&amp;", "'",
	}))
	anyURL := rapid.OneOf(rapid.String(), rapid.SampledFrom([]string{
		"https://cdn.elimaz.test/a.jpeg", "javascript:alert(1)", "data:text/html,<x>", "//evil/a", "https://u:p@x/a",
		"http://localhost/a.jpeg", "https://cdn.elimaz.test/a\".jpeg", "https://x/a,1 2w",
	}))
	return rapid.Custom(func(t *rapid.T) catalog.ArticleBlock {
		b := catalog.ArticleBlock{
			Type:  rapid.SampledFrom([]string{"paragraph", "heading", "list", "image", "html", ""}).Draw(t, "type"),
			Text:  anyText.Draw(t, "text"),
			Level: rapid.IntRange(0, 5).Draw(t, "level"),
		}
		if rapid.Bool().Draw(t, "hasItems") {
			b.Items = rapid.SliceOfN(anyText, 0, 4).Draw(t, "items")
		}
		if rapid.Bool().Draw(t, "hasFile") {
			b.FileID = fileID()
			b.Caption = anyText.Draw(t, "caption")
		}
		if rapid.Bool().Draw(t, "hasImage") {
			b.Image = &catalog.ArticleImage{}
			for range rapid.IntRange(0, 4).Draw(t, "nVariants") {
				b.Image.Variants = append(b.Image.Variants, catalog.ArticleImageVariant{
					Width:  rapid.SampledFrom([]int{0, 320, 640, 1080, -1}).Draw(t, "w"),
					Format: rapid.SampledFrom([]string{"jpeg", "webp", "avif", "svg", ""}).Draw(t, "f"),
					URL:    anyURL.Draw(t, "url"),
				})
			}
		}
		return b
	})
}

// Quy luật (500 lượt, TestMain): khối bất kỳ → hoặc ErrInvalidArticle và chuỗi rỗng, hoặc chỉ thẻ và thuộc tính
// trong danh sách cho phép; không panic.
func TestRenderArticleHTML_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		blocks := rapid.SliceOfN(genBlock(), 0, 6).Draw(rt, "blocks")
		out, err := catalog.RenderArticleHTML(blocks)
		if err != nil {
			if !errors.Is(err, catalog.ErrInvalidArticle) || out != "" {
				rt.Fatalf("lỗi phải là ErrInvalidArticle kèm chuỗi rỗng: %q, %v", out, err)
			}
			return
		}
		checkAllowed(rt, out)
	})
}

// FuzzRenderArticleHTML: đầu vào là JSON bài viết (qua DecodeArticle) cộng một URL ảnh; chạy 60 s cục bộ bằng
// go test -run '^$' -fuzz FuzzRenderArticleHTML -fuzztime 60s ./internal/modules/catalog.
func FuzzRenderArticleHTML(f *testing.F) {
	seeds, err := json.Marshal(goldenBlocks())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(string(seeds), "https://cdn.elimaz.test/a.jpeg", "jpeg")
	f.Add(`[{"type":"paragraph","text":"<script>alert(1)</script>"}]`, "javascript:alert(1)", "jpeg")
	f.Add(`[{"type":"list","items":["</li><script>"]}]`, "//evil.example/a.jpg", "svg")
	f.Add(`[{"type":"image","fileId":"0193a4b2-7c3d-7e4f-8a9b-0c1d2e3f4a5b","caption":"\" onerror=\"alert(1)"}]`, "https://u:p@cdn.example/a.jpg", "webp")
	f.Add(`[{"type":"heading","text":"x","level":4}]`, "data:image/svg+xml,x", "avif")
	f.Fuzz(func(t *testing.T, raw, u, format string) {
		blocks, err := catalog.DecodeArticle(json.RawMessage(raw))
		if err != nil {
			if !errors.Is(err, catalog.ErrInvalidArticle) {
				t.Fatalf("DecodeArticle trả lỗi lạ: %v", err)
			}
			return
		}
		for i := range blocks {
			if blocks[i].Type == "image" {
				blocks[i].Image = &catalog.ArticleImage{Variants: []catalog.ArticleImageVariant{{Width: 640, Format: format, URL: u}}}
			}
		}
		out, err := catalog.RenderArticleHTML(blocks)
		if err != nil {
			if !errors.Is(err, catalog.ErrInvalidArticle) || out != "" {
				t.Fatalf("lỗi phải là ErrInvalidArticle kèm chuỗi rỗng: %q, %v", out, err)
			}
			return
		}
		checkAllowed(t, out)
	})
}

// Mutation testing (Task 403): chiều rộng biến thể 1 và 10000 là biên hợp lệ; 0, 10001 và URL có khoảng trắng →
// ErrInvalidArticle.
func TestRenderArticleHTML_VariantBounds(t *testing.T) {
	img := func(w int, url string) []catalog.ArticleBlock {
		return []catalog.ArticleBlock{{Type: "image", FileID: fileID(), Image: &catalog.ArticleImage{
			Variants: []catalog.ArticleImageVariant{{Width: w, Format: "jpeg", URL: url}},
		}}}
	}
	const u = "https://cdn.elimaz.test/img/abc/v1/x.jpeg"
	for name, c := range map[string]struct {
		blocks []catalog.ArticleBlock
		ok     bool
	}{
		"rộng 1":           {img(1, u), true},
		"rộng 10000":       {img(10000, u), true},
		"rộng 0":           {img(0, u), false},
		"rộng 10001":       {img(10001, u), false},
		"URL khoảng trắng": {img(640, "https://cdn.elimaz.test/img/a b.jpeg"), false},
	} {
		_, err := catalog.RenderArticleHTML(c.blocks)
		if (err == nil) != c.ok || (err != nil && !errors.Is(err, catalog.ErrInvalidArticle)) {
			t.Errorf("%s: lỗi %v, mong hợp lệ = %v", name, err, c.ok)
		}
	}
}

// Mutation testing (Task 403): img src là bản JPEG gần 640 nhất, kể cả khi bản đó rộng hơn 640.
func TestRenderArticleHTML_SrcClosestTo640(t *testing.T) {
	var vs []catalog.ArticleImageVariant
	for _, w := range []int{320, 800} {
		vs = append(vs, catalog.ArticleImageVariant{Width: w, Format: "jpeg", URL: fmt.Sprintf("https://cdn.elimaz.test/%d.jpeg", w)})
	}
	out, err := catalog.RenderArticleHTML([]catalog.ArticleBlock{{Type: "image", FileID: fileID(), Image: &catalog.ArticleImage{Variants: vs}}})
	if err != nil || !strings.Contains(out, `<img src="https://cdn.elimaz.test/800.jpeg"`) {
		t.Fatalf("mong src bản 800: %v\n%s", err, out)
	}
}
