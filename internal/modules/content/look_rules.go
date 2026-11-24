package content

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// Luật bộ phối "Hôm nay mặc gì?" chép từ website (server/looks/rules.ts, features/looks/look-link.ts) và admin
// (lib/looks.mjs) — Task 454.

// Giới hạn của danh sách bộ phối (MAX_LOOKS, MAX_HOME_LOOKS, MAX_LOOK_ITEMS và lookSchema của website).
const (
	MaxLooks          = 500
	MaxHomeLooks      = 6
	MaxLookItems      = 3
	MaxLookName       = 28
	MaxLookTitle      = 60
	MaxLookHeadline   = 90
	MaxLookStory      = 220
	MaxLookColor      = 60
	MaxModelSize      = 6
	MaxLookCategories = 6
	MinModelHeight    = 130
	MaxModelHeight    = 200
)

// Lý do bộ không hiện riêng của bộ phối (LookHidden của website); disabled, scheduled, expired, product-missing dùng
// chung hằng với banner (cùng chữ).
const (
	LookHiddenSoldOut      = "sold-out"
	LookHiddenPhotoMissing = "photo-missing"
)

// lookKeyLen, lookSlugMax: liên kết /bo-phoi/<slug ≤ 60>-<8 hex đầu của id> (LOOK_KEY_LENGTH, slugify(name, 60)).
const (
	lookKeyLen  = 8
	lookSlugMax = 60
)

// LookHiddenReasons trả 6 lý do ẩn theo thứ tự kiểm của website.
func LookHiddenReasons() []string {
	reasons := [...]string{
		HiddenDisabled, HiddenScheduled, HiddenExpired, HiddenProductMissing, LookHiddenSoldOut, LookHiddenPhotoMissing,
	}
	return reasons[:]
}

// LookItem là một món trong ảnh: mẫu, màu đang mặc (rỗng = màu đầu tiên còn hàng, website tự chọn), chấm trên ảnh theo
// % chiều rộng (X) và chiều cao (Y), 0–100, làm tròn 2 chữ số.
type LookItem struct {
	ProductID uuid.UUID
	Color     string
	X, Y      float64
}

// Look là một bộ phối. OnHome: hiện trên trang chủ (≤ MaxHomeLooks); bộ nào cũng mở được bằng link. Title rỗng = tên
// các món (LookAutoTitle); Headline, Story rỗng = không hiện. PhotoFileID là mã tệp của một ảnh sản phẩm của các món.
// ModelHeightCM 0 = không ghi. CategoryIDs rỗng = danh mục của các món (LookCategories).
type Look struct {
	ID                           uuid.UUID
	OnHome                       bool
	Name, Title, Headline, Story string
	PhotoFileID                  uuid.UUID
	CategoryIDs                  []uuid.UUID
	ModelHeightCM                int
	ModelSize                    string
	StartsAt, EndsAt             *time.Time
	Items                        []LookItem
}

// ForLink trả bản sao của l để tính hiện/ẩn trên trang link của bộ (/bo-phoi/…): bộ không lên trang chủ vẫn hiện
// (ignoreEnabled của website).
func ForLink(l Look) Look {
	l.OnHome = true
	return l
}

// LookProduct là một mẫu (chưa xóa) mà danh sách nhắc. OnSale: active, người bán hoạt động, có biến thể bán được giá
// > 0. InStock: có biến thể bán được còn hàng ở kho mặc định (bên gọi đọc, content không đọc view inventory).
type LookProduct struct {
	ID         uuid.UUID
	Code, Name string
	CategoryID uuid.UUID
	OnSale     bool
	InStock    bool
}

// LookImage là một ảnh đang hiện của một mẫu được nhắc. Ready: đã xử lý xong (có biến thể của bộ kích thước web);
// Variants có khi Ready.
type LookImage struct {
	ProductID, FileID uuid.UUID
	Color             *string
	Position          int
	Ready             bool
	Variants          []catalog.ImageVariant
}

// LookColor là một màu của mẫu cho ô màu của món (Task 488): tên như của biến thể, mã "#RRGGBB" (rỗng khi không có).
type LookColor struct {
	Name, Hex string
}

// LookRefData là dữ liệu module khác mà luật bộ phối cần (LoadLookRefData). Products chỉ gồm mẫu chưa xóa mà danh sách
// nhắc (mã không có = đã xóa hay của tenant khác); Images là ảnh của các mẫu đó; Categories là danh mục đang dùng;
// Colors là màu bán được của từng mẫu (chỉ để màn hình chọn màu, luật không dùng).
type LookRefData struct {
	Products   map[uuid.UUID]LookProduct
	Images     []LookImage
	Categories map[uuid.UUID]CategoryRef
	Colors     map[uuid.UUID][]LookColor
}

// PhotoOf trả ảnh người mẫu của l nếu nó là ảnh đang hiện của một món của l.
func (r LookRefData) PhotoOf(l Look) (LookImage, bool) { return r.photo(l.PhotoFileID, l.productIDs()) }

// photo trả ảnh fileID nếu nó là ảnh của một mẫu trong owners.
func (r LookRefData) photo(fileID uuid.UUID, owners []uuid.UUID) (LookImage, bool) {
	for _, im := range r.Images {
		if im.FileID == fileID && slices.Contains(owners, im.ProductID) {
			return im, true
		}
	}
	return LookImage{}, false
}

// LookStatus là kết quả hiện/ẩn của một bộ ở một thời điểm. ProductID: món gây ra product-missing, sold-out.
// AutoTitle: tiêu đề tự động (để màn hình hiện thay cho ô trống).
type LookStatus struct {
	ID        uuid.UUID
	Shown     bool
	Reason    string
	ProductID uuid.UUID
	AutoTitle string
}

// productIDs trả mã các món theo thứ tự.
func (l Look) productIDs() []uuid.UUID {
	out := make([]uuid.UUID, len(l.Items))
	for i, it := range l.Items {
		out[i] = it.ProductID
	}
	return out
}

// LookProductIDs trả mã mọi món của ls (bỏ trùng, theo mã tăng dần).
func LookProductIDs(ls []Look) []uuid.UUID {
	var ids []uuid.UUID
	for _, l := range ls {
		ids = append(ids, l.productIDs()...)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return slices.Compact(ids)
}

// NormalizeLooks trả bản sao của ls đã chuẩn hóa: cắt khoảng trắng hai đầu mọi chữ, chấm làm tròn 2 chữ số (cột
// numeric(5,2)), danh mục bỏ trùng giữ thứ tự, mảng nil thành rỗng. Không kẹp chấm ngoài 0–100 (ValidateLooks từ
// chối). Không sửa ls.
func NormalizeLooks(ls []Look) []Look {
	out := make([]Look, len(ls))
	for i, l := range ls {
		l.Name, l.Title, l.Headline, l.Story, l.ModelSize = strings.TrimSpace(l.Name), strings.TrimSpace(l.Title),
			strings.TrimSpace(l.Headline), strings.TrimSpace(l.Story), strings.TrimSpace(l.ModelSize)
		cats := make([]uuid.UUID, 0, len(l.CategoryIDs))
		for _, c := range l.CategoryIDs {
			if !slices.Contains(cats, c) {
				cats = append(cats, c)
			}
		}
		l.CategoryIDs = cats
		items := make([]LookItem, len(l.Items))
		for k, it := range l.Items {
			it.Color = strings.TrimSpace(it.Color)
			it.X, it.Y = round2(it.X), round2(it.Y)
			items[k] = it
		}
		l.Items = items
		out[i] = l
	}
	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// ValidateLooks kiểm danh sách đã chuẩn hóa (NormalizeLooks) theo luật của website: tối đa MaxLooks bộ, tối đa
// MaxHomeLooks bộ trên trang chủ, mã và link key khác nhau; từng bộ có tên, chữ không quá dài, 1–3 món khác nhau, chấm
// trong ảnh, khung giờ đúng chiều, ảnh là ảnh của một món trong bộ. prev là danh sách đang lưu: bộ có trong prev mà
// không đổi ảnh và món thì không kiểm lại ảnh (ảnh sản phẩm bị xóa sau đó chỉ làm bộ ẩn photo-missing, không chặn lưu
// cả danh sách). Món không cần đang bán (bộ ẩn product-missing). Lỗi đầu tiên được trả, bọc "bộ phối <n>".
func ValidateLooks(ls []Look, ref LookRefData, prev []Look) error {
	if err := validateLookList(ls); err != nil {
		return err
	}
	old := lookIndex(prev)
	for i, l := range ls {
		if err := validateLook(l, ref, old); err != nil {
			return fmt.Errorf("bộ phối %d: %w", i+1, err)
		}
	}
	return nil
}

// LookSkip là một bộ bị bỏ khi xem trước vì chưa đủ để lưu; Err là sentinel lưu sẽ trả cho bộ đó (ErrInvalidLook,
// ErrLookItems, ErrLookDot, ErrLookPhoto, ErrLookWindow).
type LookSkip struct {
	ID  uuid.UUID
	Err error
}

// PreviewLooks tách danh sách đã chuẩn hóa để xem trước (Task 488): lỗi của cả danh sách (quá MaxLooks, quá MaxHomeLooks
// bộ trang chủ, mã rỗng/trùng/trùng link) vẫn trả lỗi như ValidateLooks; bộ nào chưa đủ (luật từng bộ của ValidateLooks)
// thì bị bỏ, ghi vào skipped kèm lỗi, các bộ còn lại trả theo thứ tự trong ok. Một bộ đang điền dở không làm hỏng cả
// lần xem trước.
func PreviewLooks(ls []Look, ref LookRefData, prev []Look) (ok []Look, skipped []LookSkip, err error) {
	if err := validateLookList(ls); err != nil {
		return nil, nil, err
	}
	old := lookIndex(prev)
	ok, skipped = make([]Look, 0, len(ls)), []LookSkip{}
	for _, l := range ls {
		if err := validateLook(l, ref, old); err != nil {
			skipped = append(skipped, LookSkip{ID: l.ID, Err: err})
			continue
		}
		ok = append(ok, l)
	}
	return ok, skipped, nil
}

// validateLookList kiểm luật của cả danh sách: số bộ, số bộ trang chủ, mã và link key khác nhau, không rỗng.
func validateLookList(ls []Look) error {
	if len(ls) > MaxLooks {
		return fmt.Errorf("%d bộ phối: %w", len(ls), ErrTooManyLooks)
	}
	home := 0
	for _, l := range ls {
		if l.OnHome {
			home++
		}
	}
	if home > MaxHomeLooks {
		return fmt.Errorf("%d bộ trên trang chủ: %w", home, ErrTooManyHomeLooks)
	}
	ids, keys := map[uuid.UUID]bool{}, map[string]bool{}
	for i, l := range ls {
		if l.ID == uuid.Nil || ids[l.ID] || keys[LookLinkKey(l.ID)] {
			return fmt.Errorf("bộ phối %d: mã rỗng, trùng hay trùng link: %w", i+1, ErrInvalidLook)
		}
		ids[l.ID], keys[LookLinkKey(l.ID)] = true, true
	}
	return nil
}

func lookIndex(ls []Look) map[uuid.UUID]Look {
	out := make(map[uuid.UUID]Look, len(ls))
	for _, l := range ls {
		out[l.ID] = l
	}
	return out
}

func validateLook(l Look, ref LookRefData, old map[uuid.UUID]Look) error {
	if err := validateLookShape(l); err != nil {
		return err
	}
	if l.StartsAt != nil && l.EndsAt != nil && !l.EndsAt.After(*l.StartsAt) {
		return ErrLookWindow
	}
	if len(l.Items) == 0 || len(l.Items) > MaxLookItems {
		return ErrLookItems
	}
	seen := make([]uuid.UUID, 0, len(l.Items))
	for _, it := range l.Items {
		if it.ProductID == uuid.Nil || slices.Contains(seen, it.ProductID) {
			return ErrLookItems
		}
		seen = append(seen, it.ProductID)
		if !inPercent(it.X) || !inPercent(it.Y) {
			return ErrLookDot
		}
	}
	if l.PhotoFileID == uuid.Nil {
		return ErrLookPhoto
	}
	if p, ok := old[l.ID]; ok && p.PhotoFileID == l.PhotoFileID && sameProducts(p, l) {
		return nil
	}
	if _, ok := ref.photo(l.PhotoFileID, seen); !ok {
		return ErrLookPhoto
	}
	return nil
}

// validateLookShape kiểm độ dài chữ (theo ký tự), chiều cao người mẫu, số danh mục.
func validateLookShape(l Look) error {
	n := utf8.RuneCountInString
	tooLong := n(l.Title) > MaxLookTitle || n(l.Headline) > MaxLookHeadline || n(l.Story) > MaxLookStory ||
		n(l.ModelSize) > MaxModelSize
	for _, it := range l.Items {
		tooLong = tooLong || n(it.Color) > MaxLookColor
	}
	switch {
	case l.Name == "", n(l.Name) > MaxLookName, tooLong, len(l.CategoryIDs) > MaxLookCategories,
		l.ModelHeightCM != 0 && (l.ModelHeightCM < MinModelHeight || l.ModelHeightCM > MaxModelHeight):
		return ErrInvalidLook
	}
	return nil
}

func inPercent(v float64) bool { return v >= 0 && v <= 100 } // NaN → false

// sameProducts: hai bộ có cùng tập món.
func sameProducts(a, b Look) bool {
	x, y := a.productIDs(), b.productIDs()
	if len(x) != len(y) {
		return false
	}
	for _, id := range x {
		if !slices.Contains(y, id) {
			return false
		}
	}
	return true
}

// LookVisibility cho biết bộ l có hiện ở now không, và lý do khi ẩn — đúng thứ tự explainLooks của website: không lên
// trang chủ (disabled; trang link dùng ForLink), chưa tới giờ, hết giờ, một món không đang bán (product-missing), một
// món hết hàng (sold-out), ảnh không còn là ảnh đã xử lý của một món (photo-missing).
func LookVisibility(l Look, ref LookRefData, now time.Time) (shown bool, reason string) {
	reason, _ = lookHidden(l, ref, now)
	return reason == "", reason
}

// lookHidden trả lý do ẩn (rỗng khi hiện) và món gây ra product-missing, sold-out.
func lookHidden(l Look, ref LookRefData, now time.Time) (string, uuid.UUID) {
	switch {
	case !l.OnHome:
		return HiddenDisabled, uuid.Nil
	case l.StartsAt != nil && now.Before(*l.StartsAt):
		return HiddenScheduled, uuid.Nil
	case l.EndsAt != nil && !now.Before(*l.EndsAt):
		return HiddenExpired, uuid.Nil
	}
	for _, it := range l.Items {
		if p, ok := ref.Products[it.ProductID]; !ok || !p.OnSale {
			return HiddenProductMissing, it.ProductID
		}
	}
	for _, it := range l.Items {
		if !ref.Products[it.ProductID].InStock {
			return LookHiddenSoldOut, it.ProductID
		}
	}
	if im, ok := ref.PhotoOf(l); !ok || !im.Ready {
		return LookHiddenPhotoMissing, uuid.Nil
	}
	return "", uuid.Nil
}

// LookStatuses trả hiện/ẩn của từng bộ của ls theo thứ tự, kèm món gây ra và tiêu đề tự động.
func LookStatuses(ls []Look, ref LookRefData, now time.Time) []LookStatus {
	out := make([]LookStatus, len(ls))
	for i, l := range ls {
		reason, pid := lookHidden(l, ref, now)
		out[i] = LookStatus{ID: l.ID, Shown: reason == "", Reason: reason, ProductID: pid, AutoTitle: LookAutoTitle(l, ref)}
	}
	return out
}

// LookAutoTitle là tiêu đề khi ô tiêu đề trống: tên các món có trong ref theo thứ tự, cách nhau ", ", từ món thứ hai
// chữ đầu viết thường ("Sơ mi dài tay nhún vai, quần suông"; autoTitle của website).
func LookAutoTitle(l Look, ref LookRefData) string {
	var parts []string
	for _, it := range l.Items {
		p, ok := ref.Products[it.ProductID]
		if !ok {
			continue
		}
		name := p.Name
		if len(parts) > 0 {
			if r, size := utf8.DecodeRuneInString(name); size > 0 {
				name = string(unicode.ToLower(r)) + name[size:]
			}
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, ", ")
}

// LookModelLine là dòng người mẫu trên ảnh: "Mẫu 1m62 · size S", "Mẫu 1m62", "Mẫu mặc size S" hay rỗng (modelLine của
// website). height 0 = không ghi.
func LookModelLine(height int, size string) string {
	h := ""
	if height > 0 {
		h = fmt.Sprintf("%dm%02d", height/100, height%100)
	}
	switch {
	case h != "" && size != "":
		return "Mẫu " + h + " · size " + size
	case h != "":
		return "Mẫu " + h
	case size != "":
		return "Mẫu mặc size " + size
	}
	return ""
}

// LookCategories là danh mục cho "Bán chạy cho dịp này": danh mục đã chọn, trống thì danh mục của các món có trong
// ref (bỏ trùng); chỉ giữ danh mục đang dùng hoặc là danh mục của một món (như website).
func LookCategories(l Look, ref LookRefData) []uuid.UUID {
	var own []uuid.UUID
	for _, it := range l.Items {
		if p, ok := ref.Products[it.ProductID]; ok && p.CategoryID != uuid.Nil && !slices.Contains(own, p.CategoryID) {
			own = append(own, p.CategoryID)
		}
	}
	wanted := l.CategoryIDs
	if len(wanted) == 0 {
		wanted = own
	}
	out := []uuid.UUID{}
	for _, id := range wanted {
		if _, ok := ref.Categories[id]; ok || slices.Contains(own, id) {
			out = append(out, id)
		}
	}
	return out
}

// LookLinkKey là 8 hex đầu của id (chữ thường): phần tìm bộ của liên kết (lookKey của website).
func LookLinkKey(id uuid.UUID) string {
	return strings.ReplaceAll(id.String(), "-", "")[:lookKeyLen]
}

// LookSlug là phần tên của liên kết bộ: textnorm.Slug(name, 60) (slugify(name, 60) của website).
func LookSlug(name string) string { return textnorm.Slug(name, lookSlugMax) }

// LookPath là đường dẫn trang bộ trên website: /bo-phoi/<slug>-<link key> (tên không còn chữ thì chỉ có mã).
func LookPath(id uuid.UUID, name string) string {
	if s := LookSlug(name); s != "" {
		return "/bo-phoi/" + s + "-" + LookLinkKey(id)
	}
	return "/bo-phoi/" + LookLinkKey(id)
}
