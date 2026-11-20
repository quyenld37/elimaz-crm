package realtime_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/testkit"
)

type changedData struct {
	Topic  string          `json:"topic"`
	Entity realtime.Entity `json:"entity"`
	Scope  *identity.Scope `json:"scope"`
}

func decodeChanged(t *testing.T, data []byte) changedData {
	t.Helper()
	var c changedData
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("data của changed không phải JSON: %s: %v", data, err)
	}
	return c
}

// expectNothing đòi không có khung nào khác ": ping" trong d.
func expectNothing(t *testing.T, name string, c *testkit.SSEClient, d time.Duration) {
	t.Helper()
	ev, data, err := nextNonPing(c, d)
	if !errors.Is(err, testkit.ErrSSETimeout) {
		t.Fatalf("%s: không được nhận gì, có %q %s (%v)", name, ev, data, err)
	}
}

func TestStreamResyncPublishRollback(t *testing.T) {
	e := newRTEnv(t)
	a := e.staff.Tenant(t, "A")
	c := e.openSub(a, "lan@elimaz.test")

	e.publish(realtime.Signal{
		TenantID: a, Topic: "t.hello", Requires: "realtime.subscribe",
		Entity: realtime.Entity{Type: "hello", ID: "h1"},
	}, false)
	ev, data, err := nextNonPing(c, time.Second)
	if err != nil || ev != "changed" {
		t.Fatalf("mong changed trong 1 s, có %q %s %v", ev, data, err)
	}
	got := decodeChanged(t, data)
	if got.Topic != "t.hello" || got.Entity != (realtime.Entity{Type: "hello", ID: "h1"}) || got.Scope != nil {
		t.Fatalf("changed sai: %s", data)
	}

	e.publish(realtime.Signal{
		TenantID: a, Topic: "t.rollback", Requires: "realtime.subscribe",
		Entity: realtime.Entity{Type: "hello", ID: "h2"},
	}, true)
	expectNothing(t, "transaction lùi", c, time.Second)
}

func TestStreamTenantIsolation(t *testing.T) {
	e := newRTEnv(t)
	a, b := e.staff.Tenant(t, "A"), e.staff.Tenant(t, "B")
	ca := e.openSub(a, "a@elimaz.test")
	cb := e.openSub(b, "b@elimaz.test")

	e.publish(sig(a, "t.a", "1"), false)
	if ev, data, err := nextNonPing(ca, time.Second); err != nil || ev != "changed" {
		t.Fatalf("tenant A phải nhận: %q %s %v", ev, data, err)
	}
	expectNothing(t, "tenant B nhận báo hiệu của A", cb, 500*time.Millisecond)
}

func TestStreamScopedPermission(t *testing.T) {
	e := newRTEnv(t)
	a := e.staff.Tenant(t, "A")
	sub := []string{"realtime.subscribe"}
	p1 := e.open(e.user(a, "p1@elimaz.test", map[string][]string{"all": sub, "page:p1": {"conversation.read"}}))
	all := e.open(e.user(a, "all@elimaz.test", map[string][]string{"all": {"realtime.subscribe", "conversation.read"}}))
	p2 := e.open(e.user(a, "p2@elimaz.test", map[string][]string{"all": sub, "page:p2": {"conversation.read"}}))
	// Nhiều quyền toàn shop và conversation.read trên trang khác (p3), không có conversation.read trên p2.
	other := e.open(e.user(a, "other@elimaz.test", map[string][]string{
		"all":     {"realtime.subscribe", "audit.read", "platform.jobs.manage"},
		"page:p3": {"conversation.read"},
	}))

	e.publish(convP2(a), false)
	for name, c := range map[string]*testkit.SSEClient{"phạm vi all": all, "đúng page:p2": p2} {
		ev, data, err := nextNonPing(c, time.Second)
		if err != nil || ev != "changed" {
			t.Fatalf("%s phải nhận: %q %s %v", name, ev, data, err)
		}
		got := decodeChanged(t, data)
		if got.Scope == nil || *got.Scope != (identity.Scope{Type: "page", ID: "p2"}) {
			t.Fatalf("%s: scope sai: %s", name, data)
		}
	}
	expectNothing(t, "chỉ có page:p1", p1, 500*time.Millisecond)
	expectNothing(t, "conversation.read chỉ trên p3", other, 300*time.Millisecond)
}

// Có grant trên page:p2 nhưng là quyền khác (không phải conversation.read) → không nhận. Quyền theo đích duy nhất của
// M0 là conversation.read, nên dựng thẳng AuthUser qua hub (không qua database) với grant giả lập quyền khác. Task 315
// lượt 2: audit.read là quyền quản trị (toàn shop), nên báo hiệu cần audit.read cũng không tới grant theo trang này.
func TestHubFiltersByRequiredPermissionNotScopeOnly(t *testing.T) {
	e := newRTEnv(t)
	a := e.staff.Tenant(t, "A")
	u := identity.AuthUser{UserID: uuid.New(), TenantID: a, Grants: []identity.Grant{
		{Permission: "realtime.subscribe", Scope: identity.Scope{Type: identity.ScopeAll}},
		{Permission: "audit.read", Scope: identity.Scope{Type: identity.ScopePage, ID: "p2"}},
	}}
	got := make(chan realtime.Frame, 4)
	cancel := e.hub.Subscribe(u, func(f realtime.Frame) { got <- f })
	defer cancel()
	e.publish(convP2(a), false)
	e.publish(realtime.Signal{TenantID: a, Topic: "t.audit", Requires: "audit.read", Entity: realtime.Entity{Type: "a", ID: "1"}}, false)
	e.publish(realtime.Signal{
		TenantID: a, Topic: "t.marker", Requires: "realtime.subscribe",
		Entity: realtime.Entity{Type: "m", ID: "1"},
	}, false)
	select {
	case f := <-got:
		if f.Event != "changed" || !strings.Contains(string(f.Data), "t.marker") {
			t.Fatalf("khung đầu phải là marker (báo hiệu conversation.read, audit.read bị lọc), có %s %s", f.Event, f.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("không nhận marker")
	}
}

func TestStreamBatchDedupe(t *testing.T) {
	e := newRTEnv(t)
	a := e.staff.Tenant(t, "A")
	g := newFrameGate()
	e.spy.hold(g)
	c := e.openSub(a, "lan@elimaz.test")
	s := sig(a, "t.same", "1")
	// Ba Publish giống hệt nhau trong cùng một transaction: PostgreSQL tự gộp NOTIFY trùng, tới hub một khung.
	ctx := t.Context()
	tx, err := e.staff.App.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := e.hub.Publish(ctx, tx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// Thêm hai transaction riêng: ba khung giống hệt nhau tới luồng; cổng giữ đủ ba rồi thả cùng lúc (một lô), để lô
	// ở luồng SSE phải gộp chúng thành một.
	e.publish(s, false)
	e.publish(s, false)
	held := g.waitHeld(t, 3)
	for i, f := range held {
		if f.Event != realtime.EventChanged || string(f.Data) != string(held[0].Data) {
			t.Fatalf("khung %d hub gửi: %s %s, muốn ba khung changed giống hệt nhau", i, f.Event, f.Data)
		}
	}
	g.release()
	if ev, data, err := nextNonPing(c, hangGuard); err != nil || ev != "changed" {
		t.Fatalf("mong một changed: %q %s %v", ev, data, err)
	}
	expectNothing(t, "khung trùng trong lô", c, 3*testBatch)
}

func TestStreamForbiddenWithoutSubscribe(t *testing.T) {
	e := newRTEnv(t)
	a := e.staff.Tenant(t, "A")
	tok := e.user(a, "nosub@elimaz.test", map[string][]string{"all": {"audit.read"}})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.srv.URL+"/api/v1/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cf-Access-Jwt-Assertion", tok)
	status, _, body := testkit.Do(t, req)
	if status != http.StatusForbidden || !strings.Contains(string(body), `"forbidden"`) {
		t.Fatalf("không có realtime.subscribe phải 403 forbidden, có %d %s", status, body)
	}
}

func TestStreamHeaders(t *testing.T) {
	e := newRTEnv(t)
	a := e.staff.Tenant(t, "A")
	c := e.openSub(a, "lan@elimaz.test")
	// Có nhịp tim khi listening.
	ev, _, err := c.Next(3 * testHeartbeat)
	if err != nil || ev != ":ping" {
		t.Fatalf("mong ': ping' khi listening, có %q %v", ev, err)
	}
}

func TestPublishRejectsBadSignals(t *testing.T) {
	e := newRTEnv(t)
	a := e.staff.Tenant(t, "A")
	ok := sig(a, "t.x", "1")
	big := ok
	big.Entity.ID = strings.Repeat("x", 7001)
	noTenant, noTopic, badPerm, noScope := ok, ok, ok, ok
	noScope.Requires = "conversation.read"
	noTenant.TenantID = uuid.Nil
	noTopic.Topic = ""
	badPerm.Requires = "khong.co"
	for name, tc := range map[string]struct {
		s    realtime.Signal
		want error
	}{
		"JSON quá 7.000 byte":         {big, realtime.ErrSignalTooLarge},
		"thiếu tenant":                {noTenant, realtime.ErrInvalidSignal},
		"thiếu topic":                 {noTopic, realtime.ErrInvalidSignal},
		"quyền lạ":                    {badPerm, realtime.ErrInvalidSignal},
		"quyền theo đích thiếu Scope": {noScope, realtime.ErrInvalidSignal},
	} {
		tx, err := e.staff.App.Pool.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := e.hub.Publish(t.Context(), tx, tc.s); !errors.Is(err, tc.want) {
			t.Errorf("%s: lỗi %v, mong %v", name, err, tc.want)
		}
		_ = tx.Rollback(t.Context())
	}
}
