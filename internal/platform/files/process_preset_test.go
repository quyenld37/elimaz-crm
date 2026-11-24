package files_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/imager"
	"elimaz.com.vn/crm/internal/platform/files"
)

// smallSource: nội dung ảnh thử mà imager giả trả ảnh tốt rộng 500 px (imager cũ không biết MinWidth, Task 453).
const smallSource = "SMALL"

// testBannerPurpose là mục đích có bộ kích thước riêng (như web_banner của content, Task 453): bộ PresetBannerV1, rộng
// tối thiểu 600 px.
func testBannerPurpose() files.Purpose {
	return files.Purpose{
		Key: "test_banner", Bucket: files.BucketOriginals, AllowedTypes: []string{"image/jpeg", "image/png", "image/webp"},
		MaxBytes: 8 * mib, Image: true, Upload: true, Preset: files.PresetBannerV1, MinWidth: 600,
	}
}

func (p *procEnv) withBannerPurpose() {
	p.t.Helper()
	files.RegisterPurpose(testBannerPurpose())
	if err := p.addPurposeRow("test_banner", "originals", "standard"); err != nil {
		p.t.Fatal(err)
	}
}

// Bộ banner-v1: 1200 và 2400 px × WebP, JPEG chất lượng 82; imager nhận MinWidth 600; tệp ghi preset 1001, đường dẫn
// img/<sha>/v1001/. Ảnh sản phẩm cùng worker vẫn bộ v1 (12 biến thể, không MinWidth).
func TestFileProcess_PurposePreset(t *testing.T) {
	p := newProcEnv(t)
	p.withBannerPurpose()
	body := []byte("banner-flash-sale")
	id := p.upload(p.a, "test_banner", body)
	product := p.upload(p.a, "product_image", []byte("anh-san-pham"))
	p.drain()
	r := p.fileRow(id)
	if r.Status != files.StatusReady || r.Preset == nil || *r.Preset != files.PresetBannerV1 {
		t.Fatalf("tệp banner: %+v", r)
	}
	checkBannerVariants(t, p.variants(id))
	checkPresetRequests(t, p.img.requests())
	if pr := p.fileRow(product); pr.Preset == nil || *pr.Preset != 1 || len(p.variants(product)) != 12 {
		t.Fatalf("ảnh sản phẩm: %+v", pr)
	}
	// SelectPreset theo bộ của banner trả đúng 4 biến thể.
	if vs := files.SelectPreset(p.info(p.a, id).Variants, files.PresetBannerV1); len(vs) != 4 {
		t.Fatalf("SelectPreset(1001): %+v", vs)
	}
	// Cùng ảnh banner lần hai: dùng lại tệp đầu (tìm trùng theo bộ của mục đích), không gọi imager thêm.
	calls := p.img.count()
	again := p.upload(p.a, "test_banner", body)
	p.drain()
	if p.img.count() != calls || len(p.variants(again)) != 4 {
		t.Fatalf("lần hai: imager %d→%d lần, %d biến thể", calls, p.img.count(), len(p.variants(again)))
	}
}

// Ảnh hẹp hơn MinWidth → rejected too_small: imager mới từ chối (REJ:too_small); imager cũ trả ok rộng 500 thì files
// tự từ chối, không ghi biến thể.
func TestFileProcess_TooSmall(t *testing.T) {
	p := newProcEnv(t)
	p.withBannerPurpose()
	byImager := p.upload(p.a, "test_banner", []byte(rejPrefix+imager.ReasonTooSmall+"\n"))
	byFiles := p.upload(p.a, "test_banner", []byte(smallSource))
	smallProduct := p.upload(p.a, "product_image", []byte(smallSource))
	p.drain()
	p.makeDue()
	p.drain()
	for _, c := range []struct {
		name string
		r    fileRow
	}{{"imager", p.fileRow(byImager)}, {"files", p.fileRow(byFiles)}} {
		if c.r.Status != files.StatusRejected || c.r.Reason != imager.ReasonTooSmall {
			t.Fatalf("%s: %+v", c.name, c.r)
		}
	}
	if n := len(p.variants(byFiles)); n != 0 {
		t.Fatalf("tệp bị từ chối có %d biến thể", n)
	}
	// Mục đích không khai MinWidth: ảnh 500 px vẫn ready.
	if r := p.fileRow(smallProduct); r.Status != files.StatusReady {
		t.Fatalf("ảnh sản phẩm 500 px: %+v", r)
	}
}

// Tạo lại kích thước bản v2 (bộ chung) bỏ qua tệp của mục đích có bộ riêng: tệp banner không nhận biến thể v2.
func TestRegenerate_SkipsPurposeWithOwnPreset(t *testing.T) {
	p := newProcEnv(t)
	p.withBannerPurpose()
	banner := p.upload(p.a, "test_banner", []byte("banner-regen"))
	p.drain()
	v1, _ := files.Preset(1)
	bv1, _ := files.Preset(files.PresetBannerV1)
	files.SetPresetsForTest(t, map[int][]files.VariantSpec{1: v1, 2: v1[:3], files.PresetBannerV1: bv1})
	if _, err := p.regenerate(p.a, 2); err != nil {
		t.Fatal(err)
	}
	p.drain()
	if n, _ := p.presetRows(banner, 2); n != 0 {
		t.Fatalf("tệp banner nhận %d biến thể v2", n)
	}
	if n, _ := p.presetRows(banner, files.PresetBannerV1); n != 4 {
		t.Fatalf("tệp banner còn %d biến thể của bộ riêng", n)
	}
}

// Luật khai mục đích: bộ riêng phải ≥ 1000 (không lẫn số IMAGE_PRESET_VERSION), MinWidth không âm, chỉ cho ảnh.
func TestPurpose_PresetRules(t *testing.T) {
	ok := testBannerPurpose()
	files.NewRegistry(ok)
	for name, bad := range map[string]files.Purpose{
		"bộ nhỏ":         func() files.Purpose { p := ok; p.Preset = 2; return p }(),
		"MinWidth âm":    func() files.Purpose { p := ok; p.MinWidth = -1; return p }(),
		"không phải ảnh": func() files.Purpose { p := ok; p.Image = false; return p }(),
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s: phải panic", name)
				}
			}()
			files.NewRegistry(bad)
		}()
	}
	// Khai lại cùng khóa khác bộ → panic (samePurpose so cả Preset, MinWidth).
	r := files.NewRegistry(ok)
	other := ok
	other.MinWidth = 700
	defer func() {
		if recover() == nil {
			t.Fatal("khai lại khác MinWidth phải panic")
		}
	}()
	r.Register(other)
}

// checkPresetRequests: yêu cầu imager của ảnh banner có MinWidth 600, MaxBytes 8 MiB, 4 đầu ra chất lượng 82; của ảnh
// sản phẩm vẫn 12 đầu ra, không MinWidth, chất lượng mặc định.
func checkPresetRequests(t *testing.T, reqs []imager.Request) {
	t.Helper()
	var banner, prod *imager.Request
	for i := range reqs {
		if len(reqs[i].Outputs) == 4 {
			banner = &reqs[i]
		} else {
			prod = &reqs[i]
		}
	}
	if banner == nil || prod == nil {
		t.Fatalf("yêu cầu imager: %+v", reqs)
	}
	if banner.MinWidth != 600 || banner.MaxBytes != 8*mib {
		t.Fatalf("yêu cầu banner: %+v", banner)
	}
	for _, o := range banner.Outputs {
		if o.Quality != 82 {
			t.Fatalf("đầu ra banner %d.%s chất lượng %d, muốn 82", o.Width, o.Format, o.Quality)
		}
	}
	if prod.MinWidth != 0 || len(prod.Outputs) != 12 || prod.Outputs[0].Quality != 0 {
		t.Fatalf("yêu cầu ảnh sản phẩm đổi: %+v", prod)
	}
}

// checkBannerVariants: đúng 4 biến thể của bộ banner-v1 (1200, 2400 × WebP, JPEG) trên public, khóa /v1001/.
func checkBannerVariants(t *testing.T, vs []variantRow) {
	t.Helper()
	var got []string
	for _, v := range vs {
		if v.Preset != files.PresetBannerV1 || !strings.Contains(v.Key, "/v1001/") || v.Bucket != "public" {
			t.Fatalf("biến thể: %+v", v)
		}
		got = append(got, v.Format+":"+strconv.Itoa(v.Width))
	}
	slices.Sort(got)
	if want := []string{"jpeg:1200", "jpeg:2400", "webp:1200", "webp:2400"}; !slices.Equal(got, want) {
		t.Fatalf("biến thể %v, muốn %v", got, want)
	}
}
