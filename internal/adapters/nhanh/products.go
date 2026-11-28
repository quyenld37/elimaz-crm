package nhanh

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Từ bản chụp (Task 540) sang bản nháp sản phẩm của CRM (kế hoạch M7, Task 541 = M7-41; ADR-48). Phần này thuần: không
// gọi mạng, không database, không module nghiệp vụ. Tên trường của Nhanh theo bản đọc của website
// (apps/storefront-seo/src/server/nhanh/mapping.ts); trường nào thiếu thành Problem (không kèm giá trị) để nhân viên
// soát, không bao giờ làm cả lệnh hỏng.

// ErrSelection báo danh sách mẫu chọn sai (mã không có trong bản chụp, hơn MaxProducts mẫu, danh sách rỗng).
var ErrSelection = errors.New("nhanh: danh sách mẫu chọn sai")

// ErrNoSnapshot báo thư mục chưa có bản chụp nào.
var ErrNoSnapshot = errors.New("nhanh: chưa có bản chụp")

// ErrSnapshotVersion báo bản chụp có khuôn khác SnapshotVersion.
var ErrSnapshotVersion = errors.New("nhanh: bản chụp khác phiên bản đọc được")

// Draft là một mẫu sản phẩm sẵn sàng đưa vào tệp nhập của catalog.
type Draft struct {
	LegacyID         int64
	Code, Name       string
	Description      string
	CategoryLegacyID int64 // 0: không có; bên gọi đổi sang đường danh mục CRM bằng bảng ánh xạ
	Variants         []DraftVariant
	Images           []DraftImage
	Problems         []Problem
}

// DraftVariant là một biến thể. Stock nil: Nhanh không trả tồn của biến thể này. CostPrice nil: không có giá vốn.
type DraftVariant struct {
	LegacyID                  int64
	SKU, Barcode, Color, Size string
	Price                     int64
	CostPrice                 *int64
	Stock                     *int32
}

// DraftImage là một ảnh của mẫu; Color rỗng là ảnh chung (không gắn màu). Mã cũ của ảnh là URL.
type DraftImage struct {
	URL, Color string
}

// Problem là một chỗ thiếu hay phải sửa, không kèm giá trị của Nhanh: Field là tên trường, Note là việc đã làm.
type Problem struct {
	LegacyID int64  `json:"id"`
	Field    string `json:"field"`
	Note     string `json:"note"`
}

// ReadSnapshot đọc bản chụp từ path.
func ReadSnapshot(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: đường dẫn do người chạy lệnh trên server đưa
	if err != nil {
		return nil, fmt.Errorf("đọc bản chụp: %w", err)
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("giải mã bản chụp: %w", err)
	}
	if s.Version != SnapshotVersion {
		return nil, fmt.Errorf("phiên bản %d: %w", s.Version, ErrSnapshotVersion)
	}
	return &s, nil
}

// LatestSnapshot trả đường dẫn bản chụp mới nhất (theo giờ trong tên) trong dir.
func LatestSnapshot(dir string) (string, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoSnapshot
	}
	if err != nil {
		return "", fmt.Errorf("đọc thư mục bản chụp: %w", err)
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if e.Type().IsRegular() && strings.HasPrefix(n, snapshotPrefix) && strings.HasSuffix(n, snapshotSuffix) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "", ErrNoSnapshot
	}
	slices.Sort(names) // giờ UTC định dạng cố định: thứ tự chữ là thứ tự thời gian
	return filepath.Join(dir, names[len(names)-1]), nil
}

// Select trả danh sách mẫu để chuyển: mặc định mọi mẫu của bản chụp (tối đa MaxProducts, như lúc chụp); only khác rỗng
// thì chỉ các mã đó (theo thứ tự only); remove bỏ các mã đó. Mã không có trong bản chụp, danh sách rỗng hay quá
// MaxProducts là ErrSelection. Muốn THÊM mẫu ngoài bản chụp: chụp lại bằng `nhanh-snapshot --product-ids`.
func Select(products []Product, only, remove []int64) ([]Product, error) {
	byID := make(map[int64]Product, len(products))
	for _, p := range products {
		byID[p.ID] = p
	}
	var out []Product
	if len(only) == 0 {
		out = slices.Clone(products)
	}
	for _, id := range only {
		p, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("mã %d không có trong bản chụp (chụp lại bằng --product-ids): %w", id, ErrSelection)
		}
		if !slices.ContainsFunc(out, func(q Product) bool { return q.ID == id }) {
			out = append(out, p)
		}
	}
	for _, id := range remove {
		if _, ok := byID[id]; !ok {
			return nil, fmt.Errorf("mã %d (bỏ) không có trong bản chụp: %w", id, ErrSelection)
		}
	}
	out = slices.DeleteFunc(out, func(p Product) bool { return slices.Contains(remove, p.ID) })
	if len(out) == 0 {
		return nil, fmt.Errorf("không còn mẫu nào: %w", ErrSelection)
	}
	if len(out) > MaxProducts {
		return nil, fmt.Errorf("%d mẫu, tối đa %d: %w", len(out), MaxProducts, ErrSelection)
	}
	return out, nil
}

// flexNum nhận số JSON, chuỗi số hay null (Nhanh trả cả ba tùy trường); không đọc được thì ok = false.
type flexNum struct {
	v  float64
	ok bool
}

func (f *flexNum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil //nolint:nilerr // trường sai khuôn coi như thiếu (thành Problem), không làm hỏng cả bản chụp
	}
	f.v, f.ok = v, true
	return nil
}

func (f flexNum) int64() int64 { return int64(math.Round(f.v)) }

type nhProduct struct {
	ID         flexNum `json:"id"`
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Barcode    string  `json:"barcode"`
	CategoryID flexNum `json:"categoryId"`
	Category   *struct {
		ID flexNum `json:"id"`
	} `json:"category"`
	PriceVatMode flexNum `json:"priceVatMode"`
	Prices       struct {
		Retail    flexNum `json:"retail"`
		RetailVat flexNum `json:"retailVat"`
		Import    flexNum `json:"import"`
	} `json:"prices"`
	Images struct {
		Avatar string   `json:"avatar"`
		Others []string `json:"others"`
	} `json:"images"`
	Attributes []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"attributes"`
	Content     string `json:"content"`
	Description string `json:"description"`
}

type nhInventory struct {
	ID        flexNum `json:"id"`
	ProductID flexNum `json:"productId"`
	Inventory *struct {
		Remain    flexNum `json:"remain"`
		Available flexNum `json:"available"`
	} `json:"inventory"`
}

var (
	colorNames = []string{"màu", "màu sắc", "color"}
	sizeNames  = []string{"size", "kích cỡ", "kích thước", "cỡ"}
	codeBadRe  = regexp.MustCompile(`[^A-Z0-9._-]+`)
	tagRe      = regexp.MustCompile(`<[^>]*>`)
	spaceRe    = regexp.MustCompile(`[ \t]+`)
	barcodeRe  = regexp.MustCompile(`^[0-9A-Za-z-]{4,32}$`)
)

const (
	maxName        = 200
	maxDescription = 1000
	maxOption      = 50
	maxCode        = 64
	maxImageURL    = 512
	maxStock       = 1_000_000
	defaultColor   = "Mặc định"
	defaultSize    = "Free"
	// vatExcluded là priceVatMode 2 của Nhanh: giá bán lẻ chưa gồm VAT (mặc định, cùng luật với website).
	vatExcluded = 2
)

// BuildDrafts đổi các mẫu của bản chụp sang bản nháp. Không bao giờ lỗi: chỗ thiếu thành Problem. SKU chỉ chuẩn hóa
// (chữ hoa, ký tự lạ thành "-"); trùng trong cả đợt thì thêm "-<mã Nhanh>".
func BuildDrafts(products []Product) []Draft {
	out := make([]Draft, 0, len(products))
	seenSKU := map[string]bool{}
	seenBarcode := map[string]bool{}
	for _, p := range products {
		out = append(out, buildDraft(p, seenSKU, seenBarcode))
	}
	return out
}

func buildDraft(p Product, seenSKU, seenBarcode map[string]bool) Draft {
	d := Draft{LegacyID: p.ID}
	var head, detail nhProduct
	_ = json.Unmarshal(p.List, &head)
	_ = json.Unmarshal(p.Detail, &detail)
	stock := stockByID(p.Inventory)
	rows := p.Variants
	perVariant := len(rows) > 0
	if !perVariant {
		rows = []json.RawMessage{p.List} // sản phẩm đơn: chính nó là biến thể duy nhất
	}
	d.Name = clip(strings.TrimSpace(firstNonEmpty(detail.Name, head.Name)), maxName)
	if d.Name == "" {
		d.Name = "Sản phẩm Nhanh " + strconv.FormatInt(p.ID, 10)
		d.problem("name", "thiếu tên, đặt tên tạm")
	}
	if d.Description = description(detail); d.Description == "" {
		d.problem("description", "thiếu mô tả")
	}
	if d.CategoryLegacyID = categoryID(detail, head); d.CategoryLegacyID == 0 {
		d.problem("category", "thiếu danh mục")
	}
	if perVariant {
		d.addImages(firstWithImages(detail, head), "")
	}
	for _, raw := range rows {
		var v nhProduct
		_ = json.Unmarshal(raw, &v)
		vid := p.ID
		if v.ID.ok {
			vid = v.ID.int64()
		}
		dv := d.variant(v, vid, stock)
		dv.SKU = uniqueSKU(&d, dv.SKU, vid, seenSKU)
		dv.Barcode = uniqueBarcode(&d, dv.Barcode, seenBarcode)
		d.Variants = append(d.Variants, dv)
		if perVariant {
			d.addImages(nhProduct{Images: struct {
				Avatar string   `json:"avatar"`
				Others []string `json:"others"`
			}{Avatar: v.Images.Avatar}}, colorOf(dv.Color))
		} else {
			d.addImages(v, "")
		}
	}
	d.Code = sanitizeCode(firstNonEmpty(head.Code, detail.Code), "NHANH-"+strconv.FormatInt(p.ID, 10), &d, "code")
	if len(d.Images) == 0 {
		d.problem("images", "không có ảnh")
	}
	return d
}

func firstWithImages(a, b nhProduct) nhProduct {
	if a.Images.Avatar != "" || len(a.Images.Others) > 0 {
		return a
	}
	return b
}

func colorOf(c string) string {
	if c == defaultColor {
		return ""
	}
	return c
}

func (d *Draft) problem(field, note string) {
	d.Problems = append(d.Problems, Problem{LegacyID: d.LegacyID, Field: field, Note: note})
}

func (d *Draft) variant(v nhProduct, vid int64, stock map[int64]int32) DraftVariant {
	dv := DraftVariant{LegacyID: vid, Barcode: strings.TrimSpace(v.Barcode)}
	dv.SKU = sanitizeCode(v.Code, "NHANH-"+strconv.FormatInt(vid, 10), d, "sku")
	dv.Color = clip(attr(v, colorNames), maxOption)
	dv.Size = clip(attr(v, sizeNames), maxOption)
	if dv.Color == "" {
		dv.Color = defaultColor
		d.problem("color", "thiếu màu, đặt \""+defaultColor+"\"")
	}
	if dv.Size == "" {
		dv.Size = defaultSize
		d.problem("size", "thiếu size, đặt \""+defaultSize+"\"")
	}
	dv.Price = d.customerPrice(v)
	if v.Prices.Import.ok && v.Prices.Import.v >= 0 {
		c := v.Prices.Import.int64()
		dv.CostPrice = &c
	} else {
		d.problem("cost_price", "thiếu giá vốn")
	}
	if n, ok := stock[vid]; ok {
		dv.Stock = &n
	} else {
		d.problem("stock", "Nhanh không trả tồn")
	}
	return dv
}

// customerPrice là giá khách trả: bán lẻ, cộng VAT khi Nhanh để giá chưa gồm VAT.
func (d *Draft) customerPrice(v nhProduct) int64 {
	if !v.Prices.Retail.ok || v.Prices.Retail.v < 0 {
		d.problem("price", "thiếu giá bán, đặt 0")
		return 0
	}
	price := v.Prices.Retail.v
	mode := int64(vatExcluded)
	if v.PriceVatMode.ok {
		mode = v.PriceVatMode.int64()
	}
	if mode == vatExcluded {
		if v.Prices.RetailVat.ok {
			price += v.Prices.RetailVat.v
		} else {
			d.problem("price_vat", "thiếu VAT, lấy giá bán lẻ chưa VAT")
		}
	}
	return int64(math.Round(price))
}

// addImages thêm ảnh đại diện và ảnh phụ của v vào d (bỏ trùng, chỉ https); color gắn cho ảnh vừa thêm.
func (d *Draft) addImages(v nhProduct, color string) {
	for _, u := range append([]string{v.Images.Avatar}, v.Images.Others...) {
		u = strings.TrimSpace(u)
		if u == "" || !strings.HasPrefix(u, "https://") || len(u) > maxImageURL ||
			slices.ContainsFunc(d.Images, func(i DraftImage) bool { return i.URL == u }) {
			continue
		}
		d.Images = append(d.Images, DraftImage{URL: u, Color: color})
	}
}

func uniqueSKU(d *Draft, sku string, id int64, seen map[string]bool) string {
	if seen[sku] {
		sku = clip(sku, maxCode-22) + "-" + strconv.FormatInt(id, 10)
		d.problem("sku", "SKU trùng trong đợt, thêm mã Nhanh")
	}
	seen[sku] = true
	return sku
}

func uniqueBarcode(d *Draft, bc string, seen map[string]bool) string {
	switch {
	case bc == "":
		return ""
	case !barcodeRe.MatchString(bc):
		d.problem("barcode", "mã vạch sai khuôn, bỏ (CRM tự sinh)")
		return ""
	case seen[bc]:
		d.problem("barcode", "mã vạch trùng trong đợt, bỏ (CRM tự sinh)")
		return ""
	}
	seen[bc] = true
	return bc
}

// stockByID: tồn theo mã Nhanh của sản phẩm/biến thể; "remain" (tồn thực), không có thì "available"; kẹp [0, 1e6].
func stockByID(rows []json.RawMessage) map[int64]int32 {
	out := map[int64]int32{}
	for _, raw := range rows {
		var r nhInventory
		if json.Unmarshal(raw, &r) != nil || r.Inventory == nil {
			continue
		}
		id := r.ProductID
		if !id.ok {
			id = r.ID
		}
		q := r.Inventory.Remain
		if !q.ok {
			q = r.Inventory.Available
		}
		if !id.ok || !q.ok {
			continue
		}
		out[id.int64()] = int32(max(0, min(maxStock, q.int64()))) //nolint:gosec // G115: đã kẹp trong [0, 1e6]
	}
	return out
}

func attr(v nhProduct, names []string) string {
	for _, a := range v.Attributes {
		if slices.Contains(names, strings.ToLower(strings.TrimSpace(a.Name))) {
			if s := strings.TrimSpace(a.Value); s != "" {
				return s
			}
		}
	}
	return ""
}

func categoryID(a, b nhProduct) int64 {
	for _, p := range []nhProduct{a, b} {
		if p.CategoryID.ok && p.CategoryID.int64() > 0 {
			return p.CategoryID.int64()
		}
		if p.Category != nil && p.Category.ID.ok && p.Category.ID.int64() > 0 {
			return p.Category.ID.int64()
		}
	}
	return 0
}

// description: mô tả ngắn của Nhanh; không có thì nội dung (HTML) bỏ thẻ; tối đa 1000 ký tự.
func description(d nhProduct) string {
	s := strings.TrimSpace(d.Description)
	if s == "" {
		var kept []string
		for l := range strings.SplitSeq(tagRe.ReplaceAllString(d.Content, "\n"), "\n") {
			if l = strings.TrimSpace(spaceRe.ReplaceAllString(l, " ")); l != "" {
				kept = append(kept, l)
			}
		}
		s = strings.Join(kept, "\n")
	}
	return clip(s, maxDescription)
}

// sanitizeCode đổi s thành mã hợp khuôn của catalog (chữ hoa, số, . _ -, tối đa 64, bắt đầu bằng chữ hay số); rỗng
// thì dùng fallback. Đổi giá trị thì ghi Problem (không kèm giá trị).
func sanitizeCode(s, fallback string, d *Draft, field string) string {
	raw := strings.TrimSpace(s)
	c := clip(strings.TrimLeft(codeBadRe.ReplaceAllString(strings.ToUpper(raw), "-"), "._-"), maxCode)
	if c == "" {
		d.problem(field, "thiếu mã, đặt mã từ mã Nhanh")
		return fallback
	}
	if c != raw {
		d.problem(field, "mã đổi cho hợp khuôn")
	}
	return c
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// clip cắt s còn tối đa n ký tự (rune).
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
