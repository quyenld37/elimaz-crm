// Package customer là hồ sơ khách (spec 05 mục 1): hồ sơ, danh tính, địa chỉ (Writer, Task M2-09), tìm-hoặc-tạo khi
// lên đơn (FindOrCreateForOrder), đọc hồ sơ theo gốc (Get). Hàm M1 Create giữ chữ ký, là lớp bọc (Q30).
//
// Khi nào không dùng: module khác không đọc bảng customer.* (đọc view customer.customers_v); không ghi tên khách vào
// log, audit, sự kiện (dữ liệu cá nhân).
package customer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ErrInvalidInput là lỗi khi họ tên rỗng, quá 120 ký tự hay có ký tự điều khiển. Lớp HTTP đổi sang mã ở internal/httpapi/errors.go.
var ErrInvalidInput = errors.New("customer: dữ liệu khách không hợp lệ")

const maxNameRunes = 120

// Lifecycle là giai đoạn của hồ sơ khách (cột customers.lifecycle, 05 mục 1): lead khi mới nhắn hay được tạo, customer
// từ đơn đầu tiên.
type Lifecycle string

// Giá trị của Lifecycle; khớp CHECK customers_lifecycle_check (migration 0201).
const (
	LifecycleLead     Lifecycle = "lead"
	LifecycleCustomer Lifecycle = "customer"
)

// IdentityKind là loại danh tính của khách (cột customer_identities.kind, 05 mục 1).
type IdentityKind string

// Giá trị của IdentityKind; khớp CHECK của customer_identities.kind (migration 0201). fb_psid có scope là mã Page.
const (
	IdentityPhone      IdentityKind = "phone"
	IdentityFBPSID     IdentityKind = "fb_psid"
	IdentityZaloUID    IdentityKind = "zalo_uid"
	IdentityEmail      IdentityKind = "email"
	IdentityWebAccount IdentityKind = "web_account"
)

// Create tạo khách mới chỉ với họ tên (chữ ký M1, Task M1-30) trong tx của bên gọi (tenancy.WithTenant) và trả mã
// khách. Lớp bọc của Writer.CreateCustomer (Q30): người làm system, giờ lấy SELECT now() trong tx, sổ consumer rỗng
// (sự kiện customer.created vẫn ghi vào platform.domain_events; hôm nay không consumer nào nghe topic này), không báo
// hiệu. fullName bỏ khoảng trắng hai đầu; phải 1–120 ký tự, không ký tự điều khiển (ErrInvalidInput).
func Create(ctx context.Context, tx pgx.Tx, fullName string) (uuid.UUID, error) {
	if _, err := validName(fullName); err != nil {
		return uuid.Nil, err
	}
	now, err := store.New(tx).TxNow(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc giờ của transaction: %w", err)
	}
	w := Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
	c, err := w.CreateCustomer(ctx, tx, CreateInput{FullName: fullName, Source: SourceStaff}, Actor{Type: ActorSystem}, now)
	if err != nil {
		return uuid.Nil, err
	}
	return c.ID, nil
}

// Profile là hồ sơ khách theo gốc: Customer là hồ sơ gốc (kèm chỉ số), MemberIDs mọi hồ sơ cùng gốc (gồm gốc), danh
// tính và địa chỉ (chưa xóa) của cả nhóm, nhãn của cả nhóm (mỗi nhãn một dòng, theo tên).
type Profile struct {
	Customer
	MemberIDs  []uuid.UUID
	Identities []Identity
	Addresses  []Address
	Tags       []TagLink
}

// RootOf trả gốc của hồ sơ id; không có (hay của tenant khác) → ErrNotFound.
func RootOf(ctx context.Context, tx pgx.Tx, id uuid.UUID) (uuid.UUID, error) {
	root, err := store.New(tx).GetRootOf(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc gốc của khách: %w", err)
	}
	return root, nil
}

// GroupMembers trả mọi hồ sơ có root_id = rootID (gồm gốc), theo mã.
func GroupMembers(ctx context.Context, tx pgx.Tx, rootID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := store.New(tx).ListGroupMembers(ctx, rootID)
	if err != nil {
		return nil, fmt.Errorf("đọc nhóm hồ sơ: %w", err)
	}
	return ids, nil
}

// Brief trả hồ sơ gốc của id (id là gốc hay hồ sơ con): tên, mã, chỉ số của gốc; không đọc danh tính, địa chỉ, nhãn
// (dùng cho dòng danh sách của module khác, ví dụ việc gọi lại). Không có (hay của tenant khác) → ErrNotFound.
func Brief(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Customer, error) {
	root, err := RootOf(ctx, tx, id)
	if err != nil {
		return Customer{}, err
	}
	return getOne(ctx, tx, root)
}

// Get trả hồ sơ theo gốc của id (id là gốc hay hồ sơ con): hồ sơ gốc, chỉ số của gốc, mọi hồ sơ con, danh tính và
// địa chỉ của cả nhóm. Không có (hay của tenant khác) → ErrNotFound.
func Get(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Profile, error) {
	root, err := RootOf(ctx, tx, id)
	if err != nil {
		return Profile{}, err
	}
	c, err := getOne(ctx, tx, root)
	if err != nil {
		return Profile{}, err
	}
	members, err := GroupMembers(ctx, tx, root)
	if err != nil {
		return Profile{}, err
	}
	q := store.New(tx)
	idRows, err := q.ListGroupIdentities(ctx, members)
	if err != nil {
		return Profile{}, fmt.Errorf("đọc danh tính của nhóm: %w", err)
	}
	addrRows, err := q.ListGroupAddresses(ctx, members)
	if err != nil {
		return Profile{}, fmt.Errorf("đọc địa chỉ của nhóm: %w", err)
	}
	tags, err := groupTags(ctx, tx, members)
	if err != nil {
		return Profile{}, err
	}
	p := Profile{
		Customer: c, MemberIDs: members, Identities: make([]Identity, 0, len(idRows)),
		Addresses: make([]Address, 0, len(addrRows)), Tags: tags,
	}
	for _, r := range idRows {
		p.Identities = append(p.Identities, identityOf(r.ID, r.CustomerID, r.Kind, r.Scope, r.Value, r.VerifiedAt,
			r.Source, r.CreatedAt))
	}
	for _, r := range addrRows {
		p.Addresses = append(p.Addresses, Address{
			ID: r.ID, CustomerID: r.CustomerID, Name: Name(r.RecipientName), Phone: logging.Phone(r.Phone),
			Line: logging.Address(r.Street), ProvinceCode: r.ProvinceCode, ProvinceName: r.ProvinceName,
			WardCode: r.WardCode, WardName: r.WardName, IsDefault: r.IsDefault, Version: r.Version,
		})
	}
	return p, nil
}

// getOne đọc một hồ sơ theo id (không theo gốc).
func getOne(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Customer, error) {
	r, err := store.New(tx).GetCustomerBase(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Customer{}, ErrNotFound
	}
	if err != nil {
		return Customer{}, fmt.Errorf("đọc khách: %w", err)
	}
	c := Customer{
		ID: r.ID, RootID: r.RootID, Code: r.Code, FullName: Name(r.FullName), Lifecycle: r.Lifecycle,
		Birthday: birthdayOf(r.Birthday), Version: r.Version, CreatedAt: r.CreatedAt,
		Stats: statsOf(r.OrderCount, r.DeliveredCount, r.ReturnedCount, r.TotalSpent, r.LastOrderAt),
	}
	return c, nil
}

// statsOf dựng Stats từ cột chỉ số của hồ sơ gốc; ReturnRateBP tính khi đọc.
func statsOf(orders, delivered, returned int32, spent int64, last pgtype.Timestamptz) Stats {
	s := Stats{
		OrderCount: int(orders), DeliveredCount: int(delivered), ReturnedCount: int(returned), TotalSpent: spent,
		LastOrderAt: timeOf(last),
	}
	if done := s.DeliveredCount + s.ReturnedCount; done > 0 {
		s.ReturnRateBP = s.ReturnedCount * 10000 / done
	}
	return s
}

func isControl(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) }
