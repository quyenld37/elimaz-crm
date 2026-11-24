package customer_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

func (e *writeEnv) addIdentity(tenant, id uuid.UUID, in customer.IdentityInput) (customer.Identity, error) {
	var got customer.Identity
	err := e.try(tenant, func(tx pgx.Tx) error {
		var err error
		got, err = e.w.AddIdentity(e.t.Context(), tx, id, in, e.actor)
		return err //nolint:wrapcheck // test
	})
	return got, err
}

func (e *writeEnv) removeIdentity(tenant, id, identityID uuid.UUID) error {
	return e.try(tenant, func(tx pgx.Tx) error {
		return e.w.RemoveIdentity(e.t.Context(), tx, id, identityID, e.actor)
	})
}

// AddIdentity: SĐT chuẩn hóa về +84…, email chữ thường; đã thuộc hồ sơ khác → *IdentityTakenError (không gộp); loại
// nhân viên không được thêm, giá trị sai, khách không có hay của tenant khác → lỗi tương ứng.
func TestAddIdentity(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a, b := e.pair("Khách A", "Khách B", "0987654321")

	phone, err := e.addIdentity(e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityPhone, Value: "84 912.345.678"})
	if err != nil || phone.Value != "+84912345678" || phone.Kind != customer.IdentityPhone || phone.Source != "staff" ||
		phone.CustomerID != a.ID {
		t.Fatalf("thêm SĐT: %+v %v", phone, err)
	}
	mail, err := e.addIdentity(e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityEmail, Value: " Lan@Example.VN "})
	if err != nil || mail.Value != "lan@example.vn" {
		t.Fatalf("thêm email: %+v %v", mail, err)
	}
	web, err := e.addIdentity(e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityWebAccount, Value: "acc-lan", Source: customer.SourceWeb})
	if err != nil || web.Value != "acc-lan" || web.Source != customer.SourceWeb {
		t.Fatalf("thêm web_account: %+v %v", web, err)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.identity.add' AND entity_id = $1
		AND after::text NOT LIKE '%example%' AND after::text NOT LIKE '%912345678%' AND after::text NOT LIKE '%acc-lan%'`, a.ID.String()); n != 3 {
		t.Errorf("audit thêm danh tính: %d", n)
	}

	_, err = e.addIdentity(e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityPhone, Value: "0987654321"})
	var taken *customer.IdentityTakenError
	if !errors.As(err, &taken) || taken.OwnerRootID != b.RootID {
		t.Fatalf("SĐT của B: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1`, a.ID); n != 3 {
		t.Errorf("A có %d danh tính, muốn 3", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_merges`); n != 0 {
		t.Errorf("không bao giờ tự gộp: %d lần gộp", n)
	}

	for _, c := range []struct {
		name   string
		tenant uuid.UUID
		id     uuid.UUID
		in     customer.IdentityInput
		want   error
	}{
		{"fb_psid", e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityFBPSID, Value: "123"}, customer.ErrInvalidInput},
		{"email sai", e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityEmail, Value: "khong-phai-email"}, customer.ErrInvalidInput},
		{"SĐT sai", e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityPhone, Value: "0123"}, customer.ErrInvalidPhone},
		{"web_account nguồn nhân viên", e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityWebAccount, Value: "acc-1"}, customer.ErrInvalidInput},
		{"web_account có khoảng trắng", e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityWebAccount, Value: "a b", Source: customer.SourceWeb}, customer.ErrInvalidInput},
		{"zalo có khoảng trắng", e.tenant, a.ID, customer.IdentityInput{Kind: customer.IdentityZaloUID, Value: "a b"}, customer.ErrInvalidInput},
		{"khách không có", e.tenant, uuid.New(), customer.IdentityInput{Kind: customer.IdentityZaloUID, Value: "z1"}, customer.ErrNotFound},
		{"tenant khác", e.other, a.ID, customer.IdentityInput{Kind: customer.IdentityZaloUID, Value: "z1"}, customer.ErrNotFound},
	} {
		if _, err := e.addIdentity(c.tenant, c.id, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, err, c.want)
		}
	}
}

// RemoveIdentity: gỡ được danh tính của hồ sơ trong nhóm; fb_psid → ErrInvalidInput; danh tính của nhóm khác, không
// có → ErrNotFound.
func TestRemoveIdentity(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "Khách A", Phone: "0912345678"})
	b := e.create(customer.CreateInput{FullName: "Khách B", Phone: "0987654321"})
	pa, pb := e.get(a.ID).Identities[0], e.get(b.ID).Identities[0]
	var psid uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, scope,
			value, source) VALUES ($1, $2, 'fb_psid', 'page-1', 'psid-1', 'chat') RETURNING id`, e.tenant, a.ID).Scan(&psid)
	})

	if err := e.removeIdentity(e.tenant, a.ID, pb.ID); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("gỡ danh tính của nhóm khác: %v", err)
	}
	if err := e.removeIdentity(e.tenant, a.ID, psid); !errors.Is(err, customer.ErrInvalidInput) {
		t.Errorf("gỡ fb_psid: %v", err)
	}
	if err := e.removeIdentity(e.other, a.ID, pa.ID); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("tenant khác: %v", err)
	}
	if err := e.removeIdentity(e.tenant, a.ID, pa.ID); err != nil {
		t.Fatalf("gỡ SĐT của A: %v", err)
	}
	if err := e.removeIdentity(e.tenant, a.ID, pa.ID); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("gỡ lần hai: %v", err)
	}
	ids := e.get(a.ID).Identities
	if len(ids) != 1 || ids[0].ID != psid {
		t.Errorf("còn lại: %+v", ids)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.identity.remove' AND entity_id = $1
		AND before->>'kind' = 'phone'`, a.ID.String()); n != 1 {
		t.Errorf("audit gỡ: %d", n)
	}
}
