package assign_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/convtest"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	an      = "an@elimaz.test"
	binh    = "binh@elimaz.test"
	manager = "quanly@elimaz.test"
	outside = "ngoai@elimaz.test"
)

type env struct {
	t      *testing.T
	s      *testkit.Staff
	tenant uuid.UUID
	page   uuid.UUID
	ids    map[string]uuid.UUID
}

// newEnv: Page 1; An, Bình (đọc, trả lời Page 1), quản lý (đọc, giao toàn shop), người ngoài (đọc một Page khác).
func newEnv(t *testing.T) *env {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &env{t: t, s: s, tenant: s.Tenant(t, "A"), ids: map[string]uuid.UUID{}}
	e.page = s.ChannelAccount(t, e.tenant, "fakechat", "page-1", "Page 1")
	e.ids[an] = convtest.Staff(t, s, e.tenant, e.page, an, "conversation.read", "conversation.reply")
	e.ids[binh] = convtest.Staff(t, s, e.tenant, e.page, binh, "conversation.read", "conversation.reply")
	e.ids[manager] = convtest.Staff(t, s, e.tenant, uuid.Nil, manager, "conversation.read", "conversation.assign")
	e.ids[outside] = convtest.Staff(t, s, e.tenant, uuid.New(), outside, "conversation.read", "conversation.assign")
	return e
}

func (e *env) tx(fn func(pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.s.App, e.tenant, fn) //nolint:wrapcheck // test
}

func (e *env) claim(email string, id uuid.UUID) error {
	u := convtest.Load(e.t, e.s, email)
	return e.tx(func(tx pgx.Tx) error { return assign.Writer{}.Claim(e.t.Context(), tx, u, id) })
}

func (e *env) assign(email string, id uuid.UUID, to *uuid.UUID, version int32) error {
	u := convtest.Load(e.t, e.s, email)
	return e.tx(func(tx pgx.Tx) error { return assign.Writer{}.Assign(e.t.Context(), tx, u, id, to, version) })
}

// mine: hội thoại đang mở giao cho email (câu "Của tôi" của Task 151).
func (e *env) mine(email string) []uuid.UUID {
	var out []uuid.UUID
	if err := e.tx(func(tx pgx.Tx) error {
		rows, err := store.New(tx).ListConversationsByAssignee(e.t.Context(), store.ListConversationsByAssigneeParams{
			AssigneeID: e.ids[email], Status: "open", BeforeAt: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), BeforeID: uuid.Max, Lim: 100,
		})
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *env) version(id uuid.UUID) int32 {
	return int32(e.s.CountIn(e.t, e.tenant, `SELECT version FROM conversation.conversations WHERE id = $1`, id)) //nolint:gosec // nhỏ
}

// CH06: An tự nhận → "Của tôi" của An có; quản lý giao cho Bình → của Bình có, của An không; lịch sử 2 dòng (self,
// manager); 2 sự kiện conversation.assigned, seq bằng version sau mỗi lần đổi; 2 audit.
func TestAssign_CH06(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := convtest.Conversation(t, e.s, e.tenant, e.page, "psid-1")
	if err := e.claim(an, c); err != nil {
		t.Fatal(err)
	}
	if got := e.mine(an); len(got) != 1 || got[0] != c {
		t.Fatalf("Của tôi (An) = %v", got)
	}
	v1 := e.version(c)
	if err := e.claim(an, c); err != nil { // nhận lại khi đã là mình: không đổi gì
		t.Fatalf("nhận lại: %v", err)
	}
	b := e.ids[binh]
	if err := e.assign(manager, c, &b, v1); err != nil {
		t.Fatal(err)
	}
	if got := e.mine(binh); len(got) != 1 || got[0] != c {
		t.Fatalf("Của tôi (Bình) = %v", got)
	}
	if got := e.mine(an); len(got) != 0 {
		t.Fatalf("Của tôi (An) sau khi giao = %v", got)
	}
	var hist []assign.Assignment
	u := convtest.Load(t, e.s, manager)
	if err := e.tx(func(tx pgx.Tx) (err error) {
		hist, err = assign.History(t.Context(), tx, u, c)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || hist[0].Reason != assign.ReasonSelf || hist[1].Reason != assign.ReasonManager ||
		hist[0].From != nil || hist[0].To.ID != e.ids[an] || hist[0].To.Name != an ||
		hist[1].From.ID != e.ids[an] || hist[1].To.ID != b || hist[1].Actor.ID != e.ids[manager] {
		t.Fatalf("lịch sử = %+v", hist)
	}
	v2 := e.version(c)
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events WHERE topic = 'conversation.assigned'
		AND aggregate_type = 'conversation' AND aggregate_id = $1 AND seq IN ($2, $3)`, c.String(), v1, v2); n != 2 || v2 != v1+1 {
		t.Fatalf("sự kiện = %d (version %d, %d)", n, v1, v2)
	}
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'conversation.assigned'
		AND entity_id = $1`, c.String()); n != 2 {
		t.Fatalf("audit = %d", n)
	}
}

// Từ chối: người được giao không có quyền trên Page → ErrAssigneeNotAllowed; người giao không có quyền trên Page →
// ErrNotFound; version cũ → ErrVersionConflict; người khác đang nhận → ErrAlreadyAssigned; bỏ giao → unassign.
func TestAssign_Refusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := convtest.Conversation(t, e.s, e.tenant, e.page, "psid-2")
	o := e.ids[outside]
	if err := e.assign(manager, c, &o, 1); !errors.Is(err, assign.ErrAssigneeNotAllowed) {
		t.Fatalf("giao người ngoài Page: %v", err)
	}
	a := e.ids[an]
	if err := e.assign(outside, c, &a, 1); !errors.Is(err, access.ErrNotFound) {
		t.Fatalf("người giao không có quyền trên Page: %v", err)
	}
	if err := e.assign(manager, c, &a, 7); !errors.Is(err, assign.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
	if err := e.assign(manager, c, &a, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.claim(binh, c); !errors.Is(err, assign.ErrAlreadyAssigned) {
		t.Fatalf("Bình nhận khi An đang nhận: %v", err)
	}
	e.s.SetStatus(t, e.tenant, e.ids[binh], "locked")
	b := e.ids[binh]
	if err := e.assign(manager, c, &b, e.version(c)); !errors.Is(err, assign.ErrAssigneeNotAllowed) {
		t.Fatalf("giao người bị khóa: %v", err)
	}
	if err := e.assign(manager, c, nil, e.version(c)); err != nil {
		t.Fatal(err)
	}
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.conversation_assignments
		WHERE conversation_id = $1 AND reason = 'unassign' AND to_user_id IS NULL`, c); n != 1 {
		t.Fatalf("dòng bỏ giao = %d", n)
	}
	replyOnly := "chi-tra-loi@elimaz.test"
	convtest.Staff(t, e.s, e.tenant, e.page, replyOnly, "conversation.reply")
	c2 := convtest.Conversation(t, e.s, e.tenant, e.page, "psid-2b")
	if err := e.claim(replyOnly, c2); !errors.Is(err, assign.ErrAssigneeNotAllowed) {
		t.Fatalf("nhận khi không có conversation.read: %v", err)
	}
	if err := e.claim(an, uuid.New()); !errors.Is(err, access.ErrNotFound) {
		t.Fatalf("hội thoại không có: %v", err)
	}
}

// Hai nhân viên cùng bấm nhận: đúng một người được, người kia ErrAlreadyAssigned.
func TestClaim_Concurrent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := convtest.Conversation(t, e.s, e.tenant, e.page, "psid-3")
	users := []identity.AuthUser{convtest.Load(t, e.s, an), convtest.Load(t, e.s, binh)}
	errs := make([]error, len(users))
	var wg sync.WaitGroup
	for i, u := range users {
		wg.Go(func() {
			errs[i] = e.tx(func(tx pgx.Tx) error { return assign.Writer{}.Claim(t.Context(), tx, u, c) })
		})
	}
	wg.Wait()
	ok, taken := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, assign.ErrAlreadyAssigned):
			taken++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || taken != 1 {
		t.Fatalf("được %d, bị từ chối %d", ok, taken)
	}
}
