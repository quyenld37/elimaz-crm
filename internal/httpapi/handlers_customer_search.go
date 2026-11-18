package httpapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
)

// ListCustomers (GET /api/v1/customers, customer.read; Task M2-10): q rỗng → trang khách gốc mới nhất (con trỏ); q
// khác rỗng → tìm theo mã, SĐT đủ, đuôi SĐT, tên (tối đa limit nhóm, mặc định 50 của hợp đồng, không trang sau). tagId: chỉ nhóm có
// nhãn đó. Không ghi q vào log.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCustomers(ctx context.Context, params gen.ListCustomersParams) (gen.ListCustomersRes, error) {
	var tag *uuid.UUID
	if params.TagId.Set {
		tag = &params.TagId.Value
	}
	limit := int(params.Limit.Or(0)) // ogen điền mặc định của hợp đồng (50); 0 → mặc định của customer
	var hits []customer.SearchHit
	var next *customer.ListCursor
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		if params.Q.Value != "" {
			if tag != nil {
				hits, err = customer.SearchTagged(ctx, tx, params.Q.Value, *tag, limit)
			} else {
				hits, err = customer.Search(ctx, tx, params.Q.Value, limit)
			}
			return err //nolint:wrapcheck // lỗi của customer đã bọc
		}
		f := customer.ListFilter{TagID: tag, Limit: limit}
		if params.Cursor.Set {
			c, err := customer.ParseListCursor(params.Cursor.Value)
			if err != nil {
				return fmt.Errorf("đọc tham số cursor: %w", err)
			}
			f.After = &c
		}
		hits, next, err = customer.List(ctx, tx, f)
		return err //nolint:wrapcheck // lỗi của customer đã bọc
	})
	if err != nil {
		return nil, fmt.Errorf("tìm khách: %w", err)
	}
	out := &gen.CustomerPage{Items: make([]gen.CustomerListItem, 0, len(hits))}
	for _, hit := range hits {
		item := gen.CustomerListItem{
			ID: hit.RootID, Code: hit.Code, FullName: string(hit.FullName), Phones: make([]string, 0, len(hit.Phones)),
			Stats: statsOut(hit.Stats),
		}
		for _, p := range hit.Phones {
			item.Phones = append(item.Phones, string(p))
		}
		if hit.MatchedBy != "" {
			item.MatchedBy = gen.NewOptCustomerListItemMatchedBy(gen.CustomerListItemMatchedBy(hit.MatchedBy))
		}
		out.Items = append(out.Items, item)
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}
