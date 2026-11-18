package files_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/testkit"
)

const sheetType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// ReadContent: tệp uploaded đúng mục đích → nội dung; sai mục đích, chưa tải xong, đã xóa → ErrNotReady/ErrNotFound;
// tenant khác, không có → ErrNotFound; vượt maxBytes → ErrTooLarge; mất trên kho → ErrObjectNotFound.
func TestReadContent(t *testing.T) {
	e := newEnv(t)
	mem := testkit.MemFileStore(t)
	upload := func(tenant uuid.UUID, body []byte) uuid.UUID {
		t.Helper()
		f, _ := e.mustCreate(mem, files.UploadInput{
			TenantID: tenant, Purpose: "catalog_import", FileName: "a.xlsx", ContentType: sheetType, SizeBytes: int64(len(body)),
		})
		if err := mem.Put(t.Context(), files.BucketTmp, files.TmpKey(tenant, f.ID), bytes.NewReader(body), int64(len(body)), sheetType); err != nil {
			t.Fatal(err)
		}
		if _, err := e.complete(mem, tenant, f.ID); err != nil {
			t.Fatal(err)
		}
		return f.ID
	}
	read := func(tenant, id uuid.UUID, purpose string, maxBytes int64) ([]byte, error) {
		return files.ReadContent(t.Context(), e.staff.App, mem, tenant, id, purpose, maxBytes)
	}
	body := []byte("PK nội dung thử của tệp nhập")
	ok := upload(e.a, body)
	if got, err := read(e.a, ok, "catalog_import", 1<<20); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("đọc tệp: %q %v", got, err)
	}
	waiting, _ := e.mustCreate(mem, files.UploadInput{
		TenantID: e.a, Purpose: "catalog_import", FileName: "b.xlsx", ContentType: sheetType, SizeBytes: 5,
	})
	deleted := upload(e.a, body)
	e.must(e.a, func(tx pgx.Tx) error { return files.MarkDeleted(t.Context(), tx, deleted, e.actor) })
	lost := upload(e.a, body)
	if err := mem.Delete(t.Context(), files.BucketTmp, files.TmpKey(e.a, lost)); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		tenant, id uuid.UUID
		purpose    string
		max        int64
		want       error
	}{
		"sai mục đích":  {e.a, ok, "product_image", 1 << 20, files.ErrNotReady},
		"chưa tải xong": {e.a, waiting.ID, "catalog_import", 1 << 20, files.ErrNotReady},
		"đã xóa":        {e.a, deleted, "catalog_import", 1 << 20, files.ErrNotFound},
		"tenant khác":   {e.b, ok, "catalog_import", 1 << 20, files.ErrNotFound},
		"không có":      {e.a, uuid.Must(uuid.NewV7()), "catalog_import", 1 << 20, files.ErrNotFound},
		"quá maxBytes":  {e.a, ok, "catalog_import", 4, files.ErrTooLarge},
		"mất trên kho":  {e.a, lost, "catalog_import", 1 << 20, files.ErrObjectNotFound},
	} {
		if _, err := read(c.tenant, c.id, c.purpose, c.max); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, mong %v", name, err, c.want)
		}
	}
}
