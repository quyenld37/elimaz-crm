package customer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
)

// ChannelIdentity là danh tính của người nhắn trên một kênh (Task 152): Kind là IdentityFBPSID (M4 chỉ Facebook),
// Scope là mã Page, Value là PSID (dữ liệu cá nhân, không vào log). DisplayName là tên dùng khi phải tạo hồ sơ;
// NameIsPlaceholder đánh dấu đó là tên tạm ("Khách Facebook") để tên từ kênh đè sau (SetNameFromChannel).
type ChannelIdentity struct {
	Kind, Scope, Value, DisplayName string
	NameIsPlaceholder               bool
}

// ChannelCustomer là kết quả tìm-hoặc-tạo: CustomerID là hồ sơ giữ danh tính, RootID là hồ sơ đại diện của nó, Created
// khi lần gọi này vừa tạo hồ sơ.
type ChannelCustomer struct {
	CustomerID, RootID uuid.UUID
	Created            bool
}

// errChannelTaken chỉ dùng để lùi savepoint khi danh tính đã có chủ lúc chèn; không ra khỏi gói.
var errChannelTaken = errors.New("customer: danh tính kênh đã có chủ")

// FindOrCreateByIdentity tìm khách theo danh tính kênh, chưa có thì tạo (05 mục 1, CH08) trong tx của bên gọi. Lấy
// khóa tư vấn theo (tenant, Kind, Scope, Value) trước mọi khóa dòng (ADR-46 mục "Khóa tư vấn"): tin nhắn và comment
// đầu tiên của cùng một người chạy song song vẫn ra một hồ sơ. Đã có → hồ sơ giữ danh tính và gốc của nó, Created =
// false (không bao giờ gộp). Chưa có → tạo khách lead như CreateCustomer (audit customer.create, sự kiện
// customer.created, báo hiệu), chèn danh tính nguồn chat (audit customer.identity.add, chỉ loại), name_source =
// placeholder (NameIsPlaceholder) hay channel. Đầu vào sai → ErrInvalidInput.
func (w Writer) FindOrCreateByIdentity(ctx context.Context, tx pgx.Tx, in ChannelIdentity, a Actor, now time.Time,
) (ChannelCustomer, error) {
	if err := w.check(); err != nil {
		return ChannelCustomer{}, err
	}
	in, err := validChannelIdentity(in)
	if err != nil {
		return ChannelCustomer{}, err
	}
	kind := IdentityKind(in.Kind)
	if err := store.New(tx).LockChannelIdentity(ctx, store.LockChannelIdentityParams{
		Kind: in.Kind, Scope: in.Scope, Value: in.Value,
	}); err != nil {
		return ChannelCustomer{}, fmt.Errorf("khóa danh tính kênh: %w", err)
	}
	o, found, err := identityOwner(ctx, tx, kind, in.Scope, in.Value)
	if err != nil {
		return ChannelCustomer{}, err
	}
	if found {
		return ChannelCustomer{CustomerID: o.customerID, RootID: o.rootID}, nil
	}
	var out ChannelCustomer
	var taken *owner
	// Savepoint: danh tính bị chiếm lúc chèn (đường không lấy khóa tư vấn) → lùi hồ sơ vừa tạo, trả chủ.
	err = db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		var spErr error
		out, taken, spErr = w.createFromChannel(ctx, sp, in, a, now)
		if spErr == nil && taken != nil {
			return errChannelTaken
		}
		return spErr
	})
	switch {
	case errors.Is(err, errChannelTaken):
		return ChannelCustomer{CustomerID: taken.customerID, RootID: taken.rootID}, nil
	case err != nil:
		return ChannelCustomer{}, err //nolint:wrapcheck // lỗi của createFromChannel đã bọc; lỗi savepoint do db bọc
	}
	return out, nil
}

func (w Writer) createFromChannel(ctx context.Context, tx pgx.Tx, in ChannelIdentity, a Actor, now time.Time,
) (ChannelCustomer, *owner, error) {
	c, _, err := w.create(ctx, tx, CreateInput{FullName: in.DisplayName, Source: SourceChat}, string(LifecycleLead), a, now)
	if err != nil {
		return ChannelCustomer{}, nil, err
	}
	id, tenantID, taken, err := insertIdentity(ctx, tx, c.ID, IdentityKind(in.Kind), in.Scope, in.Value, SourceChat, a)
	if err != nil || taken != nil {
		return ChannelCustomer{}, taken, err
	}
	source := NameSourceChannel
	if in.NameIsPlaceholder {
		source = NameSourcePlaceholder
	}
	if err := store.New(tx).SetNameSource(ctx, store.SetNameSourceParams{NameSource: source, ID: c.ID}); err != nil {
		return ChannelCustomer{}, nil, fmt.Errorf("ghi nguồn tên: %w", err)
	}
	if err := w.audit(ctx, tx, tenantID, a, "customer.identity.add", c.ID, nil,
		map[string]any{"identity_id": id.ID.String(), "kind": string(id.Kind)}); err != nil {
		return ChannelCustomer{}, nil, err
	}
	return ChannelCustomer{CustomerID: c.ID, RootID: c.RootID, Created: true}, nil, nil
}

// validChannelIdentity: Kind là IdentityFBPSID; Scope, Value 1–200 ký tự không khoảng trắng; DisplayName là tên hợp lệ.
// Trả bản đã bỏ khoảng trắng hai đầu.
func validChannelIdentity(in ChannelIdentity) (ChannelIdentity, error) {
	if IdentityKind(in.Kind) != IdentityFBPSID {
		return ChannelIdentity{}, ErrInvalidInput
	}
	out := in
	var err error
	for _, f := range []*string{&out.Scope, &out.Value} {
		if *f, err = validText(*f, maxIdentityRunes); err != nil || strings.ContainsAny(*f, " \t") {
			return ChannelIdentity{}, ErrInvalidInput
		}
	}
	if out.DisplayName, err = validName(in.DisplayName); err != nil {
		return ChannelIdentity{}, err
	}
	return out, nil
}
