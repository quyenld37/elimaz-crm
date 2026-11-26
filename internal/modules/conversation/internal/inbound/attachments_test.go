package inbound_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/platform/files"
)

// httpGet đọc u (đường dẫn ký của kho tệp trong bộ nhớ); mã khác 200 là lỗi kèm mã.
func httpGet(ctx context.Context, u string) ([]byte, error) {
	b, _, err := httpGetFull(ctx, u)
	return b, err
}

func httpGetFull(ctx context.Context, u string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err //nolint:wrapcheck // test
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, nil, err //nolint:wrapcheck // test
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err == nil && resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("HTTP %d", resp.StatusCode) //nolint:err113 // test
	}
	return b, resp.Header, err
}

// jpegWithGPS là "ảnh JPEG" có khối EXIF chứa tọa độ GPS (imager giả không giải mã; đủ để kiểm biến thể không chép
// nguyên ảnh thô).
func jpegWithGPS(seed string) []byte {
	b := []byte("\xFF\xD8\xFF\xE1\x00\x40Exif\x00\x00GPSLatitude=10.7769;GPSLongitude=106.7009;")
	return append(b, []byte(seed)...)
}

func cdnHost() string { return fake.CDNHost }

func img(u string) channel.AttachmentRef { return channel.AttachmentRef{Type: "image", URL: u} }

func file(u string) channel.AttachmentRef { return channel.AttachmentRef{Type: "file", URL: u} }

// TP02: ảnh JPEG có GPS → một lệnh tải; ảnh thô chỉ ở crm-tmp, 1 dòng chat_image uploaded, 1 việc
// file.process_private, 0 file.process; xử lý xong: tệp ready, mọi biến thể ở bucket private dưới
// priv/<tenant>/<tệp>/, không đối tượng nào ở public hay originals (orig/, img/), biến thể không chứa GPS; mục stored.
// Đường dẫn: thuộc gốc của kho tệp, trỏ bucket private, hạn ≤ 5 phút, đọc được; nhân viên Page khác → ErrNotFound;
// fileId của hội thoại khác → ErrNotFound; ảnh chưa xử lý xong → ErrAttachmentNotReady.
func TestInboundImage_TP02(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	raw := jpegWithGPS("anh-1")
	u := e.chat.AddFile("/i/1.jpg", raw)

	e.receive(e.tenant, "u1", "m-img", img(u))
	it, conv := e.one("m-img")
	if it.Status != "stored" || it.FileID == nil || it.DownloadOnly || it.URL != "" {
		t.Fatalf("mục sau khi tải: %+v", it)
	}
	id := *it.FileID
	if n := e.jobs("command.fb.fetch_attachment", "done"); n != 1 {
		t.Fatalf("lệnh tải done = %d", n)
	}
	if n, m := e.jobs("file.process_private", ""), e.jobs("file.process", ""); n != 1 || m != 0 {
		t.Fatalf("file.process_private = %d, file.process = %d", n, m)
	}
	if tmp := e.store.Keys(files.BucketTmp); len(tmp) != 1 || tmp[0] != files.TmpKey(e.tenant, id) {
		t.Fatalf("crm-tmp = %v", tmp)
	}
	if pub, orig := e.store.Keys(files.BucketPublic), e.store.Keys(files.BucketOriginals); len(pub)+len(orig) != 0 {
		t.Fatalf("ảnh khách lọt ra public %v / originals %v", pub, orig)
	}
	var status, purpose string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT status, purpose FROM platform.files WHERE id = $1`, id).Scan(&status, &purpose)
	})
	if status != "ready" || purpose != conversation.PurposeChatImage {
		t.Fatalf("tệp %s/%s", purpose, status)
	}
	prefix := e.checkPrivateVariants(id)

	before := time.Now()
	l, err := e.link(e.reader, conv, id)
	if err != nil {
		t.Fatalf("đường dẫn: %v", err)
	}
	pu, _ := url.Parse(l.URL)
	exp, _ := strconv.ParseInt(pu.Query().Get("exp"), 10, 64)
	switch {
	case !strings.HasPrefix(l.URL, e.storeBase()+"/private/"+prefix):
		t.Errorf("đường dẫn không trỏ biến thể private: %s", l.URL)
	case pu.Query().Get("cd") != "":
		t.Errorf("ảnh phải mở trong trình duyệt, không attachment: %s", l.URL)
	case l.ExpiresAt.Sub(before) > 5*time.Minute+time.Second || time.Unix(exp, 0).Sub(before) > 5*time.Minute+time.Second:
		t.Errorf("hạn quá 5 phút: %v, exp %d", l.ExpiresAt, exp)
	}
	if _, err := httpGet(t.Context(), l.URL); err != nil {
		t.Errorf("đọc đường dẫn: %v", err)
	}
	if _, err := httpGet(t.Context(), l.URL+"x"); err == nil {
		t.Error("chữ ký sai mà vẫn đọc được")
	}
	if _, err := e.link(e.outsdr, conv, id); !errors.Is(err, conversation.ErrNotFound) {
		t.Errorf("nhân viên Page khác: %v", err)
	}

	// Tin khác ở hội thoại khác (khách khác), ảnh để chưa xử lý: fileId của hội thoại kia → 404; chưa xong → 422.
	e.chat.AddFile("/i/2.jpg", jpegWithGPS("anh-2"))
	e.receiveNoProcess("u2", "m-img2", img("https://"+cdnHost()+"/i/2.jpg"))
	it2, conv2 := e.one("m-img2")
	if it2.FileID == nil {
		t.Fatalf("mục 2: %+v", it2)
	}
	if _, err := e.link(e.reader, conv, *it2.FileID); !errors.Is(err, conversation.ErrNotFound) {
		t.Errorf("fileId của hội thoại khác: %v", err)
	}
	if _, err := e.link(e.reader, conv2, *it2.FileID); !errors.Is(err, conversation.ErrAttachmentNotReady) {
		t.Errorf("ảnh chưa xử lý: %v", err)
	}
}

// Ảnh hỏng (R10): JPEG cụt → imager giả trả decode_failed → tệp rejected, 0 biến thể, đường dẫn →
// ErrAttachmentRejected.
func TestInboundImage_Truncated(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	u := e.chat.AddFile("/i/cut.jpg", []byte(truncatedMarker+"phần còn lại bị cắt"))
	e.receive(e.tenant, "u1", "m-cut", img(u))
	it, conv := e.one("m-cut")
	if it.FileID == nil {
		t.Fatalf("mục: %+v", it)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.files WHERE id = $1 AND status = 'rejected'`, *it.FileID); n != 1 {
		t.Fatal("tệp cụt phải rejected")
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.file_variants WHERE file_id = $1`, *it.FileID); n != 0 {
		t.Fatalf("%d biến thể của tệp hỏng", n)
	}
	if _, err := e.link(e.reader, conv, *it.FileID); !errors.Is(err, conversation.ErrAttachmentRejected) {
		t.Fatalf("đường dẫn tệp hỏng: %v", err)
	}
}

// TP04: PDF, HTML khai là ảnh (loại theo nội dung text/html), SVG → chat_attachment ready, downloadOnly; đối tượng ở
// bucket private ghi application/octet-stream; đường dẫn có Content-Disposition: attachment (tên tep-…), trả
// application/octet-stream; không lệnh xử lý ảnh nào.
func TestInboundFile_TP04(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	cases := []struct{ mid, path, wantType, ext string }{
		{"m-pdf", "/f/a.pdf", "application/pdf", ".pdf"},
		{"m-html", "/f/fake.jpg", "text/html; charset=utf-8", ".html"},
		{"m-svg", "/f/a.svg", "text/xml; charset=utf-8", ".xml"},
	}
	bodies := map[string][]byte{
		"/f/a.pdf":    []byte("%PDF-1.7\n1 0 obj<<>>endobj\n"),
		"/f/fake.jpg": []byte("<!DOCTYPE html><html><script>alert(1)</script></html>"),
		"/f/a.svg":    []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`),
	}
	for i, c := range cases {
		u := e.chat.AddFile(c.path, bodies[c.path])
		ref := file(u)
		if c.mid == "m-html" {
			ref = img(u) // kênh khai là ảnh
		}
		e.receive(e.tenant, "u"+strconv.Itoa(i), c.mid, ref)
		it, conv := e.one(c.mid)
		if it.Status != "stored" || it.FileID == nil || !it.DownloadOnly {
			t.Fatalf("%s: mục %+v", c.mid, it)
		}
		var purpose, status, ct, name string
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT purpose, status, declared_type, original_name FROM platform.files
				WHERE id = $1`, *it.FileID).Scan(&purpose, &status, &ct, &name)
		})
		if purpose != conversation.PurposeChatAttachment || status != "ready" || ct != c.wantType ||
			!strings.HasPrefix(name, "tep-") || !strings.HasSuffix(name, c.ext) {
			t.Fatalf("%s: tệp %s/%s/%s/%s", c.mid, purpose, status, ct, name)
		}
		l, err := e.link(e.reader, conv, *it.FileID)
		if err != nil {
			t.Fatalf("%s: đường dẫn %v", c.mid, err)
		}
		body, h, err := httpGetFull(t.Context(), l.URL)
		if err != nil || !bytes.Equal(body, bodies[c.path]) {
			t.Fatalf("%s: đọc %v", c.mid, err)
		}
		if h.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") ||
			!strings.Contains(h.Get("Content-Disposition"), name) {
			t.Fatalf("%s: header %v", c.mid, h)
		}
	}
	if n := e.jobs("file.process_private", "") + e.jobs("file.process", ""); n != 0 {
		t.Fatalf("%d lệnh xử lý ảnh cho tệp không phải ảnh", n)
	}
	if n := len(e.store.Keys(files.BucketTmp)); n != 0 {
		t.Fatalf("tệp không phải ảnh mà ghi crm-tmp: %d", n)
	}
}

// Địa chỉ ngoài máy chủ của kênh → lệnh dead, mục blocked, CDN không nhận yêu cầu nào; tệp quá 25 MiB → too_large;
// CDN không có tệp → unavailable; không dòng tệp nào. Hai tệp trong một tin → hai lệnh, mỗi mục đúng trạng thái của
// nó (lệnh này không đè mục của lệnh kia).
func TestInboundAttachment_Refusals(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	e.receive(e.tenant, "u1", "m-evil", img("https://evil.example/a.jpg"))
	if it, _ := e.one("m-evil"); it.Status != "blocked" || it.FileID != nil || it.URL != "" {
		t.Fatalf("máy lạ: %+v", it)
	}
	if n := e.chat.DownloadCalls(); n != 0 {
		t.Fatalf("CDN nhận %d yêu cầu cho địa chỉ lạ", n)
	}
	if n := e.jobs("command.fb.fetch_attachment", "dead"); n != 1 {
		t.Fatalf("lệnh dead = %d", n)
	}
	big := make([]byte, 26<<20)
	copy(big, jpegWithGPS("to"))
	ub := e.chat.AddFile("/i/big.jpg", big)
	ok := e.chat.AddFile("/f/ok.pdf", []byte("%PDF-1.4 ok"))
	e.receive(e.tenant, "u2", "m-two", img(ub), file("https://"+cdnHost()+"/khong-co.jpg"), file(ok))
	its, _ := e.items(e.tenant, "m-two")
	if len(its) != 3 || its[0].Status != "too_large" || its[1].Status != "unavailable" || its[2].Status != "stored" ||
		its[2].FileID == nil {
		t.Fatalf("ba mục: %+v", its)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.files`); n != 1 {
		t.Fatalf("%d dòng tệp, muốn 1", n)
	}
}

// Hai tin cùng một ảnh (cùng nội dung) ở tenant A → hai mục, imager được gọi 1 lần, hai tệp ready trỏ cùng khóa biến
// thể dưới priv/<A>/ (chống trùng của file.process_private); tenant B cùng ảnh → imager gọi thêm, biến thể riêng
// dưới priv/<B>/.
func TestInboundImage_Dedupe(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	raw := jpegWithGPS("cung-noi-dung")
	u1, u2 := e.chat.AddFile("/i/a.jpg", raw), e.chat.AddFile("/i/b.jpg", raw)
	e.receive(e.tenant, "u1", "m-a", img(u1))
	e.receive(e.tenant, "u2", "m-b", img(u2))
	a, _ := e.one("m-a")
	b, _ := e.one("m-b")
	if a.FileID == nil || b.FileID == nil || *a.FileID == *b.FileID {
		t.Fatalf("hai mục: %+v %+v", a, b)
	}
	if n := e.img.count(); n != 1 {
		t.Fatalf("imager gọi %d lần, muốn 1", n)
	}
	keys := func(tenant, id uuid.UUID) []string {
		return e.texts(tenant, `SELECT key FROM platform.file_variants WHERE file_id = $1 ORDER BY key`, id)
	}
	ka, kb := keys(e.tenant, *a.FileID), keys(e.tenant, *b.FileID)
	if len(ka) == 0 || strings.Join(ka, ",") != strings.Join(kb, ",") || !strings.HasPrefix(ka[0], "priv/"+e.tenant.String()+"/") {
		t.Fatalf("biến thể A: %v / %v", ka, kb)
	}
	e.receive(e.tenB, "u1", "m-c", img(e.chat.AddFile("/i/c.jpg", raw)))
	cs, _ := e.items(e.tenB, "m-c")
	if len(cs) != 1 || cs[0].FileID == nil {
		t.Fatalf("tenant B: %+v", cs)
	}
	if n := e.img.count(); n != 2 {
		t.Fatalf("imager sau tenant B: %d, muốn 2", n)
	}
	if kc := keys(e.tenB, *cs[0].FileID); len(kc) == 0 || !strings.HasPrefix(kc[0], "priv/"+e.tenB.String()+"/") {
		t.Fatalf("biến thể B: %v", kc)
	}
}

// checkPrivateVariants: tệp có biến thể, mọi biến thể ở bucket private dưới priv/<tenant>/<tệp>/, mọi đối tượng private
// nằm dưới tiền tố đó và không còn khối GPS của ảnh thô; trả tiền tố.
func (e *fileEnv) checkPrivateVariants(id uuid.UUID) string {
	t := e.t
	t.Helper()
	prefix := "priv/" + e.tenant.String() + "/" + id.String() + "/"
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.file_variants WHERE file_id = $1`, id); n == 0 {
		t.Fatal("không có biến thể")
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.file_variants WHERE file_id = $1
		AND (bucket <> 'private' OR key NOT LIKE $2 || '%')`, id, prefix); n != 0 {
		t.Fatalf("%d biến thể ngoài bucket private / %s", n, prefix)
	}
	for _, k := range e.store.Keys(files.BucketPrivate) {
		if !strings.HasPrefix(k, prefix) {
			t.Errorf("khóa private ngoài tiền tố của tệp: %s", k)
		}
		if vs := e.store.Versions(files.BucketPrivate, k); len(vs) > 0 && bytes.Contains(vs[len(vs)-1], []byte("GPS")) {
			t.Errorf("biến thể %s còn GPS", k)
		}
	}

	return prefix
}

// Tệp 0 byte (Task 496; phát hiện của 155): CDN trả thân rỗng → mục unavailable (không giữ pending mãi), lệnh dead với
// lý do empty_file (không in_doubt), không dòng tệp, không ghi gì lên kho.
func TestInboundAttachment_EmptyFile(t *testing.T) {
	t.Parallel()
	e := newFileEnv(t)
	u := e.chat.AddFile("/f/rong.pdf", []byte{})
	e.receive(e.tenant, "u1", "m-empty", file(u))
	if it, _ := e.one("m-empty"); it.Status != "unavailable" || it.FileID != nil || it.URL != "" {
		t.Fatalf("tệp 0 byte: %+v, muốn unavailable, không fileId, không url", it)
	}
	if n := e.staff.Count(t, `SELECT count(*) FROM platform.jobs WHERE kind = 'command.fb.fetch_attachment'
		AND status = 'dead' AND last_error = 'rejected: empty_file'`); n != 1 {
		t.Fatalf("lệnh tải dead empty_file = %d, muốn 1", n)
	}
	if n := e.staff.Count(t, `SELECT count(*) FROM platform.jobs WHERE status = 'in_doubt'`); n != 0 {
		t.Fatalf("%d việc in_doubt, muốn 0", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.files`); n != 0 {
		t.Fatalf("%d dòng tệp, muốn 0", n)
	}
}
