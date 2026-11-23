package inventory_test

// Ca thêm ở Task 403 (mutation testing vùng cao M1, docs/mutation.md): biên số dòng của phiếu và limit, trang cuối của
// ListDocuments.

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Phiếu đúng MaxDocumentLines dòng được; thêm một dòng → ErrInvalidInput.
func TestReplaceLines_MaxLines(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	vs := e.variants(inventory.MaxDocumentLines + 1)
	lines := make([]inventory.DocumentLine, len(vs))
	for i, v := range vs {
		lines[i] = line(v, 1)
	}
	d, err := e.createDoc(inventory.DocumentInput{Kind: inventory.KindImport})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.replaceLines(d.ID, d.Version, lines...); !errors.Is(err, inventory.ErrInvalidInput) {
		t.Fatalf("%d dòng: mong ErrInvalidInput, có %v", len(lines), err)
	}
	d2, err := e.replaceLines(d.ID, d.Version, lines[:inventory.MaxDocumentLines]...)
	if err != nil || len(d2.Lines) != inventory.MaxDocumentLines {
		t.Fatalf("đúng %d dòng: %d dòng, %v", inventory.MaxDocumentLines, len(d2.Lines), err)
	}
}

// limit ngoài 1…200 → ErrInvalidInput; 1 và 200 được. Đúng limit phiếu → không có trang sau.
func TestListDocuments_LimitBounds(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(10)
	for range 2 {
		e.newDoc(inventory.DocumentInput{Kind: inventory.KindExport}, line(v, 1))
	}
	list := func(limit int32) (int, bool, error) {
		var n int
		var more bool
		err := e.app(func(tx pgx.Tx) error {
			out, next, err := inventory.ListDocuments(t.Context(), tx, inventory.DocumentFilter{AllWarehouses: true, Limit: limit})
			n, more = len(out), next != nil
			return err //nolint:wrapcheck // test
		})
		return n, more, err
	}
	for _, limit := range []int32{0, -1, 201} {
		if _, _, err := list(limit); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("limit %d: mong ErrInvalidInput, có %v", limit, err)
		}
	}
	if n, more, err := list(1); err != nil || n != 1 || !more {
		t.Errorf("limit 1: %d phiếu, còn %v, %v", n, more, err)
	}
	if n, more, err := list(200); err != nil || n != 2 || more {
		t.Errorf("limit 200: %d phiếu, còn %v, %v", n, more, err)
	}
	if n, more, err := list(2); err != nil || n != 2 || more {
		t.Errorf("limit đúng số phiếu: %d phiếu, còn %v (mong không có trang sau), %v", n, more, err)
	}
}

// Writer thiếu Events hoặc thiếu Queue → ghi sổ phiếu lỗi, tồn không đổi (đổi tồn không bao giờ thiếu sự kiện).
func TestPostDocument_WriterNotWired(t *testing.T) {
	t.Parallel()
	e := newInvEnv(t)
	v := e.stock(10)
	full := e.writer()
	for name, w := range map[string]inventory.Writer{
		"thiếu Queue":  {Events: full.Events, Realtime: full.Realtime},
		"thiếu Events": {Queue: full.Queue, Realtime: full.Realtime},
	} {
		d := e.newDoc(inventory.DocumentInput{Kind: inventory.KindImport}, line(v, 1))
		if err := e.app(func(tx pgx.Tx) error {
			_, err := w.PostDocument(t.Context(), tx, d.ID, d.Version, staffActor())
			return err //nolint:wrapcheck // test
		}); err == nil {
			t.Errorf("%s: ghi sổ phải lỗi", name)
		}
		if b := e.balance(v); b.OnHand != 10 {
			t.Errorf("%s: on_hand %d, mong 10", name, b.OnHand)
		}
	}
}
