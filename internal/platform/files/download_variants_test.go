package files_test

// Ca thêm ở Task 403 (mutation testing vùng cao M1, docs/mutation.md): biến thể "lạ" nằm cạnh bộ biến thể đúng của
// tệp — của bộ kích thước cũ, khác định dạng, khác ngăn — không làm DownloadURL, Describe chọn sai hay bỏ sót.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// addVariant chèn (chủ bảng, tenant A) một dòng file_variants cho tệp id.
func (p *procEnv) addVariant(id uuid.UUID, preset, width int, format string, bucket files.Bucket, key string) {
	p.t.Helper()
	p.asOwner(func(tx pgx.Tx) (bool, error) {
		_, err := tx.Exec(p.t.Context(), `INSERT INTO platform.file_variants
			(tenant_id, file_id, preset_version, width, format, bucket, key, size_bytes) VALUES ($1, $2, $3, $4, $5, $6, $7, 1)`,
			p.a, id, preset, width, format, string(bucket), key)
		return true, err //nolint:wrapcheck // test
	})
}

// Ảnh riêng tư: DownloadURL ký tới JPEG riêng tư rộng nhất **của bộ kích thước hiện hành**, kể cả khi còn biến thể
// rộng hơn của bộ cũ hay khác định dạng.
func TestProcessPrivate_DownloadPicksCurrentPresetJPEG(t *testing.T) {
	p := newProcEnv(t)
	id := p.registerPrivate(p.a, []byte("anh-khach"))
	p.drain()
	p.addVariant(id, 0, 4000, "jpeg", files.BucketPrivate, "cu/4000.jpg")
	p.addVariant(id, 1, 3000, "webp", files.BucketPrivate, "khac/3000.webp")
	u, err := p.downloadURL(p.mem, p.a, id, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if b, key := bucketOf(t, u); b != files.BucketPrivate || key != files.PrivateVariantKey(p.a, id, 1, 1600, "jpeg") {
		t.Fatalf("ký tới %s/%s", b, key)
	}
}

// Describe liệt kê mọi biến thể công khai, kể cả khi một biến thể riêng tư đứng trước trong thứ tự đọc.
func TestDescribe_SkipsPrivateVariantKeepsRest(t *testing.T) {
	p := newProcEnv(t)
	id := p.upload(p.a, "product_image", []byte("anh-ao-dai-mau-do"))
	p.drain()
	want := len(p.info(p.a, id).Variants)
	if want == 0 {
		t.Fatal("ảnh công khai phải có biến thể")
	}
	p.addVariant(id, 0, 1, "avif", files.BucketPrivate, "rieng/1.avif") // preset 0, avif: đứng đầu ORDER BY
	if got := p.info(p.a, id).Variants; len(got) != want {
		t.Fatalf("%d biến thể công khai, mong %d", len(got), want)
	}
}

// Không đặt hạn riêng → giữ hạn 30 s của loại lệnh (không thành 0); đặt riêng thì dùng hạn đó.
func TestProcessor_KindTimeout(t *testing.T) {
	for name, h := range map[string]*files.Processor{
		"file.process":         files.NewProcessHandler(files.ProcessorOptions{}),
		"file.process_private": files.NewProcessPrivateHandler(files.ProcessorOptions{}),
	} {
		if got := h.Kind().Policy.Timeout; got != 30*time.Second {
			t.Errorf("%s: hạn %v, mong 30s", name, got)
		}
		files.SetProcessTimeout(h, time.Second)
		if got := h.Kind().Policy.Timeout; got != time.Second {
			t.Errorf("%s đặt riêng: hạn %v, mong 1s", name, got)
		}
	}
	if k := files.RegenerateKind(); k.Policy.Timeout != 30*time.Second || k.Policy.MaxAttempts != 3 ||
		k.Policy.ExpiresAfter != 24*time.Hour {
		t.Errorf("file.regenerate: hạn %v, %d lần, chờ tối đa %v; mong 30s, 3 lần, 24h", k.Policy.Timeout,
			k.Policy.MaxAttempts, k.Policy.ExpiresAfter)
	}
	if k := files.VerifyKind(); k.Policy.Timeout != 30*time.Second || k.Policy.ExpiresAfter != 24*time.Hour {
		t.Errorf("file.verify: hạn %v, chờ tối đa %v; mong 30s, 24h", k.Policy.Timeout, k.Policy.ExpiresAfter)
	}
}

// Xử lý thành công không bao giờ đi qua trần thử lại: dù lệnh đã quá RetryMax, ảnh tốt vẫn ready (không rejected
// processing_failed).
func TestFileProcess_SuccessIgnoresRetryCeiling(t *testing.T) {
	p := newProcEnvWith(t, func(o *files.ProcessorOptions) { o.RetryMax = time.Nanosecond })
	id := p.upload(p.a, "product_image", []byte("anh-tot-qua-tran"))
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusReady {
		t.Fatalf("tệp: %+v", r)
	}
}

// Kết quả imager lạ (cao 0, rộng 0, thiếu loại đã nhận; đầu ra sai rộng, sai định dạng, 0 byte) → không ghi tệp ready, chờ thử lại.
func TestFileProcess_OddImagerResultNotReady(t *testing.T) {
	for _, body := range []string{oddPrefix + "h0", oddPrefix + "w0", oddPrefix + "type", oddPrefix + "w", oddPrefix + "fmt", oddPrefix + "b0"} {
		p := newProcEnv(t)
		id := p.upload(p.a, "product_image", []byte(body))
		p.drain()
		if r := p.fileRow(id); r.Status == files.StatusReady {
			t.Fatalf("%s: tệp ready với kết quả imager lạ: %+v", body, r)
		}
	}
}

// Chạy lại lệnh của tệp đã bị từ chối vì nội dung (not_image) → vẫn rejected not_image, không gọi imager lần nữa;
// chỉ processing_failed mới được xử lý lại.
func TestFileProcess_ContentRejectNotReprocessed(t *testing.T) {
	p := newProcEnv(t)
	id := p.upload(p.a, "product_image", []byte(rejPrefix+"not_image\n<html>"))
	p.drain()
	calls := p.img.count()
	out, err := p.pub.Execute(t.Context(), queue.Job{TenantID: &p.a, Payload: []byte(`{"fileId":"` + id.String() + `"}`)})
	if err != nil || out.Status != commands.StatusRejected || out.Reason != "not_image" {
		t.Fatalf("chạy lại: %+v %v", out, err)
	}
	if p.img.count() != calls {
		t.Fatal("imager bị gọi lại cho tệp đã từ chối vì nội dung")
	}
	if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != "not_image" {
		t.Fatalf("tệp: %+v", r)
	}
}

// Lệnh có payload hợp lệ nhưng thiếu fileId → bad_payload (không đi đọc tệp uuid.Nil).
func TestFileProcess_PayloadWithoutFileID(t *testing.T) {
	p := newProcEnv(t)
	out, err := p.pub.Execute(t.Context(), queue.Job{TenantID: &p.a, Payload: []byte(`{}`)})
	if err != nil || out.Status != commands.StatusRejected || out.Reason != "bad_payload" {
		t.Fatalf("kết quả %+v %v", out, err)
	}
}

// Bản kê manifest.json có sẵn trên kho nhưng hỏng hay không khớp (sha, bộ kích thước, kích thước, số biến thể, khóa,
// rộng, định dạng, cỡ) → không dùng: dù mọi biến thể đã có, vẫn gọi imager một lần để có kết quả kiểm; tệp vẫn ready.
func TestFileProcess_BadManifestIgnored(t *testing.T) {
	body := []byte("anh-co-ban-ke")
	sha := shaHex(body)
	path := "img/" + sha + "/v1/" + "manifest.json"
	first := newProcEnv(t)
	first.upload(first.a, "product_image", body)
	first.drain()
	var good map[string]any
	if err := json.Unmarshal(readAll(t, first.mem, files.BucketPublic, path), &good); err != nil {
		t.Fatal(err)
	}
	variant0 := func(m map[string]any) map[string]any {
		return m["variants"].([]any)[0].(map[string]any) //nolint:forcetypeassert // test: đúng dạng processResult
	}
	for name, corrupt := range map[string]func(m map[string]any){
		"sha khác":        func(m map[string]any) { m["sha256"] = strings.Repeat("0", 64) },
		"bộ kích thước 2": func(m map[string]any) { m["preset"] = 2 },
		"rộng 0":          func(m map[string]any) { m["width"] = 0 },
		"cao 0":           func(m map[string]any) { m["height"] = 0 },
		"thiếu biến thể":  func(m map[string]any) { m["variants"] = m["variants"].([]any)[1:] },
		"khóa khác":       func(m map[string]any) { variant0(m)["key"] = "khac" },
		"rộng biến thể":   func(m map[string]any) { variant0(m)["width"] = 1 },
		"định dạng":       func(m map[string]any) { variant0(m)["format"] = "gif" },
		"cỡ 0":            func(m map[string]any) { variant0(m)["size"] = 0 },
	} {
		var m map[string]any
		raw, _ := json.Marshal(good)
		_ = json.Unmarshal(raw, &m)
		corrupt(m)
		bad, _ := json.Marshal(m)
		p := newProcEnv(t)
		p.seedManifest(first, path, bad, good) // mọi biến thể đã có trên kho: chỉ bản kê quyết định có gọi imager
		id := p.upload(p.a, "product_image", body)
		p.drain()
		if p.img.count() != 1 {
			t.Errorf("%s: imager gọi %d lần, mong 1 (bản kê hỏng phải bị bỏ qua)", name, p.img.count())
		}
		if r := p.fileRow(id); r.Status != files.StatusReady {
			t.Errorf("%s: tệp %+v", name, r)
		}
	}
}

// Bản kê hợp lệ, đủ biến thể: khai kích thước vượt MaxPixels → tệp rejected too_many_pixels; đúng MaxPixels
// (8000×5000) → ready. Cả hai không gọi imager (kiểm lại kết quả kiểm của bản kê dùng chung, Q7).
func TestFileProcess_ManifestPixelLimit(t *testing.T) {
	body := []byte("anh-ban-ke-qua-lon")
	path := "img/" + shaHex(body) + "/v1/manifest.json"
	first := newProcEnv(t)
	first.upload(first.a, "product_image", body)
	first.drain()
	for _, c := range []struct {
		w, h         int
		status, want string
	}{{30000, 30000, files.StatusRejected, "too_many_pixels"}, {8000, 5000, files.StatusReady, ""}} {
		var m map[string]any
		if err := json.Unmarshal(readAll(t, first.mem, files.BucketPublic, path), &m); err != nil {
			t.Fatal(err)
		}
		m["width"], m["height"] = c.w, c.h
		bad, _ := json.Marshal(m)
		p := newProcEnv(t)
		p.seedManifest(first, path, bad, m)
		id := p.upload(p.a, "product_image", body)
		p.drain()
		if r := p.fileRow(id); r.Status != c.status || r.Reason != c.want || p.img.count() != 0 {
			t.Errorf("%d×%d: tệp %+v, imager %d lần; mong %s %q, 0 lần", c.w, c.h, r, p.img.count(), c.status, c.want)
		}
	}
}

// seedManifest ghi bản kê manifest vào kho của p ở path và chép sang mọi biến thể mà bản kê ref liệt kê, lấy từ kho
// của from (đã xử lý xong cùng ảnh).
func (p *procEnv) seedManifest(from *procEnv, path string, manifest []byte, ref map[string]any) {
	p.t.Helper()
	t := p.t
	if err := p.mem.Put(t.Context(), files.BucketPublic, path, bytes.NewReader(manifest), int64(len(manifest)), "application/json"); err != nil {
		t.Fatal(err)
	}
	for _, v := range ref["variants"].([]any) { //nolint:forcetypeassert // test: đúng dạng processResult
		key := v.(map[string]any)["key"].(string) //nolint:forcetypeassert // test
		info, err := from.mem.Head(t.Context(), files.BucketPublic, key)
		if err != nil {
			t.Fatal(err)
		}
		b := readAll(t, from.mem, files.BucketPublic, key)
		if err := p.mem.Put(t.Context(), files.BucketPublic, key, bytes.NewReader(b), int64(len(b)), info.ContentType); err != nil {
			t.Fatal(err)
		}
	}
}
