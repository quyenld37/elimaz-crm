package customer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
)

// errLockRetry: gốc đổi giữa lúc đọc và lúc khóa; lùi savepoint (nhả khóa vừa lấy) rồi khóa lại.
var errLockRetry = errors.New("customer: gốc đổi khi đang khóa")

// lockedRoot là dòng gốc đã khóa.
type lockedRoot struct {
	tenantID, rootID uuid.UUID
	doNotContact     bool
}

// LockRoot khóa FOR UPDATE dòng customer.customers của hồ sơ đại diện (gốc) của customerID tới hết transaction của bên
// gọi và trả mã gốc (ADR-46 nhóm 5, đầu nhóm). Đường M4 gọi trước khi khóa hội thoại (C11: khách trước hội thoại).
// Gốc đọc lại dưới khóa; gộp hay hoàn tác song song làm gốc đổi → khóa lại (tối đa 3 lần, quá thì lỗi). Không có
// (hay của tenant khác) → ErrNotFound.
func LockRoot(ctx context.Context, tx pgx.Tx, customerID uuid.UUID) (uuid.UUID, error) {
	r, err := lockRoot(ctx, tx, customerID)
	return r.rootID, err
}

func lockRoot(ctx context.Context, tx pgx.Tx, customerID uuid.UUID) (lockedRoot, error) {
	for range mergeAttempts {
		var out lockedRoot
		err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
			var err error
			out, err = lockRootOnce(ctx, sp, customerID)
			return err
		})
		switch {
		case errors.Is(err, errLockRetry):
			continue
		case err != nil:
			return lockedRoot{}, err //nolint:wrapcheck // lỗi của lockRootOnce đã bọc; lỗi savepoint do db bọc
		}
		return out, nil
	}
	return lockedRoot{}, errRootChurn
}

func lockRootOnce(ctx context.Context, tx pgx.Tx, customerID uuid.UUID) (lockedRoot, error) {
	root, err := RootOf(ctx, tx, customerID)
	if err != nil {
		return lockedRoot{}, err
	}
	row, err := store.New(tx).LockCustomerRow(ctx, root)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRoot{}, ErrNotFound
	}
	if err != nil {
		return lockedRoot{}, fmt.Errorf("khóa hồ sơ đại diện: %w", err)
	}
	if row.RootID != root { // gốc vừa bị gộp vào hồ sơ khác
		return lockedRoot{}, errLockRetry
	}
	if customerID != root {
		again, err := RootOf(ctx, tx, customerID)
		if err != nil {
			return lockedRoot{}, err
		}
		if again != root { // hồ sơ con vừa được hoàn tác gộp hay gộp sang gốc khác
			return lockedRoot{}, errLockRetry
		}
	}
	return lockedRoot{tenantID: row.TenantID, rootID: root, doNotContact: row.DoNotContact}, nil
}
