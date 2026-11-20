package files_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

const kindVerify = "file.verify"

// verify xếp một lượt đối chiếu kho cho tenant (như POST /api/v1/platform/files/verify) và trả mã lệnh đầu.
func (p *procEnv) verify(tenant uuid.UUID) uuid.UUID {
	p.t.Helper()
	var id uuid.UUID
	p.must(tenant, func(tx pgx.Tx) error {
		var err error
		id, err = files.EnqueueVerify(p.t.Context(), tx, p.q)
		return err //nolint:wrapcheck // test
	})
	return id
}

// serverFile ghi body lên kho rồi Register (đường máy chủ ghi, mục đích catalog_import) và trả mã tệp; expires khác
// nil là tệp tạm.
func (p *procEnv) serverFile(tenant uuid.UUID, body []byte, put bool, expires *time.Time) uuid.UUID {
	p.t.Helper()
	id := uuid.Must(uuid.NewV7())
	key := files.ServerKey(tenant, id)
	if put {
		if err := p.mem.Put(p.t.Context(), files.BucketPrivate, key, bytes.NewReader(body), int64(len(body)), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"); err != nil {
			p.t.Fatal(err)
		}
	}
	p.must(tenant, func(tx pgx.Tx) error {
		_, err := files.Register(p.t.Context(), tx, files.NewFile{
			ID: id, Purpose: "catalog_import", Key: key, Size: int64(len(body)), ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			FileName: "mau.xlsx", ExpiresAt: expires,
		})
		return err //nolint:wrapcheck // test
	})
	return id
}

// lostAt đọc lost_at của tệp bằng chủ bảng (nil khi chưa mất).
func (p *procEnv) lostAt(id uuid.UUID) *time.Time {
	p.t.Helper()
	var at *time.Time
	p.asOwner(func(tx pgx.Tx) (bool, error) {
		err := tx.QueryRow(p.t.Context(), "SELECT lost_at FROM platform.files WHERE id = $1", id).Scan(&at)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return err == nil, err //nolint:wrapcheck // test
	})
	return at
}

func (p *procEnv) wantStatus(want string, ids ...uuid.UUID) {
	p.t.Helper()
	for _, id := range ids {
		if r := p.fileRow(id); r.Status != want {
			p.t.Errorf("tệp %s: trạng thái %s, muốn %s", id, r.Status, want)
		}
	}
}

// TP12: đối chiếu kho đánh dấu lost đúng các tệp ready thiếu ảnh gốc, thiếu một biến thể, hay thiếu đối tượng của
// tệp máy chủ ghi; tệp đủ, ảnh riêng tư đủ biến thể, tệp tạm, tệp của tenant khác giữ ready. Lô nhỏ (2 tệp) để đi qua
// đường nối lệnh lô sau. Tải lại đúng ảnh đã mất (mất ảnh gốc, hay mất một biến thể) → tệp mất về ready qua
// file.process với đủ đối tượng trên kho; tệp khác vẫn lost.
func TestVerifier_MarksMissingLost(t *testing.T) {
	p := newProcEnv(t)
	files.SetVerifyBatch(p.ver, 2)
	bodyX, bodyY := []byte("anh-x-mat-anh-goc"), []byte("anh-y-mat-bien-the")
	x, y := p.upload(p.a, "product_image", bodyX), p.upload(p.a, "product_image", bodyY)
	ok1, ok2 := p.upload(p.a, "product_image", []byte("anh-z1")), p.upload(p.a, "product_image", []byte("anh-z2"))
	priv := p.registerPrivate(p.a, []byte("anh-khach-rieng-tu"))
	srv := p.serverFile(p.a, []byte("ma,ten\n"), true, nil)
	srvGone := p.serverFile(p.a, []byte("ma,ten\nA,B\n"), true, nil)
	exp := time.Now().Add(time.Hour)
	tmp := p.serverFile(p.a, []byte("xuat"), false, &exp) // tệp tạm: không bao giờ đánh dấu mất
	bodyB := []byte("anh-cua-tenant-b")
	bx := p.upload(p.b, "product_image", bodyB)
	p.drain()
	p.wantStatus(files.StatusReady, x, y, ok1, ok2, priv, srv, srvGone, tmp, bx)

	ctx := t.Context()
	specs, _ := files.Preset(1)
	goneVariant := files.VariantKey(shaHex(bodyY), 1, specs[len(specs)-1].Width, specs[len(specs)-1].Format)
	for _, del := range []struct {
		b   files.Bucket
		key string
	}{
		{files.BucketOriginals, files.OriginalKey(shaHex(bodyX))},
		// Tệp tải lên tạm của x đã hết hạn như ngoài đời (lifecycle crm-tmp): dựng lại phải đọc từ ảnh gốc tải lại
		// (Task 403: MemStore không tự xóa crm-tmp nên đột biến bỏ nhánh "tệp mất" trước đây vẫn qua).
		{files.BucketTmp, files.TmpKey(p.a, x)},
		{files.BucketPublic, goneVariant},
		{files.BucketPrivate, files.ServerKey(p.a, srvGone)},
		{files.BucketOriginals, files.OriginalKey(shaHex(bodyB))},
	} {
		if err := p.mem.Delete(ctx, del.b, del.key); err != nil {
			t.Fatal(err)
		}
	}

	first := p.verify(p.a)
	p.drain()
	p.wantStatus(files.StatusLost, x, y, srvGone)
	p.wantStatus(files.StatusReady, ok1, ok2, priv, srv, tmp, bx)
	for _, id := range []uuid.UUID{x, y, srvGone} {
		if p.lostAt(id) == nil {
			t.Errorf("tệp mất %s thiếu lost_at", id)
		}
	}
	// 8 tệp ready của A, lô 2 → ít nhất 4 lệnh nối nhau, mọi lệnh done.
	runs := p.staff.Count(t, `SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'done'
		AND payload->>'runId' = (SELECT payload->>'runId' FROM platform.jobs WHERE id = $2)`, kindVerify, first)
	if runs < 4 || p.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status <> 'done'", kindVerify) != 0 {
		t.Fatalf("lượt đối chiếu có %d lệnh done (muốn ≥ 4), hoặc còn lệnh chưa xong", runs)
	}
	sigs := p.bus.signals()
	if !slices.ContainsFunc(sigs, func(s realtime.Signal) bool { return s.Entity.ID == x.String() && s.TenantID == p.a }) {
		t.Errorf("không có báo hiệu cho tệp mất %s", x)
	}

	x2, y2 := p.checkReupload(x, y, srvGone, bodyX, bodyY, goneVariant)

	// Lượt sau: tệp lost không bị kiểm lại, tệp đã phục hồi đủ đối tượng giữ ready.
	p.verify(p.a)
	p.drain()
	p.wantStatus(files.StatusReady, x, x2, y, y2, ok1, ok2)
	p.wantStatus(files.StatusLost, srvGone)
}

// checkReupload: tải lại đúng ảnh x (mất ảnh gốc) rồi ảnh y (mất một biến thể, bản kê còn) → tệp mới ready, tệp mất
// về ready với đủ đối tượng trên kho (bản kê không được tin khi thiếu biến thể); srvGone vẫn lost. Trả hai tệp mới.
func (p *procEnv) checkReupload(x, y, srvGone uuid.UUID, bodyX, bodyY []byte, goneVariant string) (x2, y2 uuid.UUID) {
	p.t.Helper()
	t := p.t
	ctx := t.Context()
	// Tải lại đúng ảnh x (nội dung giống) → tệp mới ready, x về ready, lost_at xóa; y vẫn lost.
	x2 = p.upload(p.a, "product_image", bodyX)
	p.drain()
	p.drain()
	p.wantStatus(files.StatusReady, x2, x)
	p.wantStatus(files.StatusLost, y, srvGone)
	if p.lostAt(x) != nil {
		t.Error("tệp x đã ready mà còn lost_at")
	}
	if n := p.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key LIKE $2",
		kindPublic, x.String()+":restore:%"); n != 1 {
		t.Fatalf("muốn 1 lệnh file.process phục hồi cho x, có %d", n)
	}
	if info := p.info(p.a, x); len(info.Variants) != 12 {
		t.Fatalf("x sau phục hồi có %d biến thể", len(info.Variants))
	}

	// Tải lại ảnh y (mất một biến thể, bản kê còn): bản kê không được tin khi thiếu biến thể → làm lại biến thể thiếu,
	// y về ready với đủ 12 biến thể trên kho.
	y2 = p.upload(p.a, "product_image", bodyY)
	p.drain()
	p.drain()
	p.wantStatus(files.StatusReady, y2, y)
	if _, err := p.mem.Head(ctx, files.BucketPublic, goneVariant); err != nil {
		t.Fatalf("biến thể đã mất của y chưa được làm lại: %v", err)
	}

	return x2, y2
}

// failingStore là MemStore mà Head lỗi mạng (không phải "không có").
type failingStore struct{ *files.MemStore }

func (failingStore) Head(context.Context, files.Bucket, string) (files.ObjectInfo, error) {
	return files.ObjectInfo{}, errors.New("kết nối bị đóng")
}

// jobOf đọc lệnh id như worker giao cho handler.
func (p *procEnv) jobOf(id uuid.UUID) queue.Job {
	p.t.Helper()
	j := queue.Job{ID: id, Kind: kindVerify, Attempts: 1}
	var tenant uuid.UUID
	if err := p.staff.Owner.Pool.QueryRow(p.t.Context(), "SELECT tenant_id, payload FROM platform.jobs WHERE id = $1", id).
		Scan(&tenant, &j.Payload); err != nil {
		p.t.Fatal(err)
	}
	j.TenantID = &tenant
	return j
}

// Kho lỗi tạm (Head lỗi khác "không có") → not_sent, không tệp nào thành lost (lỗi mạng không phải tệp mất).
func TestVerifier_StoreErrorMarksNothing(t *testing.T) {
	p := newProcEnv(t)
	x := p.upload(p.a, "product_image", []byte("anh-kho-loi"))
	p.drain()
	v := files.NewVerifyHandler(files.ProcessorOptions{DB: p.staff.App, Store: failingStore{p.mem}})
	out, err := v.Execute(t.Context(), p.jobOf(p.verify(p.a)))
	if err != nil || out.Status != commands.StatusNotSent {
		t.Fatalf("kho lỗi: muốn not_sent, nhận %+v %v", out, err)
	}
	p.wantStatus(files.StatusReady, x)
}

// 6.1: tệp đổi (version tăng) giữa Execute và OnDone → không đánh dấu nhầm; OnDone chạy lại không xếp trùng lệnh lô sau.
func TestVerifier_ChangedFileNotMarked(t *testing.T) {
	p := newProcEnv(t)
	files.SetVerifyBatch(p.ver, 1)
	body := []byte("anh-doi-giua-chung")
	x := p.upload(p.a, "product_image", body)
	p.upload(p.a, "product_image", []byte("anh-thu-hai"))
	p.drain()
	if err := p.mem.Delete(t.Context(), files.BucketOriginals, files.OriginalKey(shaHex(body))); err != nil {
		t.Fatal(err)
	}
	j := p.jobOf(p.verify(p.a))
	out, err := p.ver.Execute(t.Context(), j)
	if err != nil || out.Status != commands.StatusDone {
		t.Fatalf("Execute: %+v %v", out, err)
	}
	var res struct {
		Missing []struct {
			ID uuid.UUID `json:"id"`
		} `json:"missing"`
		Next string `json:"next"`
	}
	if err := json.Unmarshal(out.Result, &res); err != nil || len(res.Missing) != 1 || res.Missing[0].ID != x || res.Next == "" {
		t.Fatalf("kết quả lô đầu: %s %v", out.Result, err)
	}
	p.asOwner(func(tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(t.Context(), "UPDATE platform.files SET version = version + 1 WHERE id = $1", x)
		return tag.RowsAffected() == 1, err //nolint:wrapcheck // test
	})
	for range 2 {
		p.must(p.a, func(tx pgx.Tx) error { return p.ver.OnDone(t.Context(), j, out.Result, tx) })
	}
	p.wantStatus(files.StatusReady, x)
	if n := p.staff.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = $1", kindVerify); n != 2 {
		t.Fatalf("muốn 2 lệnh (lô đầu + đúng một lô sau), có %d", n)
	}
}
