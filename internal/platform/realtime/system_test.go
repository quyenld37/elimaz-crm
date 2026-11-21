package realtime_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// sysSink nhận báo hiệu hệ thống qua kênh có đệm (hàm nhận của hub không được chặn).
type sysSink chan realtime.Signal

func (e *rtEnv) subscribeSystem(prefix string) sysSink {
	e.t.Helper()
	ch := make(sysSink, 64)
	cancel := e.hub.SubscribeSystem(prefix, func(s realtime.Signal) {
		select {
		case ch <- s:
		default:
			e.t.Errorf("hàng nhận báo hiệu hệ thống đầy")
		}
	})
	e.t.Cleanup(cancel)
	return ch
}

// next trả báo hiệu kế tiếp (hangGuard chỉ là chốt chống treo).
func (s sysSink) next(t *testing.T) realtime.Signal {
	t.Helper()
	select {
	case sig := <-s:
		return sig
	case <-time.After(hangGuard):
		t.Fatal("không nhận báo hiệu hệ thống")
		return realtime.Signal{}
	}
}

func sysSig(tenant uuid.UUID, topic, id string) realtime.Signal {
	return realtime.Signal{
		TenantID: tenant, Topic: topic, System: true,
		Entity: realtime.Entity{Type: "product", ID: id},
	}
}

// Báo hiệu hệ thống không tới SSE, chỉ tới người đăng ký hệ thống có tiền tố khớp; tx lùi thì không ai nhận. Thứ tự
// NOTIFY được giữ trên một kết nối LISTEN, nên "không nhận" được kiểm bằng báo hiệu đánh dấu phát sau, không bằng giờ.
func TestSystemSignals(t *testing.T) {
	e := newRTEnv(t)
	a := e.staff.Tenant(t, "A")
	c := e.openSub(a, "sys@elimaz.test")
	cacheSub := e.subscribeSystem("cache.")
	flashSub := e.subscribeSystem("flash.")

	e.publish(sysSig(a, "cache.invalidate", "lui"), true) // lùi: không ai nhận
	e.publish(sysSig(a, "cache.invalidate", "p1"), false)
	e.publish(sysSig(a, "flash.changed", "f1"), false)
	e.publish(sig(a, "x.changed", "marker"), false) // báo hiệu thường đánh dấu cuối

	got := cacheSub.next(t)
	if got.Topic != "cache.invalidate" || got.Entity != (realtime.Entity{Type: "product", ID: "p1"}) || !got.System || got.TenantID != a {
		t.Fatalf("người đăng ký cache. nhận %+v", got)
	}
	if got := flashSub.next(t); got.Topic != "flash.changed" || got.Entity.ID != "f1" {
		t.Fatalf("người đăng ký flash. nhận %+v (mong flash.changed f1: cache.* và bản lùi không tới)", got)
	}
	// Nhân viên có realtime.subscribe phạm vi all: khung đầu tiên sau resync là báo hiệu đánh dấu, không phải báo
	// hiệu hệ thống nào.
	ev, data, err := nextNonPing(c, hangGuard)
	if err != nil || ev != "changed" {
		t.Fatalf("SSE: %q %s %v", ev, data, err)
	}
	var d struct {
		Topic  string          `json:"topic"`
		Entity realtime.Entity `json:"entity"`
	}
	if err := json.Unmarshal(data, &d); err != nil || d.Topic != "x.changed" || d.Entity.ID != "marker" {
		t.Fatalf("SSE nhận %s, mong chỉ báo hiệu đánh dấu", data)
	}
	select {
	case s := <-cacheSub:
		t.Fatalf("người đăng ký cache. nhận thêm %+v", s)
	default:
	}
	if _, n := e.hub.Status(); n != 1 {
		t.Fatalf("Status đếm %d client, mong 1 (người đăng ký hệ thống không tính)", n)
	}
}

func TestSystemSignalValidation(t *testing.T) {
	e := newRTEnv(t)
	ctx := t.Context()
	a := e.staff.Tenant(t, "A")
	for _, s := range []realtime.Signal{
		{TenantID: a, Topic: "cache.invalidate", System: true, Requires: "realtime.subscribe"},
		{TenantID: a, Topic: "cache.invalidate", System: true, Scope: &identity.Scope{Type: identity.ScopePage, ID: "p"}},
		{Topic: "cache.invalidate", System: true},
		{TenantID: a, System: true},
	} {
		tx, err := e.staff.App.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.hub.Publish(ctx, tx, s); !errors.Is(err, realtime.ErrInvalidSignal) {
			t.Errorf("Publish %+v: %v, mong ErrInvalidSignal", s, err)
		}
		_ = tx.Rollback(ctx)
	}
}

// Kết nối LISTEN bị cắt → sau khi nối lại, mỗi người đăng ký hệ thống (mọi tiền tố) nhận system.resync.
func TestSystemResyncOnReconnect(t *testing.T) {
	e := newRTEnv(t)
	subs := []sysSink{e.subscribeSystem("cache."), e.subscribeSystem("flash.")}
	if n := e.terminateListener(); n != 1 {
		t.Fatalf("phải cắt đúng 1 kết nối LISTEN, cắt %d", n)
	}
	for i, s := range subs {
		got := s.next(t)
		if got.Topic != realtime.TopicSystemResync || !got.System {
			t.Fatalf("người đăng ký %d nhận %+v, mong system.resync", i, got)
		}
	}
	// Hủy đăng ký: không nhận nữa (kiểm bằng một lần cắt nữa và người đăng ký còn lại).
	cancel := e.hub.SubscribeSystem("", func(realtime.Signal) { t.Error("đã hủy mà vẫn nhận") })
	cancel()
	cancel()
	e.waitListening(true, hangGuard)
	if n := e.terminateListener(); n != 1 {
		t.Fatalf("lần 2: cắt %d kết nối", n)
	}
	if got := subs[0].next(t); got.Topic != realtime.TopicSystemResync {
		t.Fatalf("lần 2 nhận %+v", got)
	}
}
