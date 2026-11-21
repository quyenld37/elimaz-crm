package order_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
)

func (e *orderEnv) sources() []order.Source {
	e.t.Helper()
	var ss []order.Source
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) (err error) {
		ss, err = order.ListSources(context.Background(), tx)
		return err //nolint:wrapcheck // test
	})
	return ss
}

func (e *orderEnv) sourceNamed(name string) order.Source {
	e.t.Helper()
	for _, s := range e.sources() {
		if s.Name == name {
			return s
		}
	}
	e.t.Fatalf("không có nguồn %q", name)
	return order.Source{}
}

func (e *orderEnv) createSource(name string) (order.Source, error) {
	var s order.Source
	err := e.staff.TryExec(context.Background(), e.tenant, func(tx pgx.Tx) (err error) {
		s, err = order.Writer{}.CreateSource(context.Background(), tx, name)
		return err //nolint:wrapcheck // test
	})
	return s, err //nolint:wrapcheck // test
}

func (e *orderEnv) updateSource(id uuid.UUID, version int64, p order.SourcePatch) (order.SourceChange, error) {
	var ch order.SourceChange
	err := e.staff.TryExec(context.Background(), e.tenant, func(tx pgx.Tx) (err error) {
		ch, err = order.Writer{}.UpdateSource(context.Background(), tx, id, version, p)
		return err //nolint:wrapcheck // test
	})
	return ch, err //nolint:wrapcheck // test
}

func (e *orderEnv) activeSource(id uuid.UUID) error {
	return e.staff.TryExec(context.Background(), e.tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		return order.ActiveSource(context.Background(), tx, id)
	})
}

func TestSources_ListDefaults(t *testing.T) {
	t.Parallel()
	e := withDefaults(t)
	ss := e.sources()
	if len(ss) != 10 || ss[0].Key != order.SourceKeyManual || ss[0].Name != "Nhập tay" || ss[9].Key != order.SourceKeyReorder {
		t.Fatalf("nguồn mặc định theo vị trí: %+v", ss)
	}
	for i, s := range ss {
		if !s.Active || s.Position != int32(i+1) || s.Version != 1 {
			t.Errorf("nguồn %d: %+v", i, s)
		}
	}
}

func TestSources_CreateUpdate(t *testing.T) {
	t.Parallel()
	e := withDefaults(t)
	s, err := e.createSource("  Tiktok  ")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Tiktok" || s.Key != "" || !s.Active || s.Position != 11 || s.Version != 1 {
		t.Fatalf("nguồn mới (bỏ khoảng trắng, đứng cuối): %+v", s)
	}
	if _, err := e.createSource("Zalo"); !errors.Is(err, order.ErrTrafficSourceNameTaken) {
		t.Errorf("tên trùng nguồn hệ thống: %v", err)
	}
	if _, err := e.createSource(" Tiktok"); !errors.Is(err, order.ErrTrafficSourceNameTaken) {
		t.Errorf("tên trùng sau khi bỏ khoảng trắng: %v", err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("á", 101)} {
		if _, err := e.createSource(bad); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("tên %q: %v", bad, err)
		}
	}
	ch, err := e.updateSource(s.ID, 1, order.SourcePatch{Name: new("TikTok Shop"), Position: new(int32(0))})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Before.Name != "Tiktok" || ch.After.Name != "TikTok Shop" || ch.After.Position != 0 || ch.After.Version != 2 ||
		!ch.After.Active {
		t.Fatalf("đổi tên, vị trí: %+v", ch)
	}
	if got := e.sources(); got[0].ID != s.ID {
		t.Errorf("vị trí 0 đứng đầu: %+v", got[0])
	}
	if _, err := e.updateSource(s.ID, 1, order.SourcePatch{Active: new(false)}); !errors.Is(err, order.ErrVersionConflict) {
		t.Errorf("version cũ: %v", err)
	}
	if _, err := e.updateSource(s.ID, 2, order.SourcePatch{Name: new("Messenger")}); !errors.Is(err, order.ErrTrafficSourceNameTaken) {
		t.Errorf("đổi sang tên đã có: %v", err)
	}
	if _, err := e.updateSource(s.ID, 2, order.SourcePatch{Position: new(int32(-1))}); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("vị trí âm: %v", err)
	}
	if _, err := e.updateSource(uuid.New(), 1, order.SourcePatch{Active: new(false)}); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("nguồn không có: %v", err)
	}
}

func TestSources_ActiveSource(t *testing.T) {
	t.Parallel()
	e := withDefaults(t)
	zalo := e.sourceNamed("Zalo")
	if err := e.activeSource(zalo.ID); err != nil {
		t.Fatalf("Zalo đang bật: %v", err)
	}
	ch, err := e.updateSource(zalo.ID, zalo.Version, order.SourcePatch{Active: new(false)})
	if err != nil || !ch.Before.Active || ch.After.Active || ch.After.Key != order.SourceKeyZalo {
		t.Fatalf("tắt Zalo: %+v %v", ch, err)
	}
	if err := e.activeSource(zalo.ID); !errors.Is(err, order.ErrTrafficSourceInactive) {
		t.Errorf("Zalo đã tắt: %v", err)
	}
	if err := e.activeSource(uuid.New()); !errors.Is(err, order.ErrTrafficSourceInactive) {
		t.Errorf("nguồn không có: %v", err)
	}
	if len(e.sources()) != 10 {
		t.Errorf("nguồn đã tắt vẫn có trong danh sách")
	}
	// Tenant khác không thấy nguồn của A.
	b := e.staff.Tenant(t, "B")
	if err := e.staff.TryExec(context.Background(), b, func(tx pgx.Tx) error {
		return order.ActiveSource(context.Background(), tx, e.sourceNamed("Website").ID)
	}); !errors.Is(err, order.ErrTrafficSourceInactive) {
		t.Errorf("nguồn của tenant khác: %v", err)
	}
}
