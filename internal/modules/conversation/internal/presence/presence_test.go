package presence_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/presence"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// env: database thử; tenant A có một Page, một hội thoại trên Page đó, một hội thoại trên Page khác;
// tenant B có một hội thoại. An và Bình có conversation.read trên Page; Cường chỉ có trên Page khác.
type env struct {
	t0              time.Time
	bus             *realtimetest.RecordingBus
	t               *testing.T
	staff           *testkit.Staff
	a, b            uuid.UUID
	page, otherPage uuid.UUID
	conv, otherConv uuid.UUID
	convB           uuid.UUID
	an, binh, cuong identity.AuthUser
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &env{ // giờ gốc cố định
		t: t, staff: s, a: s.Tenant(t, "A"), b: s.Tenant(t, "B"),
		bus: &realtimetest.RecordingBus{}, t0: time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC),
	}
	// Page là tài khoản kênh thật: hội thoại có khóa ngoại tới channel.channel_accounts (Task 153).
	e.page = s.ChannelAccount(t, e.a, "facebook", "page-a", "Page A")
	e.otherPage = s.ChannelAccount(t, e.a, "facebook", "page-khac", "Page khác")
	e.conv = e.conversation(e.a, e.page)
	e.otherConv = e.conversation(e.a, e.otherPage)
	e.convB = e.conversation(e.b, s.ChannelAccount(t, e.b, "facebook", "page-b", "Page B"))
	e.an = e.user("an@elimaz.test", "An", e.page)
	e.binh = e.user("binh@elimaz.test", "Bình", e.page)
	e.cuong = e.user("cuong@elimaz.test", "Cường", e.otherPage)
	return e
}

func (e *env) conversation(tenant, page uuid.UUID) uuid.UUID {
	e.t.Helper()
	ctx := e.t.Context()
	var id uuid.UUID
	e.staff.Exec(e.t, tenant, func(tx pgx.Tx) error {
		var customer uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO customer.customers (tenant_id, full_name, name_search)
			VALUES (current_setting('app.tenant_id')::uuid, 'Chị Lan', 'chi lan') RETURNING id`).Scan(&customer); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO conversation.conversations
			(tenant_id, channel_account_id, customer_id, kind, external_thread_ref, last_message_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'inbox', $3, now()) RETURNING id`,
			page, customer, uuid.NewString()).Scan(&id)
	})
	return id
}

// user tạo nhân viên tên name ở tenant A, quyền conversation.read trên page.
func (e *env) user(email, name string, page uuid.UUID) identity.AuthUser {
	e.t.Helper()
	id := e.staff.User(e.t, e.a, email, "active")
	e.staff.Exec(e.t, e.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), "UPDATE identity.users SET display_name = $2 WHERE id = $1", id, name)
		return err //nolint:wrapcheck // test
	})
	return identity.AuthUser{UserID: id, TenantID: e.a, Grants: []identity.Grant{{
		Permission: "conversation.read", Scope: identity.Scope{Type: identity.ScopePage, ID: page.String()},
	}}}
}

func (e *env) tx(tenant uuid.UUID, fn func(pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, fn) //nolint:wrapcheck // test
}

func (e *env) set(u identity.AuthUser, conv uuid.UUID, s presence.State, at time.Duration) error {
	return e.tx(u.TenantID, func(tx pgx.Tx) error {
		return presence.Set(e.t.Context(), tx, e.bus, u, conv, s, e.t0.Add(at))
	})
}

func (e *env) mustSet(u identity.AuthUser, s presence.State, at time.Duration) {
	e.t.Helper()
	if err := e.set(u, e.conv, s, at); err != nil {
		e.t.Fatalf("Set %s: %v", s, err)
	}
}

func (e *env) list(u identity.AuthUser, conv uuid.UUID, at time.Duration) ([]presence.Entry, error) {
	var out []presence.Entry
	err := e.tx(u.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = presence.List(e.t.Context(), tx, u, conv, e.t0.Add(at))
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *env) mustList(u identity.AuthUser, at time.Duration) []presence.Entry {
	e.t.Helper()
	out, err := e.list(u, e.conv, at)
	if err != nil {
		e.t.Fatalf("List: %v", err)
	}
	return out
}

// signals: số báo hiệu conversation.presence đã phát; mọi báo hiệu phải đúng tenant, quyền, phạm vi Page, thực thể.
func (e *env) signals() int {
	e.t.Helper()
	n := 0
	for _, s := range e.bus.Signals() {
		if s.Topic != "conversation.presence" {
			continue
		}
		n++
		want := realtime.Signal{
			TenantID: e.a, Topic: "conversation.presence", Requires: "conversation.read", Scope: access.PageScope(e.page),
			Entity: realtime.Entity{Type: "conversation", ID: e.conv.String()},
		}
		if s.TenantID != want.TenantID || s.Requires != want.Requires || *s.Scope != *want.Scope || s.Entity != want.Entity || s.System {
			e.t.Errorf("báo hiệu %+v, mong %+v", s, want)
		}
	}
	return n
}

func entries(es ...presence.Entry) []presence.Entry { return es }

// [CH10] An viewing → Bình thấy [{An, viewing}], 1 báo hiệu; An gửi lại viewing 5 lần (mỗi 10 s) → không thêm báo
// hiệu, vẫn còn hạn; An typing → 1 báo hiệu, Bình thấy typing; An drafting_order (LV07) → 1 báo hiệu. An không thấy
// chính mình; Bình viewing → An thấy Bình.
func TestSet_SignalsOnlyOnChange(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.mustSet(e.an, presence.Viewing, 0)
	an := func(s presence.State) presence.Entry {
		return presence.Entry{UserID: e.an.UserID, DisplayName: "An", State: s}
	}
	if got := e.mustList(e.binh, time.Second); !slices.Equal(got, entries(an(presence.Viewing))) {
		t.Fatalf("Bình thấy %+v", got)
	}
	if n := e.signals(); n != 1 {
		t.Fatalf("báo hiệu sau viewing đầu: %d, mong 1", n)
	}
	for i := 1; i <= 5; i++ {
		e.mustSet(e.an, presence.Viewing, time.Duration(i)*10*time.Second)
	}
	if n := e.signals(); n != 1 {
		t.Fatalf("viewing lặp lại không được phát thêm: %d", n)
	}
	// Nhịp tim cuối lúc 50 s → còn hạn tới 80 s.
	if got := e.mustList(e.binh, 79*time.Second); !slices.Equal(got, entries(an(presence.Viewing))) {
		t.Fatalf("nhịp tim phải gia hạn 30 s: %+v", got)
	}
	e.mustSet(e.an, presence.Typing, 60*time.Second)
	if n := e.signals(); n != 2 {
		t.Fatalf("typing: %d báo hiệu, mong 2", n)
	}
	if got := e.mustList(e.binh, 61*time.Second); !slices.Equal(got, entries(an(presence.Typing))) {
		t.Fatalf("Bình thấy %+v, mong typing", got)
	}
	e.mustSet(e.an, presence.DraftingOrder, 70*time.Second)
	if n := e.signals(); n != 3 {
		t.Fatalf("drafting_order: %d báo hiệu, mong 3", n)
	}
	if got := e.mustList(e.an, 71*time.Second); len(got) != 0 {
		t.Fatalf("An không thấy chính mình: %+v", got)
	}
	e.mustSet(e.binh, presence.Viewing, 72*time.Second)
	want := presence.Entry{UserID: e.binh.UserID, DisplayName: "Bình", State: presence.Viewing}
	if got := e.mustList(e.an, 73*time.Second); !slices.Equal(got, entries(want)) {
		t.Fatalf("An thấy %+v", got)
	}
}

// Dòng đã hết hạn mà lịch dọn chưa chạy: không còn trong danh sách; nhịp tim cùng trạng thái sau đó là "mới xuất
// hiện" → phát báo hiệu.
func TestSet_ExpiredRowReappears(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.mustSet(e.an, presence.Viewing, 0)
	if got := e.mustList(e.binh, 30*time.Second); len(got) != 0 {
		t.Fatalf("hết hạn đúng 30 s: %+v", got)
	}
	e.mustSet(e.an, presence.Viewing, 40*time.Second)
	if n := e.signals(); n != 2 {
		t.Fatalf("xuất hiện lại phải phát báo hiệu: %d, mong 2", n)
	}
	if got := e.mustList(e.binh, 41*time.Second); len(got) != 1 {
		t.Fatalf("Bình thấy %+v", got)
	}
}

// cleanup chạy việc của lịch conversation.presence_cleanup (handler từ Parts) ở tenant A lúc t0+at.
func (e *env) cleanup(at time.Duration) json.RawMessage {
	e.t.Helper()
	p := presence.Parts(deps.Deps{Bus: e.bus, Now: func() time.Time { return e.t0.Add(at) }})
	if len(p.Jobs) != 1 {
		e.t.Fatalf("Parts.Jobs: %d", len(p.Jobs))
	}
	var out json.RawMessage
	if err := e.tx(e.a, func(tx pgx.Tx) error {
		var err error
		out, err = p.Jobs[0].Handle(e.t.Context(), queue.Job{TenantID: &e.a, Kind: p.Jobs[0].Kind().Name}, tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("dọn: %v", err)
	}
	return out
}

func (e *env) rows(tenant uuid.UUID) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, tenant, "SELECT count(*) FROM conversation.presence")
}

// Không nhịp tim 31 s rồi lịch dọn chạy → Bình thấy [], đúng 1 báo hiệu cho hội thoại bị đổi (dù hai người cùng hết
// hạn); dòng còn hạn (Bình) không bị xóa; chạy dọn lần nữa → không báo hiệu thêm.
func TestCleanup_ExpiredRows(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.mustSet(e.an, presence.Viewing, 0)
	e.mustSet(e.binh, presence.Typing, time.Second)
	e.mustSet(e.binh, presence.Viewing, 20*time.Second)
	base := e.signals()
	if string(e.cleanup(31*time.Second)) != `{"conversations":1}` {
		t.Fatal("dọn lúc 31 s phải đổi 1 hội thoại")
	}
	if n := e.signals(); n != base+1 {
		t.Fatalf("dọn: %d báo hiệu, mong %d", n, base+1)
	}
	if got := e.mustList(e.binh, 31*time.Second); len(got) != 0 {
		t.Fatalf("Bình thấy %+v sau khi An hết hạn", got)
	}
	if got := e.mustList(e.an, 31*time.Second); len(got) != 1 || got[0].UserID != e.binh.UserID {
		t.Fatalf("dòng còn hạn của Bình phải giữ: %+v", got)
	}
	if n := e.rows(e.a); n != 1 {
		t.Fatalf("còn %d dòng, mong 1", n)
	}
	if string(e.cleanup(32*time.Second)) != `{"conversations":0}` || e.signals() != base+1 {
		t.Fatal("dọn lần hai không được phát báo hiệu")
	}
	// Hai người cùng hết hạn trong một hội thoại → một báo hiệu.
	e.mustSet(e.an, presence.Viewing, 40*time.Second)
	base = e.signals()
	if string(e.cleanup(2*time.Minute)) != `{"conversations":1}` || e.signals() != base+1 || e.rows(e.a) != 0 {
		t.Fatalf("hai dòng hết hạn cùng hội thoại: %d báo hiệu, %d dòng", e.signals()-base, e.rows(e.a))
	}
}

// Lịch conversation.presence_cleanup: 60 s, theo tenant, Kind có handler; việc chỉ chạy lại ở kỳ sau.
func TestParts_Schedule(t *testing.T) {
	t.Parallel()
	p := presence.Parts(deps.Deps{})
	if len(p.Schedules) != 1 || len(p.Jobs) != 1 {
		t.Fatalf("Parts: %+v", p)
	}
	s := p.Schedules[0]
	if s.Name != "conversation.presence_cleanup" || s.Every != time.Minute ||
		!s.PerTenant || s.Kind.Name != p.Jobs[0].Kind().Name || s.Kind.Policy.MaxAttempts != 1 {
		t.Fatalf("lịch: %+v", s)
	}
}

// Người không có quyền trên Page của hội thoại, hội thoại không có, hội thoại của tenant khác → ErrNotFound (không lộ
// sự tồn tại), không ghi dòng, không phát báo hiệu. Trạng thái lạ, thiếu bus → lỗi, không ghi.
func TestAccess_NotFound(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for name, c := range map[string]struct {
		u    identity.AuthUser
		conv uuid.UUID
	}{
		"Page khác":         {e.cuong, e.conv},
		"không có":          {e.an, uuid.New()},
		"tenant khác":       {e.an, e.convB},
		"Page khác (của C)": {e.an, e.otherConv},
	} {
		if err := e.set(c.u, c.conv, presence.Viewing, 0); !errors.Is(err, access.ErrNotFound) {
			t.Errorf("%s: Set = %v, mong ErrNotFound", name, err)
		}
		if _, err := e.list(c.u, c.conv, 0); !errors.Is(err, access.ErrNotFound) {
			t.Errorf("%s: List = %v, mong ErrNotFound", name, err)
		}
	}
	if err := e.set(e.an, e.conv, presence.State("away"), 0); err == nil || errors.Is(err, access.ErrNotFound) {
		t.Errorf("trạng thái lạ: %v", err)
	}
	if err := e.tx(e.a, func(tx pgx.Tx) error {
		return presence.Set(t.Context(), tx, nil, e.an, e.conv, presence.Viewing, e.t0)
	}); err == nil {
		t.Error("thiếu bus phải là lỗi (tx lùi), không panic")
	}
	if e.rows(e.a) != 0 || e.signals() != 0 {
		t.Fatalf("bị từ chối mà vẫn ghi: %d dòng, %d báo hiệu", e.rows(e.a), e.signals())
	}
}
