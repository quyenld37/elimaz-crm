package customer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// OrderCustomerInput là khách của một đơn đang lên (05 mục 1). CustomerID có → dùng hồ sơ đó; không có → tìm theo
// Phone (chỉ khi PhoneIsCustomers) hoặc tạo khách FullName. Phone là SĐT người nhận nhân viên nhập; nó chỉ thành danh
// tính của khách khi PhoneIsCustomers ("Số này là của khách", DH12). Source là nguồn của danh tính (rỗng → SourceOrder).
type OrderCustomerInput struct {
	CustomerID       *uuid.UUID
	FullName         string
	Phone            string
	PhoneIsCustomers bool
	Source           string
}

// OrderCustomer là kết quả tìm-hoặc-tạo: hồ sơ đơn gắn vào (CustomerID) và gốc của nó; Created khi vừa tạo khách mới;
// Phone là SĐT đã chuẩn hóa (rỗng khi không nhập); PhoneConflictWith là gốc của nhóm khác đang giữ SĐT mà nhân viên đánh
// dấu là của khách (màn hình hiện "Có thể là cùng một khách", DH13) — không bao giờ tự gộp.
type OrderCustomer struct {
	CustomerID, RootID uuid.UUID
	Created            bool
	Phone              logging.Phone
	PhoneConflictWith  *uuid.UUID
}

// FindOrCreateForOrder chọn hoặc tạo khách cho một đơn trong tx của bên gọi (05 mục 1):
//   - Có CustomerID: dùng hồ sơ đó (không có → ErrNotFound). PhoneIsCustomers và SĐT chưa thuộc ai → chèn danh tính
//     (nguồn order); SĐT thuộc cùng nhóm → không làm gì; thuộc nhóm khác → không gắn, trả PhoneConflictWith.
//   - Không CustomerID, PhoneIsCustomers và SĐT đã là danh tính → dùng hồ sơ giữ nó (Created = false).
//   - Còn lại: tạo khách mới (lifecycle customer); chỉ chèn danh tính khi PhoneIsCustomers (DH12). Hai yêu cầu cùng SĐT
//     mới chạy song song: bên thua lùi savepoint (hồ sơ vừa chèn biến mất, chưa ai thấy) và dùng hồ sơ của bên thắng.
//
// Hồ sơ gốc đang lead thì sang customer (đơn đầu tiên).
func (w Writer) FindOrCreateForOrder(ctx context.Context, tx pgx.Tx, in OrderCustomerInput, a Actor, now time.Time,
) (OrderCustomer, error) {
	if err := w.check(); err != nil {
		return OrderCustomer{}, err
	}
	var out OrderCustomer
	if in.Phone != "" {
		p, err := NormalizePhone(in.Phone)
		if err != nil {
			return OrderCustomer{}, err
		}
		out.Phone = p
	}
	source := in.Source
	if source == "" {
		source = SourceOrder
	}
	if !validSource(source) {
		return OrderCustomer{}, ErrInvalidInput
	}
	linkPhone := out.Phone != "" && in.PhoneIsCustomers
	switch {
	case in.CustomerID != nil:
		if err := w.useExisting(ctx, tx, *in.CustomerID, linkPhone, source, a, &out); err != nil {
			return OrderCustomer{}, err
		}
	default:
		if err := w.findOrCreate(ctx, tx, in.FullName, linkPhone, source, a, now, &out); err != nil {
			return OrderCustomer{}, err
		}
	}
	if !out.Created { // khách mới đã tạo thẳng với lifecycle customer
		if err := w.promote(ctx, tx, out.RootID, a, now); err != nil {
			return OrderCustomer{}, err
		}
	}
	return out, nil
}

// useExisting: đơn gắn vào hồ sơ id có sẵn; linkPhone thì thử chèn SĐT làm danh tính của hồ sơ đó.
func (w Writer) useExisting(ctx context.Context, tx pgx.Tx, id uuid.UUID, linkPhone bool, source string, a Actor,
	out *OrderCustomer,
) error {
	root, err := RootOf(ctx, tx, id)
	if err != nil {
		return err
	}
	out.CustomerID, out.RootID = id, root
	if !linkPhone {
		return nil
	}
	// ADR-46 nhóm 5: khóa dòng khách (hồ sơ và gốc, theo mã tăng dần) trước khi chạm danh tính; promote sau đó đổi
	// dòng gốc đã khóa.
	if root, err = lockPair(ctx, tx, id, root); err != nil {
		return err
	}
	out.RootID = root
	ident, tenant, taken, err := insertIdentity(ctx, tx, id, IdentityPhone, "", string(out.Phone), source, a)
	if err != nil {
		return err
	}
	if taken != nil {
		if taken.rootID != root {
			other := taken.rootID
			out.PhoneConflictWith = &other
		}
		return nil
	}
	if err := w.audit(ctx, tx, tenant, a, "customer.identity.add", id, nil,
		map[string]any{"identity_id": ident.ID.String(), "kind": string(ident.Kind)}); err != nil {
		return err
	}
	return w.signal(ctx, tx, tenant, root)
}

// findOrCreate: không có mã khách; tìm theo SĐT (chỉ khi linkPhone) rồi tạo khách mới.
func (w Writer) findOrCreate(ctx context.Context, tx pgx.Tx, fullName string, linkPhone bool, source string, a Actor,
	now time.Time, out *OrderCustomer,
) error {
	if linkPhone {
		o, found, err := identityOwner(ctx, tx, IdentityPhone, "", string(out.Phone))
		if err != nil {
			return err
		}
		if found {
			out.CustomerID, out.RootID = o.customerID, o.rootID
			return nil
		}
	}
	in := CreateInput{FullName: fullName, Source: source}
	if linkPhone {
		in.Phone = string(out.Phone)
	}
	c, taken, err := w.create(ctx, tx, in, string(LifecycleCustomer), a, now)
	if err != nil {
		return err
	}
	if taken != nil { // yêu cầu song song vừa giữ SĐT này: dùng hồ sơ của bên đó
		out.CustomerID, out.RootID = taken.customerID, taken.rootID
		return nil
	}
	out.CustomerID, out.RootID, out.Created = c.ID, c.RootID, true
	return nil
}

// promote đổi hồ sơ gốc từ lead sang customer (tăng version, audit, báo hiệu); đã là customer thì không làm gì.
func (w Writer) promote(ctx context.Context, tx pgx.Tx, rootID uuid.UUID, a Actor, now time.Time) error {
	row, err := store.New(tx).PromoteCustomer(ctx, store.PromoteCustomerParams{Now: now, ID: rootID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("đổi khách sang customer: %w", err)
	}
	if err := w.audit(ctx, tx, row.TenantID, a, "customer.update", rootID,
		map[string]any{"lifecycle": string(LifecycleLead), "version": row.Version - 1},
		map[string]any{"lifecycle": string(LifecycleCustomer), "version": row.Version}); err != nil {
		return err
	}
	return w.signal(ctx, tx, row.TenantID, rootID)
}

// lockPair khóa dòng hồ sơ id và dòng gốc root theo mã tăng dần (ADR-46: cùng bảng thì theo khóa chính tăng dần) và
// trả gốc đọc dưới khóa. Gốc đổi giữa lúc đọc và lúc khóa (gộp, hoàn tác chạy song song) → khóa lại theo gốc mới, tối
// đa 3 lần.
func lockPair(ctx context.Context, tx pgx.Tx, id, root uuid.UUID) (uuid.UUID, error) {
	for range 3 {
		ids := []uuid.UUID{id}
		if root != id {
			ids = append(ids, root)
			slices.SortFunc(ids, func(x, y uuid.UUID) int { return bytes.Compare(x[:], y[:]) })
		}
		var locked uuid.UUID
		for _, r := range ids {
			row, err := lockOwner(ctx, tx, r)
			if err != nil {
				return uuid.Nil, err
			}
			if r == id {
				locked = row.RootID
			}
		}
		if locked == root {
			return root, nil
		}
		root = locked
	}
	return uuid.Nil, errRootChurn
}
