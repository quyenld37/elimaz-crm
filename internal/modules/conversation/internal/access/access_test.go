package access_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/identity"
)

func user(grants ...identity.Grant) identity.AuthUser {
	return identity.AuthUser{UserID: uuid.New(), TenantID: uuid.New(), Grants: grants}
}

func grant(p identity.Permission, typ, id string) identity.Grant {
	return identity.Grant{Permission: p, Scope: identity.Scope{Type: typ, ID: id}}
}

// [Task 151, PQ04 nền] Quyền theo Page: grant trên page:A cho Page A, không cho Page B (ErrNotFound, không lộ sự tồn
// tại); grant all cho mọi Page; quyền khác, grant theo kho, không grant → ErrNotFound; quyền lạ → ErrNotFound.
func TestRequire(t *testing.T) {
	t.Parallel()
	pageA, pageB := uuid.New(), uuid.New()
	read := identity.Permission("conversation.read")
	reply := identity.Permission("conversation.reply")
	for _, c := range []struct {
		name string
		u    identity.AuthUser
		p    identity.Permission
		page uuid.UUID
		ok   bool
	}{
		{"đúng Page", user(grant(read, identity.ScopePage, pageA.String())), read, pageA, true},
		{"Page khác", user(grant(read, identity.ScopePage, pageA.String())), read, pageB, false},
		{"grant all", user(grant(read, identity.ScopeAll, "")), read, pageB, true},
		{"quyền khác trên Page", user(grant(read, identity.ScopePage, pageA.String())), reply, pageA, false},
		{"grant theo kho cùng mã", user(grant(read, identity.ScopeWarehouse, pageA.String())), read, pageA, false},
		{"không grant", user(), read, pageA, false},
		{"quyền lạ", user(grant("conversation.unknown", identity.ScopeAll, "")), "conversation.unknown", pageA, false},
	} {
		err := access.Require(c.u, c.p, c.page)
		switch {
		case c.ok && err != nil:
			t.Errorf("%s: %v, mong được", c.name, err)
		case !c.ok && !errors.Is(err, access.ErrNotFound):
			t.Errorf("%s: %v, mong ErrNotFound", c.name, err)
		}
	}
}

// [Task 151] PageScope dựng phạm vi page:<mã>.
func TestPageScope(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	if s := access.PageScope(id); s == nil || *s != (identity.Scope{Type: identity.ScopePage, ID: id.String()}) {
		t.Fatalf("PageScope: %+v", s)
	}
}

// [Task 151] VisiblePages: grant all → all; grant theo Page → đúng các Page (sắp, không trùng); grant theo kho và mã
// Page hỏng bị bỏ; không grant → không Page nào.
func TestVisiblePages(t *testing.T) {
	t.Parallel()
	read := identity.Permission("conversation.read")
	a, b := uuid.New(), uuid.New()
	want := []uuid.UUID{a, b}
	slices.SortFunc(want, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
	for _, c := range []struct {
		name string
		u    identity.AuthUser
		all  bool
		ids  []uuid.UUID
	}{
		{"all", user(grant(read, identity.ScopePage, a.String()), grant(read, identity.ScopeAll, "")), true, nil},
		{"hai Page", user(grant(read, identity.ScopePage, b.String()), grant(read, identity.ScopePage, a.String()),
			grant(read, identity.ScopePage, b.String())), false, want},
		{"kho và mã hỏng", user(grant(read, identity.ScopeWarehouse, a.String()), grant(read, identity.ScopePage, "x"),
			grant(read, identity.ScopePage, a.String())), false, []uuid.UUID{a}},
		{"quyền khác", user(grant("conversation.reply", identity.ScopePage, a.String())), false, nil},
		{"không grant", user(), false, nil},
	} {
		all, ids := access.VisiblePages(c.u)
		if all != c.all || !slices.Equal(ids, c.ids) {
			t.Errorf("%s: all %v ids %v; mong %v %v", c.name, all, ids, c.all, c.ids)
		}
	}
}
