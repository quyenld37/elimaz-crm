package catalog_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
)

func (e *catalogEnv) setColor(productID uuid.UUID, version int64, imageID uuid.UUID, color *string) error {
	return e.app(func(tx pgx.Tx) error {
		return e.writer().SetImageColor(e.t.Context(), tx, e.seller, productID, version, imageID, color, e.actor)
	})
}

// TestSetImageColor (Task 325): đổi màu ảnh đang hiện theo version mẫu; bỏ màu → ảnh chung; màu không thuộc biến thể,
// version cũ, ảnh đã xóa, ảnh của mẫu khác bị từ chối; mỗi lần đổi version +1, audit catalog.image.color.
func TestSetImageColor(t *testing.T) {
	e, p, a, b := withTwoImages(t)
	v := e.version(p.ID)
	if err := e.setColor(p.ID, v, a.ID, ptr(" Đen ")); err != nil {
		t.Fatalf("SetImageColor: %v", err)
	}
	got := e.images(p.ID)
	if got[0].Color == nil || *got[0].Color != "Đen" || got[1].Color != nil {
		t.Fatalf("màu sau khi đặt sai: %+v", got)
	}
	if e.version(p.ID) != v+1 {
		t.Errorf("version mẫu: mong %d, có %d", v+1, e.version(p.ID))
	}
	if err := e.setColor(p.ID, v+1, a.ID, nil); err != nil {
		t.Fatalf("bỏ màu: %v", err)
	}
	if got := e.images(p.ID); got[0].Color != nil {
		t.Errorf("bỏ màu phải thành ảnh chung: %+v", got[0])
	}
	if n := e.count("SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.image.color' AND entity_id = $1",
		p.ID.String()); n != 2 {
		t.Errorf("audit catalog.image.color: mong 2, có %d", n)
	}
	v = e.version(p.ID)
	if err := e.deleteImage(p.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	other := e.otherProduct()
	cur := e.version(p.ID)
	for name, c := range map[string]struct {
		product uuid.UUID
		version int64
		image   uuid.UUID
		color   *string
		want    error
	}{
		"màu không thuộc biến thể": {p.ID, cur, a.ID, ptr("Xanh"), catalog.ErrInvalidInput},
		"màu rỗng":                 {p.ID, cur, a.ID, ptr("  "), catalog.ErrInvalidInput},
		"version cũ":               {p.ID, v, a.ID, ptr("Đỏ"), catalog.ErrVersionConflict},
		"ảnh đã xóa":               {p.ID, cur, b.ID, ptr("Đỏ"), catalog.ErrNotFound},
		"ảnh không có":             {p.ID, cur, uuid.New(), ptr("Đỏ"), catalog.ErrNotFound},
		"ảnh qua mẫu khác":         {other.ID, other.Version, a.ID, ptr("Đỏ"), catalog.ErrNotFound},
	} {
		if err := e.setColor(c.product, c.version, c.image, c.color); !errors.Is(err, c.want) {
			t.Errorf("%s: mong %v, có %v", name, c.want, err)
		}
	}
	if e.version(p.ID) != cur {
		t.Errorf("lần đổi bị từ chối không được tăng version: %d → %d", cur, e.version(p.ID))
	}
}
