package customer_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

// psid là danh tính Facebook của người u-9 trên Page page (tên tạm "Khách Facebook").
func psid(page, user string) customer.ChannelIdentity {
	return customer.ChannelIdentity{
		Kind: string(customer.IdentityFBPSID), Scope: page, Value: user, DisplayName: "Khách Facebook",
		NameIsPlaceholder: true,
	}
}

func (e *writeEnv) findOrCreate(tenant uuid.UUID, in customer.ChannelIdentity) (customer.ChannelCustomer, error) {
	var got customer.ChannelCustomer
	err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		got, err = e.w.FindOrCreateByIdentity(e.t.Context(), tx, in, customer.Actor{Type: customer.ActorSystem}, e.now)
		return err //nolint:wrapcheck // test
	})
	return got, err
}

func (e *writeEnv) mustFindOrCreate(in customer.ChannelIdentity) customer.ChannelCustomer {
	e.t.Helper()
	got, err := e.findOrCreate(e.tenant, in)
	if err != nil {
		e.t.Fatalf("tìm-hoặc-tạo: %v", err)
	}
	return got
}

// Lần đầu: 1 khách lead, name_source placeholder, 1 danh tính fb_psid phạm vi Page, nguồn chat, Created; lần hai cùng
// khách, không tạo; Page khác là người khác; NameIsPlaceholder = false → name_source channel; danh tính thuộc hồ sơ
// đã gộp → hồ sơ đó và gốc mới.
func TestFindOrCreateByIdentity(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	first := e.mustFindOrCreate(psid("page1", "u-9"))
	if !first.Created || first.CustomerID == uuid.Nil || first.RootID != first.CustomerID {
		t.Fatalf("lần đầu: %+v", first)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND lifecycle = 'lead'
		AND name_source = 'placeholder' AND full_name = 'Khách Facebook' AND name_search = 'khach facebook'
		AND do_not_contact = false`, first.CustomerID); n != 1 {
		t.Errorf("hồ sơ tạo từ kênh: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1 AND kind = 'fb_psid'
		AND scope = 'page1' AND value = 'u-9' AND source = 'chat'`, first.CustomerID); n != 1 {
		t.Errorf("danh tính: %d", n)
	}
	second := e.mustFindOrCreate(psid("page1", "u-9"))
	if second.Created || second.CustomerID != first.CustomerID || second.RootID != first.CustomerID {
		t.Fatalf("lần hai: %+v, lần đầu %+v", second, first)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'customer.created' AND aggregate_id = $1`,
		first.CustomerID.String()); n != 1 {
		t.Errorf("sự kiện customer.created: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1
		AND action IN ('customer.create', 'customer.identity.add') AND actor_type = 'system'`, first.CustomerID.String()); n != 2 {
		t.Errorf("audit tạo và thêm danh tính: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1 AND row_to_json(audit_logs)::text LIKE '%u-9%'`,
		first.CustomerID.String()); n != 0 {
		t.Errorf("audit chứa PSID: %d", n)
	}

	other := e.mustFindOrCreate(psid("page2", "u-9"))
	if !other.Created || other.CustomerID == first.CustomerID {
		t.Errorf("Page khác cùng mã người dùng phải là hồ sơ khác: %+v", other)
	}
	named := e.mustFindOrCreate(customer.ChannelIdentity{Kind: "fb_psid", Scope: "page1", Value: "u-10", DisplayName: "Lan Anh"})
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND name_source = 'channel'
		AND full_name = 'Lan Anh'`, named.CustomerID); n != 1 {
		t.Errorf("tên thật từ kênh: %d", n)
	}

	// Danh tính thuộc hồ sơ đã gộp vào A → trả hồ sơ giữ danh tính và gốc A.
	a := e.create(customer.CreateInput{FullName: "Khách Gốc"})
	e.mustMerge(a.ID, first.CustomerID)
	merged := e.mustFindOrCreate(psid("page1", "u-9"))
	if merged.Created || merged.CustomerID != first.CustomerID || merged.RootID != a.ID {
		t.Errorf("sau gộp: %+v, mong hồ sơ %s gốc %s", merged, first.CustomerID, a.ID)
	}
}

// Đầu vào sai: loại khác fb_psid, thiếu phạm vi, giá trị rỗng hay có khoảng trắng, tên rỗng → ErrInvalidInput, không
// ghi gì. Tenant B không thấy danh tính của A (tạo khách riêng).
func TestFindOrCreateByIdentityInvalid(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	for name, in := range map[string]customer.ChannelIdentity{
		"loại phone":    {Kind: "phone", Scope: "p", Value: "0912345678", DisplayName: "X"},
		"không phạm vi": {Kind: "fb_psid", Value: "u-1", DisplayName: "X"},
		"giá trị rỗng":  {Kind: "fb_psid", Scope: "p", Value: " ", DisplayName: "X"},
		"có khoảng":     {Kind: "fb_psid", Scope: "p", Value: "u 1", DisplayName: "X"},
		"tên rỗng":      {Kind: "fb_psid", Scope: "p", Value: "u-1", DisplayName: "  "},
	} {
		if _, err := e.findOrCreate(e.tenant, in); !errors.Is(err, customer.ErrInvalidInput) {
			t.Errorf("%s: lỗi %v", name, err)
		}
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 0 {
		t.Errorf("đầu vào sai vẫn tạo %d khách", n)
	}
	a := e.mustFindOrCreate(psid("page1", "u-1"))
	b, err := e.findOrCreate(e.other, psid("page1", "u-1"))
	if err != nil || !b.Created || b.CustomerID == a.CustomerID {
		t.Errorf("tenant B: %+v, %v", b, err)
	}
}

// 20 goroutine cùng PSID mới: đúng 1 khách, 1 danh tính, 1 lần Created, mọi lời gọi cùng CustomerID (05 mục 1: tin
// nhắn và comment đầu tiên của một người xử lý song song).
func TestFindOrCreateByIdentityRace(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	const n = 20
	got := make([]customer.ChannelCustomer, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { got[i], errs[i] = e.findOrCreate(e.tenant, psid("page1", "u-9")) })
	}
	wg.Wait()
	expectOneCreated(t, errs, func(i int) (uuid.UUID, bool) { return got[i].CustomerID, got[i].Created })
	if c := e.count(`SELECT count(*) FROM customer.customers`); c != 1 {
		t.Errorf("số khách: %d", c)
	}
	if c := e.count(`SELECT count(*) FROM customer.customer_identities WHERE kind = 'fb_psid' AND value = 'u-9'`); c != 1 {
		t.Errorf("số danh tính: %d", c)
	}
}

func (e *writeEnv) attach(customerID uuid.UUID, in customer.ChannelIdentity) (bool, error) {
	var got bool
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		got, err = e.w.AttachChannelIdentity(e.t.Context(), tx, customerID, in, customer.Actor{Type: customer.ActorSystem})
		return err //nolint:wrapcheck // test
	})
	return got, err
}

// Task 158: gắn PSID mới cho hồ sơ có sẵn (tên bỏ qua) → 1 danh tính nguồn chat, audit customer.identity.add không chứa
// PSID; gắn lại → attached = false, không thêm; PSID của hồ sơ khác → *IdentityTakenError gốc của chủ, không gộp; đầu
// vào sai → ErrInvalidInput; hồ sơ không có → ErrNotFound.
func TestAttachChannelIdentity(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "Chị Hoa"})
	in := customer.ChannelIdentity{Kind: string(customer.IdentityFBPSID), Scope: "page1", Value: "p-55"}
	if ok, err := e.attach(a.ID, in); err != nil || !ok {
		t.Fatalf("gắn lần đầu: %v, %v", ok, err)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1 AND kind = 'fb_psid'
		AND scope = 'page1' AND value = 'p-55' AND source = 'chat'`, a.ID); n != 1 {
		t.Fatalf("danh tính: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1 AND action = 'customer.identity.add'`,
		a.ID.String()); n != 1 {
		t.Errorf("audit: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1 AND row_to_json(audit_logs)::text LIKE '%p-55%'`,
		a.ID.String()); n != 0 {
		t.Errorf("audit chứa PSID: %d", n)
	}
	if ok, err := e.attach(a.ID, in); err != nil || ok {
		t.Fatalf("gắn lại: %v, %v", ok, err)
	}
	b := e.create(customer.CreateInput{FullName: "Chị Khác"})
	_, err := e.attach(b.ID, in)
	if te, ok := errors.AsType[*customer.IdentityTakenError](err); !ok || te.OwnerRootID != a.ID {
		t.Fatalf("PSID đã có chủ: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_merges`); n != 0 {
		t.Errorf("không được gộp: %d", n)
	}
	for name, bad := range map[string]customer.ChannelIdentity{
		"loại khác":    {Kind: "phone", Scope: "page1", Value: "p-56"},
		"thiếu Page":   {Kind: string(customer.IdentityFBPSID), Value: "p-56"},
		"khoảng trắng": {Kind: string(customer.IdentityFBPSID), Scope: "page1", Value: "p 56"},
	} {
		if _, err := e.attach(a.ID, bad); !errors.Is(err, customer.ErrInvalidInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.attach(uuid.New(), customer.ChannelIdentity{Kind: "fb_psid", Scope: "page1", Value: "p-57"}); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("hồ sơ không có: %v", err)
	}
}
