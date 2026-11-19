package files_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// assertNothingPublic: không đối tượng nào trong crm-public, crm-originals; không khóa orig/, img/ ở bất kỳ bucket
// nào; mọi PutURL imager nhận đều trỏ crm-private (hoặc crm-tmp cho đầu ra thử), nguồn luôn ở crm-tmp (R10).
func (p *procEnv) assertNothingPublic() {
	p.t.Helper()
	for _, b := range []files.Bucket{files.BucketPublic, files.BucketOriginals} {
		if ks := p.mem.Keys(b); len(ks) != 0 {
			p.t.Fatalf("%s có đối tượng: %v", b, ks)
		}
	}
	for _, b := range []files.Bucket{files.BucketTmp, files.BucketPrivate} {
		for _, k := range p.mem.Keys(b) {
			if strings.HasPrefix(k, "orig/") || strings.HasPrefix(k, "img/") {
				p.t.Fatalf("khóa công khai %s/%s", b, k)
			}
		}
	}
	for _, r := range p.img.requests() {
		if b, _ := bucketOf(p.t, r.SourceURL); b != files.BucketTmp {
			p.t.Fatalf("nguồn ở %s", b)
		}
		for _, o := range r.Outputs {
			if b, _ := bucketOf(p.t, o.PutURL); b != files.BucketPrivate && b != files.BucketTmp {
				p.t.Fatalf("đầu ra ở %s", b)
			}
		}
	}
}

// R10: ảnh khách → chỉ crm-private, khóa priv/<tenant>/<file>/v1/; không công khai ở đâu.
func TestProcessPrivate_NeverInPublicBucket(t *testing.T) {
	p := newProcEnv(t)
	id := p.registerPrivate(p.a, []byte("anh-khach-co-gps"))
	if p.jobs(kindPrivate, id) != 1 || p.jobs(kindPublic, id) != 0 {
		t.Fatalf("lệnh: private %d, public %d", p.jobs(kindPrivate, id), p.jobs(kindPublic, id))
	}
	p.drain()
	r := p.fileRow(id)
	if r.Status != files.StatusReady || r.Bucket != "private" || r.Key != files.ServerKey(p.a, id) || r.Width == nil {
		t.Fatalf("tệp: %+v", r)
	}
	vs := p.variants(id)
	if len(vs) != 12 {
		t.Fatalf("muốn 12 biến thể, có %d", len(vs))
	}
	prefix := "priv/" + p.a.String() + "/" + id.String() + "/v1/"
	for _, v := range vs {
		if v.Bucket != "private" || !strings.HasPrefix(v.Key, prefix) {
			t.Fatalf("biến thể: %+v", v)
		}
	}
	p.assertNothingPublic()
	if info := p.info(p.a, id); len(info.Variants) != 0 {
		t.Fatalf("Describe trả biến thể công khai: %v", info.Variants)
	}
	var got map[uuid.UUID][]files.VariantURL
	p.must(p.a, func(tx pgx.Tx) error {
		var err error
		got, err = files.PublicVariants(t.Context(), tx, []uuid.UUID{id}, publicBase)
		return err //nolint:wrapcheck // test
	})
	if len(got) != 0 {
		t.Fatalf("PublicVariants trả tệp riêng tư: %v", got)
	}
}

// DownloadURL nhánh private: ký có hạn tới biến thể JPEG lớn nhất trên crm-private, không phải ảnh thô.
func TestProcessPrivate_SignedExpiringLink(t *testing.T) {
	p := newProcEnv(t)
	id := p.registerPrivate(p.a, []byte("anh-khach"))
	if _, err := p.downloadURL(p.mem, p.a, id, time.Minute); !errors.Is(err, files.ErrNotReady) {
		t.Fatalf("trước khi xử lý: muốn ErrNotReady, nhận %v", err)
	}
	p.drain()
	u, err := p.downloadURL(p.mem, p.a, id, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	b, key := bucketOf(t, u)
	if b != files.BucketPrivate || key != files.PrivateVariantKey(p.a, id, 1, 1600, "jpeg") || strings.HasPrefix(u, publicBase) {
		t.Fatalf("đường dẫn: %s", u)
	}
	if code, _ := get(t, u); code != http.StatusOK {
		t.Fatalf("GET: %d", code)
	}
	short, err := p.downloadURL(p.mem, p.a, id, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	if code, _ := get(t, short); code != http.StatusForbidden {
		t.Fatalf("hết hạn: muốn 403, nhận %d", code)
	}
	if _, err := p.downloadURL(p.mem, p.b, id, time.Minute); !errors.Is(err, files.ErrNotFound) {
		t.Fatalf("tenant B: muốn ErrNotFound, nhận %v", err)
	}
}

// Nội dung không phải ảnh → rejected not_image, không biến thể; RegisterPrivateImage kiểm loại, cỡ, chế độ.
func TestProcessPrivate_RejectsNonImage(t *testing.T) {
	p := newProcEnv(t)
	id := p.registerPrivate(p.a, []byte(rejPrefix+"not_image\n<html>"))
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusRejected || r.Reason != "not_image" {
		t.Fatalf("tệp: %+v", r)
	}
	if len(p.variants(id)) != 0 {
		t.Fatal("có biến thể")
	}
	if _, err := p.downloadURL(p.mem, p.a, id, time.Minute); !errors.Is(err, files.ErrRejected) {
		t.Fatalf("muốn ErrRejected, nhận %v", err)
	}
	cases := []struct {
		name string
		in   files.NewPrivateImage
		want error
	}{
		{"svg", files.NewPrivateImage{Purpose: "test_private_image", Size: 10, ContentType: "image/svg+xml"}, files.ErrTypeNotAllowed},
		{"quá lớn", files.NewPrivateImage{Purpose: "test_private_image", Size: 21 * mib, ContentType: "image/jpeg"}, files.ErrTooLarge},
		{"mục đích standard", files.NewPrivateImage{Purpose: "product_image", Size: 10, ContentType: "image/jpeg"}, files.ErrPurposeMode},
		{"chưa đăng ký", files.NewPrivateImage{Purpose: "khong_co", Size: 10, ContentType: "image/jpeg"}, files.ErrUnknownPurpose},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.in.ID = uuid.Must(uuid.NewV7())
			if _, err := p.registerPrivateRow(p.a, c.in); !errors.Is(err, c.want) {
				t.Fatalf("muốn %v, nhận %v", c.want, err)
			}
			if n := p.countFiles(p.a, c.in.ID); n != 0 {
				t.Fatal("có dòng tệp")
			}
			if p.jobs(kindPrivate, c.in.ID) != 0 {
				t.Fatal("có lệnh")
			}
		})
	}
}

// file.process xếp nhầm cho mục đích private → rejected purpose_private, không gọi imager, tệp không đổi; tải lên
// của nhân viên với mục đích private đi file.process_private.
func TestFileProcess_RefusesPrivatePurpose(t *testing.T) {
	p := newProcEnv(t)
	id := p.upload(p.a, "test_private_image", []byte("nhan-vien-tai-anh-khach"))
	if p.jobs(kindPrivate, id) != 1 || p.jobs(kindPublic, id) != 0 {
		t.Fatalf("lệnh: private %d, public %d", p.jobs(kindPrivate, id), p.jobs(kindPublic, id))
	}
	p.must(p.a, func(tx pgx.Tx) error {
		_, _, err := p.q.Enqueue(t.Context(), tx, files.ProcessKind(), queue.NewJob{
			TenantID: &p.a, Payload: map[string]string{"fileId": id.String()}, DedupeKey: id.String(),
		})
		return err //nolint:wrapcheck // test
	})
	// Gọi thẳng file.process: từ chối, không gọi imager, tệp không đổi.
	out, err := p.pub.Execute(t.Context(), p.queueJob(kindPublic, id))
	if err != nil || out.Status != commands.StatusRejected || out.Reason != "purpose_private" {
		t.Fatalf("Execute: %+v %v", out, err)
	}
	if p.img.count() != 0 || len(p.mem.Keys(files.BucketPublic)) != 0 {
		t.Fatalf("imager %d lần, public %v", p.img.count(), p.mem.Keys(files.BucketPublic))
	}
	if r := p.fileRow(id); r.Status != files.StatusUploaded {
		t.Fatalf("tệp bị lệnh nhầm đổi: %+v", r)
	}
	p.drain()
	if r := p.fileRow(id); r.Status != files.StatusReady {
		t.Fatalf("file.process_private: %+v, lệnh %+v", r, p.job(kindPrivate, id))
	}
	if j := p.job(kindPublic, id); j.Status != "dead" || !strings.Contains(j.LastError, "purpose_private") {
		t.Fatalf("lệnh nhầm: %+v", j)
	}
	if p.img.count() != 1 {
		t.Fatalf("imager gọi %d lần, muốn 1 (chỉ lệnh riêng tư)", p.img.count())
	}
	p.assertNothingPublic()
}

// Chống trùng chỉ trong cùng tenant, cùng mục đích; cùng ID hai lần → một dòng, một lệnh.
func TestProcessPrivate_Dedupe(t *testing.T) {
	p := newProcEnv(t)
	body := []byte("cung-anh-khach")
	first := p.registerPrivate(p.a, body)
	p.drain()
	second := p.registerPrivate(p.a, body)
	p.drain()
	if p.img.count() != 1 {
		t.Fatalf("imager gọi %d lần, muốn 1", p.img.count())
	}
	if p.fileRow(first).Status != files.StatusReady || p.fileRow(second).Status != files.StatusReady {
		t.Fatal("hai tệp phải ready")
	}
	ofB := p.registerPrivate(p.b, body)
	p.drain()
	if p.img.count() != 2 {
		t.Fatalf("tenant B: imager gọi %d lần, muốn 2", p.img.count())
	}
	for _, v := range p.variants(ofB) {
		if !strings.HasPrefix(v.Key, "priv/"+p.b.String()+"/") {
			t.Fatalf("biến thể của B: %s", v.Key)
		}
	}
	in := files.NewPrivateImage{ID: first, Purpose: "test_private_image", Size: int64(len(body)), ContentType: "image/jpeg"}
	if got, err := p.registerPrivateRow(p.a, in); err != nil || got != first {
		t.Fatalf("lần hai: %s %v", got, err)
	}
	if p.jobs(kindPrivate, first) != 1 || p.countFiles(p.a, first) != 1 {
		t.Fatal("cùng ID hai lần phải một dòng, một lệnh")
	}
	p.assertNothingPublic()
}

// Looker trả not_sent (chạy lại an toàn); ảnh thô bị xóa → source_missing; imager treo → 3 lần rồi dead.
func TestProcessPrivate_LookerMissingSourceAndHang(t *testing.T) {
	p := newProcEnv(t)
	gone := p.registerPrivate(p.a, []byte("da-xoa"))
	if err := p.mem.Delete(t.Context(), files.BucketTmp, files.TmpKey(p.a, gone)); err != nil {
		t.Fatal(err)
	}
	files.SetProcessTimeout(p.priv, 300*time.Millisecond)
	p.w = p.newWorker()
	hang := p.registerPrivate(p.a, []byte(hangPrefix))
	p.rounds(4)
	if r := p.fileRow(gone); r.Status != files.StatusRejected || r.Reason != "source_missing" {
		t.Fatalf("ảnh thô đã xóa: %+v", r)
	}
	// Treo là lỗi hạ tầng (Task 409): không tính lượt, tệp processing tới khi hết trần thời gian.
	if j, r := p.job(kindPrivate, hang), p.fileRow(hang); j.Status != "pending" || j.Attempts != 0 || r.Status != files.StatusProcessing {
		t.Fatalf("lệnh treo trước trần: %+v, tệp %+v", j, r)
	}
	p.backdate(kindPrivate, hang, files.DefaultRetryMax+time.Minute)
	p.makeDue()
	p.drain()
	if j := p.job(kindPrivate, hang); j.Status != "dead" || j.Attempts != 1 {
		t.Fatalf("lệnh treo: %+v", j)
	}
	if r := p.fileRow(hang); r.Status != files.StatusRejected || r.Reason != "processing_failed" {
		t.Fatalf("tệp treo: %+v", r)
	}
	look, ok := p.priv.(commands.Looker)
	if !ok {
		t.Fatal("file.process_private phải có Looker")
	}
	if st, err := look.Lookup(t.Context(), p.queueJob(kindPrivate, hang)); err != nil || st != commands.StatusNotSent {
		t.Fatalf("Lookup: %s %v", st, err)
	}
	p.assertNothingPublic()
}

// Quy luật (rapid): với mọi chuỗi kết quả của imager, lệnh riêng tư không bao giờ tạo đối tượng ở crm-public,
// crm-originals hay khóa orig/, img/; lý do từ chối luôn thuộc tập đã khai; lỗi hạ tầng (upload_failed, timeout, treo)
// không bao giờ làm tệp rejected trước trần — tệp ở processing (Task 409).
func TestProcessPrivate_PropertyNeverPublic(t *testing.T) {
	p := newProcEnv(t)
	files.SetProcessTimeout(p.priv, 500*time.Millisecond)
	p.w = p.newWorker()
	known := map[string]bool{
		"not_image": true, "type_not_allowed": true, "too_large": true, "too_many_pixels": true, "decode_failed": true,
		"processing_failed": true, "source_missing": true,
	}
	infra := map[string]bool{"upload_failed": true, "timeout": true, "hang": true}
	rapid.Check(t, func(rt *rapid.T) {
		kind := rapid.SampledFrom([]string{"ok", "not_image", "too_many_pixels", "decode_failed", "upload_failed", "timeout", "hang"}).Draw(rt, "imager")
		var body []byte
		switch kind {
		case "ok":
			body = []byte("ok-" + uuid.NewString())
		case "hang":
			body = []byte(hangPrefix + uuid.NewString())
		default:
			body = []byte(rejPrefix + kind + "\n" + uuid.NewString())
		}
		id := p.registerPrivate(p.a, body)
		for range 4 {
			p.drain()
			p.makeDue()
		}
		r := p.fileRow(id)
		switch r.Status {
		case files.StatusReady:
			if len(p.variants(id)) != 12 {
				rt.Fatalf("ready mà %d biến thể", len(p.variants(id)))
			}
		case files.StatusRejected:
			if !known[r.Reason] || infra[kind] {
				rt.Fatalf("lý do lạ %q (%s)", r.Reason, kind)
			}
		case files.StatusProcessing:
			if !infra[kind] {
				rt.Fatalf("%s mà vẫn processing", kind)
			}
		default:
			rt.Fatalf("sau 4 vòng vẫn %s (%s)", r.Status, kind)
		}
		p.assertNothingPublic()
		// Lệnh lỗi hạ tầng còn chờ tới trần (24 giờ): bỏ đi để vòng sau không chạy lại chúng (makeDue đưa mọi việc về hạn).
		if _, err := p.staff.Owner.Pool.Exec(t.Context(),
			"UPDATE platform.jobs SET status = 'expired', finished_at = now() WHERE status = 'pending'"); err != nil {
			rt.Fatal(err)
		}
	})
}

// countFiles đếm dòng tệp id trong tenant (crm_app, RLS).
func (p *procEnv) countFiles(tenant, id uuid.UUID) int {
	p.t.Helper()
	var n int
	p.must(tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(p.t.Context(), "SELECT count(*) FROM platform.files WHERE id = $1", id).Scan(&n)
	})
	return n
}
