package reporting

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting/internal/store"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// FilterOption là một lựa chọn của bộ lọc: mã và tên hiển thị.
type FilterOption struct {
	ID   uuid.UUID
	Name string
}

// FilterOptions là lựa chọn của bộ lọc báo cáo bán hàng trong phạm vi người xem (GET /api/v1/reports/sales/filters).
type FilterOptions struct {
	Sources, Staff, Pages []FilterOption
	CarrierKeys           []string
}

// Filters đọc lựa chọn của bộ lọc cho phạm vi s (QĐ-230-5): nguồn từng gắn với đơn trong phạm vi trong 366 ngày (người
// toàn shop thêm mọi nguồn đang bật); nhân viên đang làm cộng người từng phụ trách hay tạo đơn trong phạm vi trong 366
// ngày; Page của người bán own trong phạm vi; khóa hãng của tài khoản hãng. Không phạm vi nào (NoSales) → mọi lát rỗng.
func Filters(ctx context.Context, tx pgx.Tx, s DataScope) (FilterOptions, error) {
	out := FilterOptions{Sources: []FilterOption{}, Staff: []FilterOption{}, Pages: []FilterOption{}, CarrierKeys: []string{}}
	if s.NoSales() {
		return out, nil
	}
	q := store.New(tx)
	pages := append([]uuid.UUID{}, s.PageIDs...) // nil → mảng rỗng, không NULL
	sources, err := q.FilterSources(ctx, store.FilterSourcesParams{AllPages: s.AllPages, SellerID: s.SellerID, PageIds: pages})
	if err != nil {
		return FilterOptions{}, fmt.Errorf("đọc nguồn của bộ lọc: %w", err)
	}
	for _, r := range sources {
		out.Sources = append(out.Sources, FilterOption{ID: r.ID, Name: r.Name})
	}
	staff, err := q.FilterStaff(ctx, store.FilterStaffParams{SellerID: s.SellerID, AllPages: s.AllPages, PageIds: pages})
	if err != nil {
		return FilterOptions{}, fmt.Errorf("đọc nhân viên của bộ lọc: %w", err)
	}
	for _, r := range staff {
		out.Staff = append(out.Staff, FilterOption{ID: r.ID, Name: r.DisplayName})
	}
	accounts, err := q.FilterPages(ctx, store.FilterPagesParams{SellerID: s.SellerID, AllPages: s.AllPages, PageIds: pages})
	if err != nil {
		return FilterOptions{}, fmt.Errorf("đọc Page của bộ lọc: %w", err)
	}
	for _, r := range accounts {
		out.Pages = append(out.Pages, FilterOption{ID: r.ID, Name: r.Name})
	}
	keys, err := q.FilterCarrierKeys(ctx, s.SellerID)
	if err != nil {
		return FilterOptions{}, fmt.Errorf("đọc khóa hãng của bộ lọc: %w", err)
	}
	out.CarrierKeys = append(out.CarrierKeys, keys...)
	return out, nil
}

// Filters đọc lựa chọn của bộ lọc cho người gọi u trong transaction chỉ đọc của báo cáo (TxReport, hạn giờ xem tại
// chỗ). Không lấy chỗ của báo cáo: câu nhẹ, và hợp đồng của GET này không có run_in_background.
func (r *Interactive) Filters(ctx context.Context, u identity.AuthUser) (FilterOptions, error) {
	var out FilterOptions
	err := tenancy.WithTenant(ctx, r.d, u.TenantID, func(tx pgx.Tx) error {
		out = FilterOptions{} // RetryTx có thể chạy lại hàm
		seller, err := OwnSeller(ctx, tx)
		if err != nil {
			return err
		}
		f, err := Filters(ctx, tx, ScopeFor(u, seller))
		if err != nil {
			return err
		}
		out = f
		return nil
	}, TxReport(r.o.Timeout, r.o.LockTimeout))
	if err != nil {
		return FilterOptions{}, fmt.Errorf("lựa chọn bộ lọc báo cáo: %w", err)
	}
	return out, nil
}
