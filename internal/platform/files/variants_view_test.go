package files_test

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/testkit"
)

// usePrivatePurpose đăng ký mục đích thử test_private_image (chế độ private, R10) ở sổ Go và bảng.
func (e *env) usePrivatePurpose() {
	e.t.Helper()
	files.RegisterPurpose(testPrivateImagePurpose())
	if err := e.addPurposeRow("test_private_image", "private", "private"); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) viewRows(tenant, fileID uuid.UUID) int {
	e.t.Helper()
	var n int
	e.must(tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT count(*) FROM platform.file_variants_v WHERE file_id = $1", fileID).Scan(&n)
	})
	return n
}

// platform.file_variants_v (A2-12): tệp ready → 12 dòng, đúng cột theo docs/views.md, security_invoker; lost,
// MarkDeleted → 0 dòng; tenant B đọc → 0 dòng của A; tệp ready của mục đích private → 0 dòng (R10).
func TestFileVariantsView(t *testing.T) {
	e := newEnv(t)
	e.usePrivatePurpose()
	var ready, lost, deleted, private uuid.UUID
	e.must(e.a, func(tx pgx.Tx) error {
		ready = filestest.Image(t, tx, e.a, "product_image")
		lost = filestest.Image(t, tx, e.a, "product_image")
		deleted = filestest.Image(t, tx, e.a, "product_image")
		private = filestest.Image(t, tx, e.a, "test_private_image")
		filestest.MarkLost(t, tx, lost)
		return files.MarkDeleted(t.Context(), tx, deleted, e.actor)
	})
	if n := e.viewRows(e.a, ready); n != 12 {
		t.Errorf("tệp ready: mong 12 dòng, có %d", n)
	}
	for name, id := range map[string]uuid.UUID{"lost": lost, "đã xóa": deleted, "private": private} {
		if n := e.viewRows(e.a, id); n != 0 {
			t.Errorf("tệp %s: mong 0 dòng, có %d", name, n)
		}
	}
	if n := e.viewRows(e.b, ready); n != 0 {
		t.Errorf("tenant B đọc tệp của A: mong 0 dòng, có %d", n)
	}
	var cols []string
	var opts []string
	e.must(e.a, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), "SELECT * FROM platform.file_variants_v WHERE file_id = $1 LIMIT 1", ready)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, f := range rows.FieldDescriptions() {
			cols = append(cols, f.Name)
		}
		rows.Close()
		return nil
	})
	if want := []string{"file_id", "preset_version", "width", "format", "key"}; !slices.Equal(cols, want) {
		t.Errorf("cột của view: mong %v, có %v", want, cols)
	}
	e.staff.Exec(t, e.a, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT coalesce(reloptions, '{}') FROM pg_class
			WHERE oid = 'platform.file_variants_v'::regclass`).Scan(&opts)
	})
	if !slices.Contains(opts, "security_invoker=true") {
		t.Errorf("view phải security_invoker=true, có %v", opts)
	}
	var key string
	e.must(e.a, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT key FROM platform.file_variants_v WHERE file_id = $1 AND width = 320
			AND format = 'webp'`, ready).Scan(&key)
	})
	if !strings.HasPrefix(key, "img/") || !strings.HasSuffix(key, "/v1/320.webp") {
		t.Errorf("khóa biến thể sai: %s", key)
	}
}

func variant(preset, width int, format string) files.VariantURL {
	return files.VariantURL{Preset: preset, Width: width, Format: format, URL: "u"}
}

func fullPreset(v int) []files.VariantURL {
	specs, _ := files.Preset(1)
	out := make([]files.VariantURL, 0, len(specs))
	for _, s := range specs {
		out = append(out, variant(v, s.Width, s.Format))
	}
	return out
}

// SelectPreset: bản want đủ thì dùng; thiếu thì lùi về bản nhỏ hơn đủ gần nhất; không bản nào đủ → nil; không bao giờ
// trả bản lớn hơn want.
func TestSelectPreset(t *testing.T) {
	v1 := fullPreset(1)
	if got := files.SelectPreset(v1, 1); len(got) != 12 {
		t.Errorf("v1 đủ, muốn 1: mong 12, có %d", len(got))
	}
	if got := files.SelectPreset(v1, 2); len(got) != 12 || got[0].Preset != 1 {
		t.Errorf("muốn 2 mà chỉ có v1: phải lùi về v1, có %v", got)
	}
	if got := files.SelectPreset(v1[:11], 1); got != nil {
		t.Errorf("v1 thiếu một biến thể: mong nil, có %v", got)
	}
	if got := files.SelectPreset(nil, 1); got != nil {
		t.Errorf("không có biến thể: mong nil, có %v", got)
	}
	rapid.Check(t, func(rt *rapid.T) {
		want := rapid.IntRange(1, 3).Draw(rt, "want")
		var in []files.VariantURL
		for _, f := range rapid.SliceOfN(rapid.IntRange(0, 12*3-1), 0, 40).Draw(rt, "picks") {
			specs, _ := files.Preset(1)
			s := specs[f%12]
			in = append(in, variant(f/12+1, s.Width, s.Format))
		}
		got := files.SelectPreset(in, want)
		for _, v := range got {
			if v.Preset > want || v.Preset != got[0].Preset {
				rt.Fatalf("bản sai: %+v (muốn %d)", v, want)
			}
		}
		for _, v := range got {
			if !slices.Contains(in, v) {
				rt.Fatalf("trả biến thể không có ở đầu vào: %+v", v)
			}
		}
	})
}

// DownloadURL của ảnh sản phẩm ký tới ảnh gốc orig/<sha> trên crm-originals (không phải tmp/… tự xóa sau 24 giờ).
func TestDownloadURL_ProductImageSignsOriginal(t *testing.T) {
	e := newEnv(t)
	mem := testkit.MemFileStore(t)
	var id uuid.UUID
	var sha string
	e.must(e.a, func(tx pgx.Tx) error {
		id = filestest.Image(t, tx, e.a, "product_image")
		return tx.QueryRow(t.Context(), "SELECT encode(content_sha256, 'hex') FROM platform.files WHERE id = $1", id).Scan(&sha)
	})
	body := randomBytes(t, 32)
	if err := mem.Put(t.Context(), files.BucketOriginals, files.OriginalKey(sha), strings.NewReader(string(body)),
		int64(len(body)), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	url, err := e.downloadURL(mem, e.a, id, time.Minute)
	if err != nil {
		t.Fatalf("DownloadURL: %v", err)
	}
	if !strings.Contains(url, "/originals/orig/"+sha) || strings.Contains(url, "/tmp/") {
		t.Fatalf("phải ký tới ảnh gốc trên originals, có %s", url)
	}
	resp, err := http.Get(url) //nolint:gosec,noctx // đường dẫn của MemStore trong test
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(got) != string(body) {
		t.Fatalf("đọc ảnh gốc: %d", resp.StatusCode)
	}
}
