package reporting

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// customerRow là một hồ sơ gốc của danh sách khách. Phone là danh tính phone xác minh mới nhất (rồi mới tạo nhất) trong
// mọi hồ sơ cùng root_id, dạng +84…; Tags là tên nhãn của mọi hồ sơ cùng root_id, không trùng, theo tên; ReturnRateBP
// là cột return_rate_bp của customer.customers_v (nil khi mẫu số 0).
type customerRow struct {
	ID                          uuid.UUID
	Code, Name                  string
	Phone                       *string
	Orders, Delivered, Returned int64
	ReturnRateBP                *int32
	Spent                       int64
	LastOrderAt                 *time.Time
	Tags                        []string
	CreatedAt                   time.Time
}

// customerKey là vị trí keyset (created_at, id), giảm dần: khách mới tạo trước (chỉ mục customers_roots_created).
type customerKey struct {
	created time.Time
	id      uuid.UUID
}

// customerRows đọc tối đa limit hồ sơ gốc sau after (nil: từ đầu) khớp p, mới tạo trước. Chỉ đọc view đã công bố của
// customer (customers_v, customer_identities_v, customer_tag_links_v, customer_tags_v; RLS theo tenant của tx).
func customerRows(ctx context.Context, tx pgx.Tx, p *CustomerExportParams, after *customerKey, limit int) ([]customerRow, error) {
	created, err := dayRange(p.CreatedFrom, p.CreatedTo)
	if err != nil {
		return nil, err
	}
	last, err := dayRange(p.LastOrderFrom, p.LastOrderTo)
	if err != nil {
		return nil, err
	}
	b := &sqlBuilder{}
	where := "c.id = c.root_id"
	rangeCond := func(column string, r timeRange) {
		if !r.Start.IsZero() {
			where += " AND " + column + " >= " + b.Arg(r.Start)
		}
		if !r.End.IsZero() {
			where += " AND " + column + " < " + b.Arg(r.End)
		}
	}
	rangeCond("c.created_at", created)
	rangeCond("c.last_order_at", last)
	if p.Lifecycle != nil {
		where += " AND c.lifecycle = " + b.Arg(*p.Lifecycle)
	}
	if p.MinOrders != nil {
		where += " AND c.order_count >= " + b.Arg(*p.MinOrders)
	}
	if len(p.TagIDs) > 0 {
		where += ` AND EXISTS (SELECT 1 FROM customer.customers_v m
	JOIN customer.customer_tag_links_v l ON l.customer_id = m.id
	WHERE m.root_id = c.id AND l.tag_id = ANY(` + b.Arg(append([]uuid.UUID{}, p.TagIDs...)) + `::uuid[]))`
	}
	if after != nil {
		where += " AND (c.created_at, c.id) < (" + b.Arg(after.created) + ", " + b.Arg(after.id) + "::uuid)"
	}
	q := `SELECT c.id, c.code, c.full_name,
	(SELECT i.value FROM customer.customers_v m
		JOIN customer.customer_identities_v i ON i.customer_id = m.id AND i.kind = 'phone'
		WHERE m.root_id = c.id
		ORDER BY i.verified_at DESC NULLS LAST, i.created_at DESC, i.id DESC LIMIT 1),
	c.order_count::bigint, c.delivered_count::bigint, c.returned_count::bigint, c.return_rate_bp, c.total_spent,
	c.last_order_at,
	coalesce((SELECT array_agg(DISTINCT t.name ORDER BY t.name) FROM customer.customers_v m
		JOIN customer.customer_tag_links_v l ON l.customer_id = m.id
		JOIN customer.customer_tags_v t ON t.id = l.tag_id
		WHERE m.root_id = c.id), '{}'::text[]),
	c.created_at
FROM customer.customers_v c
WHERE ` + where + `
ORDER BY c.created_at DESC, c.id DESC
LIMIT ` + b.Arg(limit)
	rows, err := tx.Query(ctx, q, b.args...)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách khách: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (customerRow, error) {
		var x customerRow
		err := r.Scan(&x.ID, &x.Code, &x.Name, &x.Phone, &x.Orders, &x.Delivered, &x.Returned, &x.ReturnRateBP, &x.Spent,
			&x.LastOrderAt, &x.Tags, &x.CreatedAt)
		return x, err //nolint:wrapcheck // CollectRows trả lỗi, bọc ngay dưới
	})
	if err != nil {
		return nil, fmt.Errorf("đọc dòng danh sách khách: %w", err)
	}
	return out, nil
}
