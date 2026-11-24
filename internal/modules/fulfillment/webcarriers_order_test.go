package fulfillment_test

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

func (e *webEnv) reorder(ids []uuid.UUID, version int) (fulfillment.WebCarrierOrderChange, error) {
	var ch fulfillment.WebCarrierOrderChange
	err := e.app(func(tx pgx.Tx) (err error) {
		ch, err = e.svc.ReorderWebCarriers(e.t.Context(), tx, ids, version, e.actor)
		return err //nolint:wrapcheck // test
	})
	return ch, err
}

func (e *webEnv) stats() []fulfillment.WebCarrierStat {
	e.t.Helper()
	set := e.settings()
	var out []fulfillment.WebCarrierStat
	if err := e.app(func(tx pgx.Tx) (err error) {
		out, err = fulfillment.WebCarrierStats(e.t.Context(), tx, set, time.Now())
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *webEnv) accountVersion(id uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT version FROM fulfillment.carrier_accounts WHERE id = $1`, id)
}

// Task 488: đổi thứ tự cả danh sách một lần ghi — web_sort thành 1, 2…; version cài đặt + 1, người và giờ lưu là người
// đổi; một sự kiện carrier.web_settings_changed; chỉ tài khoản đổi số mới tăng version. Version cũ, tập khác tập đang
// hiện (thiếu, thừa, tài khoản không hiện) → ErrVersionConflict, không đổi gì.
func TestReorderWebCarriers(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	if s := e.settings(); s.UpdatedBy != nil || s.UpdatedAt != nil {
		t.Fatalf("chưa lưu mà có người lưu: %+v", s)
	}
	vBest, vVTP := e.accountVersion(e.accBest), e.accountVersion(e.accVTP)
	ch, err := e.reorder([]uuid.UUID{e.accVTP, e.accBest}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ch.Before, []uuid.UUID{e.accBest, e.accVTP}) || !slices.Equal(ch.After, []uuid.UUID{e.accVTP, e.accBest}) ||
		ch.Settings.Version != 2 {
		t.Fatalf("đổi thứ tự: %+v", ch)
	}
	if got := e.carriers(); len(got) != 2 || got[0].AccountID != e.accVTP || got[1].AccountID != e.accBest {
		t.Fatalf("thứ tự trên web: %+v", got)
	}
	s := e.settings()
	if s.Version != 2 || s.UpdatedBy == nil || *s.UpdatedBy != e.actor.UserID || s.UpdatedAt == nil {
		t.Fatalf("cài đặt sau đổi: %+v", s)
	}
	if e.accountVersion(e.accBest) != vBest+1 || e.accountVersion(e.accVTP) != vVTP+1 {
		t.Fatalf("version tài khoản không tăng")
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = $1 AND seq = 2`,
		fulfillment.TopicWebCarrierSettingsChanged); n != 1 {
		t.Fatalf("sự kiện: %d", n)
	}
	// Gửi lại đúng thứ tự đang có: version cài đặt tăng, tài khoản không đổi số thì không tăng version.
	if _, err := e.reorder([]uuid.UUID{e.accVTP, e.accBest}, 2); err != nil || e.accountVersion(e.accBest) != vBest+1 {
		t.Fatalf("thứ tự không đổi: %v", err)
	}
	for _, c := range []struct {
		name    string
		ids     []uuid.UUID
		version int
	}{
		{"version cũ", []uuid.UUID{e.accBest, e.accVTP}, 2},
		{"thiếu tài khoản", []uuid.UUID{e.accBest}, 3},
		{"thừa tài khoản ẩn", []uuid.UUID{e.accBest, e.accVTP, e.accGHN}, 3},
		{"tài khoản lạ", []uuid.UUID{e.accBest, uuid.New()}, 3},
	} {
		if _, err := e.reorder(c.ids, c.version); !errors.Is(err, fulfillment.ErrVersionConflict) {
			t.Errorf("%s: %v, muốn ErrVersionConflict", c.name, err)
		}
	}
	if s := e.settings(); s.Version != 3 {
		t.Fatalf("lỗi đã đổi cài đặt: %+v", s)
	}
	if got := e.carriers(); got[0].AccountID != e.accVTP {
		t.Fatalf("lỗi đã đổi thứ tự: %+v", got)
	}
}

// Task 488: số vận đơn 30 ngày cho cả tài khoản chưa hiện trên web (ShowOnWeb false), trừ revoked.
func TestWebCarrierStats_IncludesHidden(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	// Mọi tài khoản khác của shop (môi trường gốc) bị thu hồi: không còn trong danh sách.
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET status = 'revoked' WHERE id <> ALL($1)",
		[]uuid.UUID{e.accBest, e.accVTP, e.accGHN})
	got := map[uuid.UUID]bool{}
	for _, s := range e.stats() {
		got[s.AccountID] = s.ShowOnWeb
	}
	want := map[uuid.UUID]bool{e.accBest: true, e.accVTP: true, e.accGHN: false}
	if len(got) != len(want) {
		t.Fatalf("tài khoản: %v, muốn %v", got, want)
	}
	for id, shown := range want {
		if v, ok := got[id]; !ok || v != shown {
			t.Errorf("%s: %v %v, muốn hiện %v", id, v, ok, shown)
		}
	}
}

// Đổi thứ tự song song với tắt hiện một hãng: không deadlock (ADR-46 nhóm 8: tài khoản theo mã tăng dần, rồi cài đặt);
// mỗi lệnh qua hoặc trả ErrVersionConflict (đổi thứ tự tăng version tài khoản, nên lệnh tắt đọc version trước đó cũng
// lệch), không lỗi nào khác.
func TestReorderWebCarriers_ConcurrentHide(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	for range 10 {
		var errReorder, errHide error
		var wg sync.WaitGroup
		set := e.settings()
		ids := []uuid.UUID{e.accVTP, e.accBest}
		if e.carriers()[0].AccountID == e.accVTP {
			ids = []uuid.UUID{e.accBest, e.accVTP}
		}
		wg.Go(func() { _, errReorder = e.reorder(ids, set.Version) })
		wg.Go(func() { errHide = e.hide(e.accBest) })
		wg.Wait()
		if errHide != nil && !errors.Is(errHide, fulfillment.ErrVersionConflict) {
			t.Fatalf("tắt hiện: %v", errHide)
		}
		if errReorder != nil && !errors.Is(errReorder, fulfillment.ErrVersionConflict) {
			t.Fatalf("đổi thứ tự: %v", errReorder)
		}
		e.ownerExec("UPDATE fulfillment.carrier_accounts SET show_on_web = true WHERE id = $1", e.accBest)
	}
}
