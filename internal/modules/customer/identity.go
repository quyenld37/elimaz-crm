package customer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// maxIdentityRunes là độ dài tối đa của giá trị danh tính (khớp maxLength của AddCustomerIdentityRequest.value).
const maxIdentityRunes = 200

// IdentityValue là giá trị của một danh tính (SĐT +84…, email, mã Zalo, PSID; dữ liệu cá nhân); ghi log luôn ra
// "[Redacted]".
type IdentityValue string

// LogValue che giá trị danh tính.
func (IdentityValue) LogValue() slog.Value { return slog.StringValue(redacted) }

// Identity là một danh tính của khách. CustomerID là hồ sơ (gốc hay con) đang giữ nó.
type Identity struct {
	ID, CustomerID uuid.UUID
	Kind           IdentityKind
	Scope          string
	Value          IdentityValue
	VerifiedAt     *time.Time
	Source         string
	CreatedAt      time.Time
}

// IdentityInput là danh tính cần thêm. Nhân viên chỉ thêm IdentityPhone, IdentityEmail, IdentityZaloUID (fb_psid do M4
// quản, web_account do website gửi). Source rỗng → SourceStaff.
type IdentityInput struct {
	Kind   IdentityKind
	Value  string
	Source string
}

// AddIdentity thêm danh tính cho hồ sơ customerID (khóa dòng khách trước). Giá trị chuẩn hóa: SĐT về +84… (sai →
// ErrInvalidPhone), email chữ thường; zalo_uid, web_account không có khoảng trắng; web_account chỉ với nguồn web (đơn
// website, Task 193). Đã thuộc một hồ sơ (kể cả chính nhóm này) → *IdentityTakenError kèm gốc của chủ.
// Không bao giờ gộp. Ghi audit customer.identity.add (chỉ loại), báo hiệu customer.changed.
func (w Writer) AddIdentity(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, in IdentityInput, a Actor) (Identity, error) {
	if err := w.check(); err != nil {
		return Identity{}, err
	}
	value, err := normalizeIdentity(in.Kind, in.Value)
	if err != nil {
		return Identity{}, err
	}
	source := in.Source
	if source == "" {
		source = SourceStaff
	}
	if !validSource(source) || (in.Kind == IdentityWebAccount && source != SourceWeb) {
		return Identity{}, ErrInvalidInput
	}
	cust, err := lockOwner(ctx, tx, customerID)
	if err != nil {
		return Identity{}, err
	}
	id, _, taken, err := insertIdentity(ctx, tx, customerID, in.Kind, "", value, source, a)
	if err != nil {
		return Identity{}, err
	}
	if taken != nil {
		return Identity{}, &IdentityTakenError{OwnerRootID: taken.rootID}
	}
	if err := w.audit(ctx, tx, cust.TenantID, a, "customer.identity.add", customerID, nil,
		map[string]any{"identity_id": id.ID.String(), "kind": string(id.Kind)}); err != nil {
		return Identity{}, err
	}
	if err := w.signal(ctx, tx, cust.TenantID, cust.RootID); err != nil {
		return Identity{}, err
	}
	return id, nil
}

// RemoveIdentity gỡ danh tính identityID khỏi nhóm của customerID (danh tính phải thuộc một hồ sơ cùng gốc; không thì
// ErrNotFound). Không gỡ fb_psid (M4 quản) và web_account (website quản): ErrInvalidInput. Khóa dòng hồ sơ giữ danh
// tính trước khi xóa; ghi audit customer.identity.remove (chỉ loại).
func (w Writer) RemoveIdentity(ctx context.Context, tx pgx.Tx, customerID, identityID uuid.UUID, a Actor) error {
	if err := w.check(); err != nil {
		return err
	}
	q := store.New(tx)
	root, err := RootOf(ctx, tx, customerID)
	if err != nil {
		return err
	}
	cur, err := q.GetIdentity(ctx, identityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("đọc danh tính: %w", err)
	}
	holder, err := lockOwner(ctx, tx, cur.CustomerID)
	if err != nil {
		return err
	}
	if holder.RootID != root {
		return ErrNotFound
	}
	if k := IdentityKind(cur.Kind); k == IdentityFBPSID || k == IdentityWebAccount {
		return ErrInvalidInput
	}
	n, err := q.DeleteIdentity(ctx, identityID)
	if err != nil {
		return fmt.Errorf("gỡ danh tính: %w", err)
	}
	if n == 0 { // đã bị gỡ bởi yêu cầu khác trước khi khóa
		return ErrNotFound
	}
	if err := w.audit(ctx, tx, holder.TenantID, a, "customer.identity.remove", cur.CustomerID,
		map[string]any{"identity_id": identityID.String(), "kind": cur.Kind}, nil); err != nil {
		return err
	}
	return w.signal(ctx, tx, holder.TenantID, holder.RootID)
}

var errIdentityChurn = errors.New("customer: danh tính đổi chủ liên tục")

// insertIdentity chèn danh tính (ON CONFLICT DO NOTHING), trả danh tính mới và tenant của nó. Đã có chủ → taken là chủ
// đó, không lỗi. Bên kia vừa gỡ danh
// tính giữa hai câu → thử lại (tối đa 3 lần).
func insertIdentity(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, kind IdentityKind, scope, value, source string,
	a Actor,
) (Identity, uuid.UUID, *owner, error) {
	q := store.New(tx)
	reversed := ""
	if kind == IdentityPhone {
		reversed = ReverseDigits(logging.Phone(value))
	}
	for range 3 {
		row, err := q.InsertIdentity(ctx, store.InsertIdentityParams{
			CustomerID: customerID, Kind: string(kind), Scope: scope, Value: value, ValueReversed: reversed,
			Source: source, CreatedBy: a.createdBy(),
		})
		if err == nil {
			return identityOf(row.ID, row.CustomerID, row.Kind, row.Scope, row.Value, row.VerifiedAt, row.Source,
				row.CreatedAt), row.TenantID, nil, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Identity{}, uuid.Nil, nil, fmt.Errorf("ghi danh tính: %w", err)
		}
		o, found, err := identityOwner(ctx, tx, kind, scope, value)
		if err != nil {
			return Identity{}, uuid.Nil, nil, err
		}
		if found {
			return Identity{}, uuid.Nil, &o, nil
		}
	}
	return Identity{}, uuid.Nil, nil, errIdentityChurn
}

// identityOwner trả hồ sơ đang giữ (kind, scope, value); found = false khi chưa ai giữ.
func identityOwner(ctx context.Context, tx pgx.Tx, kind IdentityKind, scope, value string) (owner, bool, error) {
	row, err := store.New(tx).GetIdentityOwner(ctx, store.GetIdentityOwnerParams{Kind: string(kind), Scope: scope, Value: value})
	if errors.Is(err, pgx.ErrNoRows) {
		return owner{}, false, nil
	}
	if err != nil {
		return owner{}, false, fmt.Errorf("đọc chủ danh tính: %w", err)
	}
	return owner{customerID: row.CustomerID, rootID: row.RootID}, true, nil
}

// normalizeIdentity kiểm loại nhân viên được thêm và chuẩn hóa giá trị.
func normalizeIdentity(kind IdentityKind, raw string) (string, error) {
	switch kind {
	case IdentityPhone:
		p, err := NormalizePhone(raw)
		return string(p), err
	case IdentityEmail:
		v, err := validText(strings.ToLower(raw), maxIdentityRunes)
		if err != nil {
			return "", err
		}
		if addr, err := mail.ParseAddress(v); err != nil || addr.Address != v {
			return "", ErrInvalidInput
		}
		return v, nil
	case IdentityZaloUID, IdentityWebAccount:
		v, err := validText(raw, maxIdentityRunes)
		if err != nil || strings.ContainsAny(v, " \t") {
			return "", ErrInvalidInput
		}
		return v, nil
	default:
		return "", ErrInvalidInput
	}
}

func identityOf(id, customerID uuid.UUID, kind, scope, value string, verified pgtype.Timestamptz, source string,
	created time.Time,
) Identity {
	out := Identity{
		ID: id, CustomerID: customerID, Kind: IdentityKind(kind), Scope: scope, Value: IdentityValue(value),
		Source: source, CreatedAt: created,
	}
	if verified.Valid {
		v := verified.Time
		out.VerifiedAt = &v
	}
	return out
}
