package files

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// Lỗi của mục đích tệp.
var (
	// ErrUnknownPurpose: mục đích chưa đăng ký bằng RegisterPurpose.
	ErrUnknownPurpose = errors.New("files: mục đích tệp chưa đăng ký")
	// ErrUploadNotAllowed: mục đích không cho nhân viên tải lên (Upload = false).
	ErrUploadNotAllowed = errors.New("files: mục đích này không nhận tải lên")
	// ErrPurposeMode: mục đích chế độ private dùng đường dành cho tệp thường (hay ngược lại), R10.
	ErrPurposeMode = errors.New("files: chế độ của mục đích không cho đường này")
	// ErrPurposeMismatch: sổ mục đích trong Go khác bảng platform.file_purposes.
	ErrPurposeMismatch = errors.New("files: mục đích tệp trong Go khác bảng platform.file_purposes")
)

// purposeKeyPattern khớp CHECK của platform.file_purposes.key.
var purposeKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,40}$`)

// Purpose là một mục đích tệp (Q38): bucket đích, loại và dung lượng được nhận.
type Purpose struct {
	Key          string
	Bucket       Bucket
	AllowedTypes []string
	MaxBytes     int64
	// Image: ảnh, xử lý bằng imager (lệnh file.process hoặc file.process_private).
	Image bool
	// Upload: nhân viên tải lên được qua API.
	Upload bool
	// Private (R10): ảnh riêng tư của khách, chỉ file.process_private xử lý; đòi Bucket = BucketPrivate, Image = true.
	Private bool
	// Preset (Task 453): bộ kích thước riêng của mục đích (≥ purposePresetBase, ví dụ PresetBannerV1); 0 = bộ chung
	// IMAGE_PRESET_VERSION như mọi mục đích có sẵn. Tạo lại kích thước bộ chung bỏ qua tệp của mục đích có bộ riêng.
	Preset int
	// AnyType (M4 Task 155): nhận mọi loại tệp (AllowedTypes phải rỗng); chỉ cho tệp máy chủ ghi, không tải lên, không
	// phải ảnh (tệp khách gửi trong hộp chat: loại do máy chủ nhận dạng theo nội dung, chỉ cho tải về).
	AnyType bool
	// KeyPrefix (Task 515): bắt buộc với BucketExports, là ExportReportsPrefix hay ExportCustomersPrefix (quyết định hạn
	// tự xóa của tệp); rỗng với bucket khác.
	KeyPrefix string
	// MinWidth > 0 (Task 453): ảnh hẹp hơn bị rejected too_small (imager kiểm trước khi giải mã, files kiểm lại kết
	// quả); 0 = không kiểm.
	MinWidth int
}

// presetFor là bộ kích thước mà ảnh của mục đích p dùng khi bộ chung là general.
func (p Purpose) presetFor(general int) int {
	if p.Preset != 0 {
		return p.Preset
	}
	return general
}

// acceptsPreset: bộ v là bộ của mục đích p (bộ riêng đúng p.Preset; mục đích không có bộ riêng chỉ nhận bộ chung).
func (p Purpose) acceptsPreset(v int) bool {
	if p.Preset != 0 {
		return v == p.Preset
	}
	return v < purposePresetBase
}

func (p Purpose) mode() string {
	if p.Private {
		return "private"
	}
	return "standard"
}

func (p Purpose) allows(contentType string) bool {
	return p.AnyType || slices.Contains(p.AllowedTypes, contentType)
}

func (p Purpose) validate() error {
	switch {
	case !purposeKeyPattern.MatchString(p.Key):
		return fmt.Errorf("khóa %q sai dạng", p.Key)
	case !p.Bucket.valid():
		return fmt.Errorf("%s: bucket %q lạ", p.Key, p.Bucket)
	case p.AnyType && (len(p.AllowedTypes) > 0 || p.Upload || p.Image):
		return fmt.Errorf("%s: AnyType chỉ cho tệp máy chủ ghi, không kèm danh sách loại", p.Key)
	case len(p.AllowedTypes) == 0 && !p.AnyType:
		return fmt.Errorf("%s: không có loại tệp nào", p.Key)
	case p.MaxBytes <= 0:
		return fmt.Errorf("%s: MaxBytes phải > 0", p.Key)
	case p.Private && (p.Bucket != BucketPrivate || !p.Image):
		return fmt.Errorf("%s: chế độ private đòi bucket private và Image", p.Key)
	case p.Preset != 0 && (p.Preset < purposePresetBase || !p.Image || p.Private):
		return fmt.Errorf("%s: bộ kích thước riêng phải ≥ %d, cho ảnh công khai", p.Key, purposePresetBase)
	case p.MinWidth < 0 || (p.MinWidth > 0 && !p.Image):
		return fmt.Errorf("%s: MinWidth phải ≥ 0, chỉ cho ảnh", p.Key)
	case p.Bucket == BucketExports && (p.Upload || p.Image):
		return fmt.Errorf("%s: bucket exports chỉ cho tệp máy chủ ghi, không ảnh", p.Key)
	case p.Bucket == BucketExports && p.KeyPrefix != ExportReportsPrefix && p.KeyPrefix != ExportCustomersPrefix:
		return fmt.Errorf("%s: bucket exports đòi KeyPrefix %q hay %q", p.Key, ExportReportsPrefix, ExportCustomersPrefix)
	case p.Bucket != BucketExports && p.KeyPrefix != "":
		return fmt.Errorf("%s: KeyPrefix chỉ cho bucket exports", p.Key)
	}
	return nil
}

func samePurpose(a, b Purpose) bool {
	return a.Key == b.Key && a.Bucket == b.Bucket && slices.Equal(a.AllowedTypes, b.AllowedTypes) &&
		a.MaxBytes == b.MaxBytes && a.Image == b.Image && a.Upload == b.Upload && a.Private == b.Private &&
		a.Preset == b.Preset && a.MinWidth == b.MinWidth && a.AnyType == b.AnyType && a.KeyPrefix == b.KeyPrefix
}

// Registry là sổ mục đích tệp. Dùng sổ mặc định qua RegisterPurpose, PurposeOf; NewRegistry chỉ để test so một tập
// khác với bảng.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Purpose
}

// NewRegistry trả sổ chứa ps; mục đích sai → panic như Register.
func NewRegistry(ps ...Purpose) *Registry {
	r := &Registry{m: map[string]Purpose{}}
	for _, p := range ps {
		r.Register(p)
	}
	return r
}

// Register thêm p. Sai luật (khóa, bucket, loại, MaxBytes, Private ngoài bucket private hay không phải ảnh, AnyType kèm
// loại, tải lên hay ảnh) hoặc khai
// lại cùng khóa khác nội dung → panic (lỗi lập trình, gọi lúc khởi động). Khai lại đúng như cũ thì không làm gì.
func (r *Registry) Register(p Purpose) {
	if err := p.validate(); err != nil {
		panic("files.RegisterPurpose: " + err.Error())
	}
	p.AllowedTypes = slices.Clone(p.AllowedTypes)
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.m[p.Key]; ok {
		if !samePurpose(old, p) {
			panic("files.RegisterPurpose: khai lại " + p.Key + " khác lần trước")
		}
		return
	}
	r.m[p.Key] = p
}

// Lookup trả mục đích theo khóa.
func (r *Registry) Lookup(key string) (Purpose, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.m[key]
	if ok {
		p.AllowedTypes = slices.Clone(p.AllowedTypes)
	}
	return p, ok
}

// Check so sổ với platform.file_purposes: cùng tập khóa, mỗi khóa cùng bucket, cùng mode. Lệch → lỗi bọc
// ErrPurposeMismatch nêu từng khóa (thường là quên RegisterPurpose hay quên dòng INSERT trong migration).
func (r *Registry) Check(ctx context.Context, d *db.DB) error {
	var rows []store.PlatformFilePurpose
	if err := d.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		var err error
		rows, err = store.New(tx).ListFilePurposes(ctx)
		return err //nolint:wrapcheck // bọc ở dưới
	}); err != nil {
		return fmt.Errorf("đọc platform.file_purposes: %w", err)
	}
	r.mu.RLock()
	goKeys := maps.Clone(r.m)
	r.mu.RUnlock()
	var problems []string
	for _, row := range rows {
		p, ok := goKeys[row.Key]
		delete(goKeys, row.Key)
		switch {
		case !ok:
			problems = append(problems, row.Key+": có trong bảng, chưa RegisterPurpose")
		case string(p.Bucket) != row.Bucket || p.mode() != row.Mode:
			problems = append(problems, fmt.Sprintf("%s: Go %s/%s, bảng %s/%s", row.Key, p.Bucket, p.mode(), row.Bucket, row.Mode))
		}
	}
	for _, k := range slices.Sorted(maps.Keys(goKeys)) {
		problems = append(problems, k+": đã RegisterPurpose, thiếu dòng trong bảng")
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrPurposeMismatch, strings.Join(problems, "; "))
	}
	return nil
}

// M1Purposes là các mục đích của M1 (Q11, Q12): ảnh sản phẩm và tệp nhập danh mục.
func M1Purposes() []Purpose {
	return []Purpose{
		{
			Key: "product_image", Bucket: BucketOriginals, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"},
			MaxBytes: 20 << 20, Image: true, Upload: true,
		},
		{
			Key: "catalog_import", Bucket: BucketPrivate,
			AllowedTypes: []string{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
			MaxBytes:     5 << 20, Upload: true,
		},
	}
}

// defaultRegistry là sổ của tiến trình: mục đích M1 có sẵn, mốc sau gọi RegisterPurpose lúc khởi động (trong
// cmd/crm). Interfaces của kế hoạch khai RegisterPurpose, PurposeOf là hàm gói (CreateUpload, Register không nhận
// sổ), nên sổ là biến cấp gói, có khóa.
//
//nolint:gochecknoglobals // Interfaces của kế hoạch M1 Task 41; chỉ đổi qua Register (có khóa, chỉ thêm)
var defaultRegistry = NewRegistry(M1Purposes()...)

// RegisterPurpose thêm p vào sổ của tiến trình (gọi lúc khởi động); luật như Registry.Register.
func RegisterPurpose(p Purpose) { defaultRegistry.Register(p) }

// PurposeOf trả mục đích theo khóa trong sổ của tiến trình.
func PurposeOf(key string) (Purpose, bool) { return defaultRegistry.Lookup(key) }

// CheckPurposes so sổ của tiến trình với bảng (api, worker gọi lúc khởi động).
func CheckPurposes(ctx context.Context, d *db.DB) error { return defaultRegistry.Check(ctx, d) }
