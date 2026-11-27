package reporting

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting/internal/store"
)

// Quyền đọc của hai họ báo cáo có phạm vi (Task 225).
const (
	permSalesRead     identity.Permission = "report.sales.read"
	permInventoryRead identity.Permission = "report.inventory.read"
)

// DataScope là phần dữ liệu một người được xem trong báo cáo (vùng cao, kế hoạch M6): người bán own của tenant, Page
// được xem báo cáo bán hàng, kho được xem báo cáo tồn. AllPages thì thấy mọi đơn, kể cả đơn không gắn Page; không
// AllPages thì chỉ đơn của PageIDs. Không grant nào thì không thấy gì (NoSales, NoInventory).
type DataScope struct {
	TenantID, SellerID uuid.UUID
	AllPages           bool
	PageIDs            []uuid.UUID
	AllWarehouses      bool
	WarehouseIDs       []uuid.UUID
}

// ScopeFor dựng phạm vi của u cho người bán sellerID từ u.Grants: report.sales.read phạm vi all → AllPages, phạm vi
// page → PageIDs; report.inventory.read tương tự với warehouse. Grant phạm vi khác loại bị bỏ (G14 sau Task 223: không
// dùng identity.ScopesFor, hàm đó gom mã của mọi loại phạm vi); mã không phải uuid dạng chuẩn bị bỏ, vì identity.Can
// so mã nguyên văn với đích. Một Page (kho) thấy được ⇔ identity.Can đúng với đích đó (test thuộc tính).
func ScopeFor(u identity.AuthUser, sellerID uuid.UUID) DataScope {
	s := DataScope{TenantID: u.TenantID, SellerID: sellerID}
	s.AllPages, s.PageIDs = scopeIDs(u.Grants, permSalesRead, identity.ScopePage)
	s.AllWarehouses, s.WarehouseIDs = scopeIDs(u.Grants, permInventoryRead, identity.ScopeWarehouse)
	return s
}

// scopeIDs đọc phạm vi của quyền p trên loại đích typ: all = có grant phạm vi all (mã rỗng, như identity.Can); ngược
// lại các mã đích hợp lệ, không trùng, theo thứ tự grant.
func scopeIDs(grants []identity.Grant, p identity.Permission, typ string) (all bool, ids []uuid.UUID) {
	for _, g := range grants {
		if g.Permission != p {
			continue
		}
		switch {
		case g.Scope.Type == identity.ScopeAll && g.Scope.ID == "":
			return true, nil
		case g.Scope.Type == typ:
			id, err := uuid.Parse(g.Scope.ID)
			if err == nil && id.String() == g.Scope.ID && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return false, ids
}

// NoSales cho biết người này không xem được đơn nào trong báo cáo bán hàng.
func (s DataScope) NoSales() bool { return !s.AllPages && len(s.PageIDs) == 0 }

// NoInventory cho biết người này không xem được kho nào trong báo cáo tồn.
func (s DataScope) NoInventory() bool { return !s.AllWarehouses && len(s.WarehouseIDs) == 0 }

// OwnSeller đọc người bán của chính shop trong tenant của tx: kind own, active, id nhỏ nhất (cùng luật với
// catalog.DefaultSeller, G3 sau Task 223; reporting không nhập catalog nên đọc view catalog.sellers_v). Không có →
// ErrNoOwnSeller.
func OwnSeller(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	id, err := store.New(tx).OwnSeller(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoOwnSeller
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc người bán own: %w", err)
	}
	return id, nil
}
