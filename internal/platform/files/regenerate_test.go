package files_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/platform/files"
)

const kindRegenerate = "file.regenerate"

// presetsWithV2 là bảng bộ kích thước thử của TP07: v1 như Q13, v2 thêm chiều rộng 1200 (15 biến thể).
func presetsWithV2(t *testing.T) []files.VariantSpec {
	t.Helper()
	v1, ok := files.Preset(1)
	if !ok {
		t.Fatal("không có bộ kích thước v1")
	}
	var v2 []files.VariantSpec
	for _, w := range []int{320, 640, 1080, 1200, 1600} {
		for _, f := range []string{"avif", "webp", "jpeg"} {
			v2 = append(v2, files.VariantSpec{Width: w, Format: f})
		}
	}
	files.SetPresetsForTest(t, map[int][]files.VariantSpec{1: v1, 2: v2})
	return v2
}

// regenerate xếp lệnh tạo lại bộ kích thước preset cho tenant (như POST /api/v1/platform/files/regenerate).
func (p *procEnv) regenerate(tenant uuid.UUID, preset int) (int, error) {
	var n int
	err := p.inTenant(tenant, func(tx pgx.Tx) error {
		var err error
		n, err = files.EnqueueRegenerate(p.t.Context(), tx, p.q, preset)
		return err //nolint:wrapcheck // test
	})
	return n, err
}

// regenJobs đếm lệnh file.regenerate của tệp id cho bản preset (DedupeKey = mã tệp + ":v" + bản).
func (p *procEnv) regenJobs(id uuid.UUID, preset string) int {
	p.t.Helper()
	return p.staff.Count(p.t, "SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2",
		kindRegenerate, id.String()+":v"+preset)
}

// presetRows đếm dòng biến thể của bản preset của tệp id và cho biết có chiều rộng 1200 không.
func (p *procEnv) presetRows(id uuid.UUID, preset int) (n int, has1200 bool) {
	p.t.Helper()
	for _, v := range p.variants(id) {
		if v.Preset == preset {
			n++
			has1200 = has1200 || v.Width == 1200
		}
	}
	return n, has1200
}

// productWithImage tạo mẫu của tenant A có một ảnh (tệp fileID) và trả người bán, mẫu.
func (p *procEnv) productWithImage(fileID uuid.UUID) (seller, product uuid.UUID) {
	p.t.Helper()
	p.staff.Exec(p.t, p.a, func(tx pgx.Tx) error {
		seller = catalogtest.Seller(p.t, tx, p.a, "ELIMAZ", "own")
		product = catalogtest.Product(p.t, tx, p.a, seller, "AO-01")
		_, err := tx.Exec(p.t.Context(), `INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position)
			VALUES ($1, $2, $3, 1)`, p.a, product, fileID)
		return err //nolint:wrapcheck // test
	})
	return seller, product
}

// imageURLs đọc đường dẫn ảnh của mẫu như getProduct với IMAGE_PRESET_VERSION = preset.
func (p *procEnv) imageURLs(seller, product uuid.UUID, preset int) []string {
	p.t.Helper()
	var urls []string
	p.must(p.a, func(tx pgx.Tx) error {
		imgs, err := catalog.ProductImages(p.t.Context(), tx, seller, product, catalog.ImageView{BaseURL: publicBase, Preset: preset})
		for _, img := range imgs {
			for _, v := range img.Variants {
				urls = append(urls, v.URL)
			}
		}
		return err //nolint:wrapcheck // test
	})
	return urls
}

// regenFixture: hai ảnh sản phẩm của A (ids), một ảnh private của A, một ảnh của B, đều ready với v1; mẫu có ảnh ids[0].
type regenFixture struct {
	bodies          [][]byte
	ids             []uuid.UUID
	priv, foreign   uuid.UUID
	seller, product uuid.UUID
}

func (p *procEnv) regenFixture() regenFixture {
	p.t.Helper()
	f := regenFixture{bodies: [][]byte{[]byte("anh-ao-dai-xanh"), []byte("anh-vay-hoa")}}
	for _, b := range f.bodies {
		f.ids = append(f.ids, p.upload(p.a, "product_image", b))
	}
	f.priv = p.registerPrivate(p.a, []byte("anh-khach-gui"))
	f.foreign = p.upload(p.b, "product_image", []byte("anh-cua-shop-b"))
	p.drain()
	for _, id := range append(slices.Clone(f.ids), f.priv, f.foreign) {
		if r := p.fileRow(id); r.Status != files.StatusReady {
			p.t.Fatalf("tệp %s chưa ready: %+v", id, r)
		}
	}
	f.seller, f.product = p.productWithImage(f.ids[0])
	return f
}

// storeSnapshot: số đối tượng trong crm-tmp, crm-originals và ETag của một ảnh gốc (kiểm "không tải lên lại").
type storeSnapshot struct {
	tmp, orig int
	etag      string
}

func (p *procEnv) snapshot(sha string) storeSnapshot {
	p.t.Helper()
	info, err := p.mem.Head(p.t.Context(), files.BucketOriginals, files.OriginalKey(sha))
	if err != nil {
		p.t.Fatal(err)
	}
	return storeSnapshot{tmp: len(p.mem.Keys(files.BucketTmp)), orig: len(p.mem.Keys(files.BucketOriginals)), etag: info.ETag}
}

// checkV2 kiểm tệp id: lệnh done, đủ biến thể v2 (có 1200) trên kho, v1 giữ nguyên 12, tệp vẫn ready.
func (p *procEnv) checkV2(id uuid.UUID, v2 []files.VariantSpec) {
	p.t.Helper()
	if j := p.jobByKey(kindRegenerate, id.String()+":v2"); j != "done" {
		p.t.Fatalf("lệnh tạo lại của tệp %s: %s", id, j)
	}
	n2, has1200 := p.presetRows(id, 2)
	n1, _ := p.presetRows(id, 1)
	if n2 != len(v2) || !has1200 || n1 != 12 {
		p.t.Fatalf("tệp %s: %d biến thể v2 (có 1200: %v), %d v1", id, n2, has1200, n1)
	}
	if r := p.fileRow(id); r.Status != files.StatusReady {
		p.t.Fatalf("tệp %s sau khi tạo lại: %+v", id, r)
	}
	for _, v := range p.variants(id) {
		if v.Preset != 2 {
			continue
		}
		if _, err := p.mem.Head(p.t.Context(), files.BucketPublic, v.Key); err != nil || !strings.Contains(v.Key, "/v2/") {
			p.t.Fatalf("biến thể v2 %s: %v", v.Key, err)
		}
	}
}

// checkImagerFromOriginals: imager được gọi đúng want lần kể từ lần thứ from, nguồn là ảnh gốc, đủ đầu ra của v2.
func (p *procEnv) checkImagerFromOriginals(from, want, outputs int) {
	p.t.Helper()
	if got := p.img.count() - from; got != want {
		p.t.Fatalf("imager gọi thêm %d lần, muốn %d", got, want)
	}
	for _, req := range p.img.requests()[from:] {
		if b, key := bucketOf(p.t, req.SourceURL); b != files.BucketOriginals || !strings.HasPrefix(key, "orig/") {
			p.t.Fatalf("nguồn của tạo lại: %s/%s (phải là ảnh gốc)", b, key)
		}
		if len(req.Outputs) != outputs {
			p.t.Fatalf("số đầu ra: %d, muốn %d", len(req.Outputs), outputs)
		}
	}
}

// checkURLs: getProduct với IMAGE_PRESET_VERSION = preset trả đúng n đường dẫn, mọi đường dẫn bắt đầu prefix.
func (p *procEnv) checkURLs(f regenFixture, preset, n int, prefix string) {
	p.t.Helper()
	urls := p.imageURLs(f.seller, f.product, preset)
	if len(urls) != n {
		p.t.Fatalf("getProduct v%d: %d đường dẫn, muốn %d", preset, len(urls), n)
	}
	for _, u := range urls {
		if !strings.HasPrefix(u, prefix) {
			p.t.Fatalf("getProduct v%d: đường dẫn %s không bắt đầu %s", preset, u, prefix)
		}
	}
}

// TP07: thêm bộ kích thước v2 (thêm chiều rộng 1200) → regenerateImages {presetVersion: 2} → sau khi worker chạy, mỗi
// tệp ready của mục đích standard có thêm biến thể v2 (có 1200) tạo từ ảnh gốc; không tải lên lại, ảnh gốc giữ
// nguyên (cùng ETag); getProduct với IMAGE_PRESET_VERSION=2 trả đường dẫn v2; tệp private và tệp của tenant khác
// không được xếp lệnh.
func TestRegeneratePresetV2(t *testing.T) {
	p := newProcEnv(t)
	v2 := presetsWithV2(t)
	f := p.regenFixture()
	sha := shaHex(f.bodies[0])
	img := publicBase + "/img/" + sha
	p.checkURLs(f, 2, 12, img+"/v1/") // chưa có v2: lùi về v1
	before, calls := p.snapshot(sha), p.img.count()

	if n, err := p.regenerate(p.a, 2); err != nil || n != 2 {
		t.Fatalf("regenerate: %d, %v (muốn 2)", n, err)
	}
	for id, want := range map[uuid.UUID]int{f.ids[0]: 1, f.ids[1]: 1, f.priv: 0, f.foreign: 0} {
		if c := p.regenJobs(id, "2"); c != want {
			t.Fatalf("tệp %s có %d lệnh file.regenerate, muốn %d (private, tenant B: 0)", id, c, want)
		}
	}
	p.drain()

	for _, id := range f.ids {
		p.checkV2(id, v2)
	}
	p.checkImagerFromOriginals(calls, 2, len(v2))
	if after := p.snapshot(sha); after != before || len(p.mem.Versions(files.BucketOriginals, files.OriginalKey(sha))) != 1 {
		t.Fatalf("kho đổi khi tạo lại (tải lên mới, ảnh gốc mới hay ảnh gốc bị ghi lại): %+v → %+v", before, after)
	}
	for _, id := range []uuid.UUID{f.priv, f.foreign} {
		if n, _ := p.presetRows(id, 2); n != 0 {
			t.Fatalf("tệp %s (private hay tenant B) có %d biến thể v2", id, n)
		}
	}
	p.checkURLs(f, 2, len(v2), img+"/v2/")
	p.checkURLs(f, 1, 12, img+"/v1/")

	// Gọi lại: mọi tệp đã có v2 → không xếp lệnh nào.
	if n, err := p.regenerate(p.a, 2); err != nil || n != 0 {
		t.Fatalf("gọi lại: %d, %v (muốn 0)", n, err)
	}
}

// Bộ kích thước chưa có trong bảng → ErrUnknownPreset, không xếp lệnh.
func TestRegenerate_UnknownPreset(t *testing.T) {
	p := newProcEnv(t)
	p.upload(p.a, "product_image", []byte("anh-mot"))
	p.drain()
	for _, preset := range []int{0, 2, 99} {
		if _, err := p.regenerate(p.a, preset); !errors.Is(err, files.ErrUnknownPreset) {
			t.Fatalf("preset %d: %v, muốn ErrUnknownPreset", preset, err)
		}
	}
	if c := p.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = $1", kindRegenerate); c != 0 {
		t.Fatalf("có %d lệnh file.regenerate", c)
	}
}

// jobByKey đọc trạng thái lệnh kind có khóa chống trùng key.
func (p *procEnv) jobByKey(kind, key string) string {
	p.t.Helper()
	var status string
	if err := p.staff.Owner.Pool.QueryRow(p.t.Context(),
		"SELECT status FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2", kind, key).Scan(&status); err != nil {
		p.t.Fatalf("đọc lệnh %s %s: %v", kind, key, err)
	}
	return status
}

// Ảnh gốc mất trên kho → lệnh rejected source_missing (dead), tệp vẫn ready với bản cũ, không có biến thể v2,
// getProduct với v2 lùi về v1. Tệp đã xóa sau khi xếp lệnh → lệnh xong không ghi gì.
func TestRegenerate_SourceMissingKeepsFileReady(t *testing.T) {
	p := newProcEnv(t)
	presetsWithV2(t)
	body, gone := []byte("anh-mat-goc"), []byte("anh-se-xoa")
	id := p.upload(p.a, "product_image", body)
	del := p.upload(p.a, "product_image", gone)
	p.drain()
	if err := p.mem.Delete(t.Context(), files.BucketOriginals, files.OriginalKey(shaHex(body))); err != nil {
		t.Fatal(err)
	}
	if n, err := p.regenerate(p.a, 2); err != nil || n != 2 {
		t.Fatalf("regenerate: %d, %v", n, err)
	}
	p.must(p.a, func(tx pgx.Tx) error {
		return files.MarkDeleted(t.Context(), tx, del, p.actor)
	})
	calls := p.img.count()
	p.drain()
	if j := p.jobByKey(kindRegenerate, id.String()+":v2"); j != "dead" {
		t.Fatalf("lệnh của tệp mất ảnh gốc: %s, muốn dead", j)
	}
	if j := p.jobByKey(kindRegenerate, del.String()+":v2"); j != "done" && j != "dead" {
		t.Fatalf("lệnh của tệp đã xóa: %s", j)
	}
	if r := p.fileRow(id); r.Status != files.StatusReady || r.Reason != "" {
		t.Fatalf("tệp phải giữ ready: %+v", r)
	}
	if n, _ := p.presetRows(id, 2); n != 0 {
		t.Fatalf("có %d biến thể v2", n)
	}
	if n, _ := p.presetRows(del, 2); n != 0 {
		t.Fatalf("tệp đã xóa có %d biến thể v2", n)
	}
	if p.img.count() != calls {
		t.Fatalf("imager được gọi %d lần", p.img.count()-calls)
	}
}
