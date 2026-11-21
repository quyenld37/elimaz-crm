package order_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
)

func (e *orderEnv) pack(to string, ids ...uuid.UUID) ([]order.PackingResult, error) {
	var res []order.PackingResult
	err := e.app(func(tx pgx.Tx) error {
		var err error
		res, err = e.writer().SetPackingStatus(e.t.Context(), tx, e.seller, ids, to, e.actor, e.now())
		return err //nolint:wrapcheck // test
	})
	return res, err
}

func (e *orderEnv) mustPack(to string, ids ...uuid.UUID) map[uuid.UUID]string {
	e.t.Helper()
	res, err := e.pack(to, ids...)
	if err != nil {
		e.t.Fatalf("đóng gói %s: %v", to, err)
	}
	out := make(map[uuid.UUID]string, len(res))
	for _, r := range res {
		out[r.OrderID] = r.Result
	}
	return out
}

// VC04/AT08: packed rồi picking → lần hai already_further, vẫn packed (in lại, chọn lại không lùi); picking → packed
// đi tới; packed lần hai → already_further; đơn new → not_allowed. Mỗi lần đổi: version + 1, order.updated, lịch sử
// confirmed → confirmed lý do packing_<to> với người đóng gói.
func TestSetPackingStatus(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	d1, _, _ := e.d1()
	d2, _, _ := e.d1()
	v := e.variant(100000, 5)
	in := e.m2Input(0, ln(v, 1, 0))
	in.Intent = order.IntentNew
	n := e.mustCreateM2(in).Order

	if r := e.mustPack(order.PackingPicking, d2.ID); r[d2.ID] != order.PackingUpdated {
		t.Fatalf("D2 picking: %v", r)
	}
	r := e.mustPack(order.PackingPacked, d1.ID, d2.ID, n.ID)
	if r[d1.ID] != order.PackingUpdated || r[d2.ID] != order.PackingUpdated || r[n.ID] != order.PackingNotAllowed ||
		len(r) != 3 {
		t.Fatalf("packed: %v", r)
	}
	g1 := e.get(d1.ID)
	if g1.PackingStatus != order.PackingPacked || g1.Version != d1.Version+1 || g1.Status != order.StatusConfirmed {
		t.Errorf("D1 sau packed: %s, version %d", g1.PackingStatus, g1.Version)
	}
	if c := e.events(d1.ID, order.TopicOrderUpdated, g1.Version); c != 1 {
		t.Errorf("order.updated seq %d: %d", g1.Version, c)
	}
	if c := e.history(d1.ID, `from_status = 'confirmed' AND to_status = 'confirmed' AND reason = 'packing_packed'
		AND actor_type = 'staff' AND actor_id = $2`, e.actor.UserID); c != 1 {
		t.Errorf("lịch sử đóng gói D1: %d", c)
	}
	if g := e.get(d2.ID); g.PackingStatus != order.PackingPacked || g.Version != d2.Version+2 {
		t.Errorf("D2: %s, version %d", g.PackingStatus, g.Version)
	}
	if g := e.get(n.ID); g.PackingStatus != order.PackingNone || g.Version != n.Version {
		t.Errorf("đơn new bị đổi: %s, %d", g.PackingStatus, g.Version)
	}
	for _, to := range []string{order.PackingPicking, order.PackingPacked} {
		if r := e.mustPack(to, d1.ID); r[d1.ID] != order.PackingAlreadyFurther {
			t.Errorf("D1 packed → %s: %v; muốn already_further", to, r)
		}
	}
	if g := e.get(d1.ID); g.PackingStatus != order.PackingPacked || g.Version != g1.Version {
		t.Errorf("lùi đổi D1: %s, %d", g.PackingStatus, g.Version)
	}
}

// Đầu vào sai → ErrInvalidInput; đơn không có hay của người bán khác → ErrNotFound, không đơn nào bị đổi; mã trùng
// trong danh sách → một kết quả.
func TestSetPackingStatus_Rejects(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	d1, _, _ := e.d1()
	for _, to := range []string{"", order.PackingNone, "shipped"} {
		if _, err := e.pack(to, d1.ID); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("đích %q: %v; muốn ErrInvalidInput", to, err)
		}
	}
	if _, err := e.pack(order.PackingPacked); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("không đơn: %v; muốn ErrInvalidInput", err)
	}
	if _, err := e.pack(order.PackingPacked, d1.ID, uuid.New()); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("đơn không có: %v; muốn ErrNotFound", err)
	}
	if g := e.get(d1.ID); g.PackingStatus != order.PackingNone {
		t.Errorf("D1 bị đổi khi cả lệnh lỗi: %s", g.PackingStatus)
	}
	if r, err := e.pack(order.PackingPicking, d1.ID, d1.ID); err != nil || len(r) != 1 || r[0].Result != order.PackingUpdated {
		t.Errorf("mã trùng: %v, %v", r, err)
	}
}

// Hai goroutine cùng SetPackingStatus trên [D1, D2] và [D2, D1] (lặp 10 lần) → không deadlock: mọi lần xong không lỗi.
func TestSetPackingStatus_NoDeadlock(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	for range 10 {
		d1, _, _ := e.d1()
		d2, _, _ := e.d1()
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, ids := range [][]uuid.UUID{{d1.ID, d2.ID}, {d2.ID, d1.ID}} {
			to := []string{order.PackingPicking, order.PackingPacked}[i]
			wg.Go(func() { _, errs[i] = e.pack(to, ids...) })
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("goroutine %d: %v", i, err)
			}
		}
	}
}
