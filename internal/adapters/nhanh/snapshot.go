package nhanh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// MaxProducts là hạn cứng số mẫu sản phẩm trong một bản chụp (chủ shop 28/11: "< 50 sản phẩm thôi").
const MaxProducts = 50

// SnapshotVersion là phiên bản khuôn bản chụp JSON.
const SnapshotVersion = 1

const (
	pageSize     = 100
	maxPages     = 5000
	statusSell   = 2  // trạng thái "đang bán" của Nhanh (hiện trên website và cửa hàng)
	gpidParents  = 2  // mẫu cha và sản phẩm đơn (không lấy con)
	parentMarker = -2 // parentId của mẫu cha có biến thể
)

// ErrBadOptions báo tham số chụp sai (hơn MaxProducts mẫu, giới hạn âm); lỗi trước mọi lời gọi.
var ErrBadOptions = errors.New("nhanh: tham số chụp sai")

// ErrStuckPaginator báo con trỏ trang không tiến (hai trang liền cùng next); dừng thay vì lặp mãi.
var ErrStuckPaginator = errors.New("nhanh: con trỏ trang không tiến")

// Options của một lần chụp.
type Options struct {
	// Limit là số mẫu mặc định lấy (mẫu đang bán tạo gần nhất); 0 là MaxProducts. Không quá MaxProducts.
	Limit int
	// ProductIDs, khi có, thay cho lựa chọn mặc định: đúng các mẫu này (không quá MaxProducts).
	ProductIDs []int64
	// Since, khi khác không, chỉ lấy phần đổi từ lúc đó (updatedAtFrom) cho sản phẩm chọn mặc định và khách.
	Since time.Time
	// SkipCustomers bỏ phần khách (chỉ chụp sản phẩm).
	SkipCustomers bool
	// Now cho TakenAt; rỗng là đồng hồ của khách.
	Now func() time.Time
}

// Product là một mẫu cùng biến thể, chi tiết và tồn, giữ nguyên JSON của Nhanh.
type Product struct {
	ID        int64             `json:"id"`
	List      json.RawMessage   `json:"list"`
	Detail    json.RawMessage   `json:"detail,omitempty"`
	Variants  []json.RawMessage `json:"variants"`
	Inventory []json.RawMessage `json:"inventory"`
}

// Gap là một trường API không trả (không kèm giá trị nào của khách): task sau soát tay hoặc bổ sung.
type Gap struct {
	Kind  string `json:"kind"` // product_detail | customer
	ID    int64  `json:"id"`
	Field string `json:"field"`
}

// Snapshot là bản chụp: JSON gốc của Nhanh, không biến đổi.
type Snapshot struct {
	Version   int               `json:"version"`
	TakenAt   time.Time         `json:"taken_at"`
	Since     *time.Time        `json:"since,omitempty"`
	Products  []Product         `json:"products"`
	Customers []json.RawMessage `json:"customers"`
	Gaps      []Gap             `json:"gaps"`
}

type idRow struct {
	ID       json.Number `json:"id"`
	ParentID json.Number `json:"parentId"`
	Mobile   *string     `json:"mobile"`
	Content  *string     `json:"content"`
	Desc     *string     `json:"description"`
	ProdID   json.Number `json:"productId"`
}

func (r idRow) id() (int64, bool) {
	n, err := r.ID.Int64()
	return n, err == nil && r.ID != ""
}

func parseRow(raw json.RawMessage) idRow {
	var r idRow
	_ = json.Unmarshal(raw, &r)
	return r
}

// Collect chụp dữ liệu qua c (chỉ đọc). Kiểm tham số trước, rồi: chọn mẫu (product/list), biến thể của mẫu cha
// (product/list parentId), chi tiết từng mẫu (product/detail), tồn (product/inventory), khách (customer/list).
func Collect(ctx context.Context, c *Client, o Options) (*Snapshot, error) {
	limit := o.Limit
	if limit == 0 {
		limit = MaxProducts
	}
	if limit < 0 || limit > MaxProducts || len(o.ProductIDs) > MaxProducts {
		return nil, fmt.Errorf("tối đa %d mẫu: %w", MaxProducts, ErrBadOptions)
	}
	now := o.Now
	if now == nil {
		now = c.Now
	}
	snap := &Snapshot{Version: SnapshotVersion, TakenAt: now().UTC(), Products: []Product{}, Customers: []json.RawMessage{}, Gaps: []Gap{}}
	if !o.Since.IsZero() {
		s := o.Since.UTC()
		snap.Since = &s
	}
	picked, err := pickProducts(ctx, c, o, limit)
	if err != nil {
		return nil, err
	}
	for _, p := range picked {
		if err := fillProduct(ctx, c, &p, snap); err != nil {
			return nil, err
		}
		snap.Products = append(snap.Products, p)
	}
	if o.SkipCustomers {
		return snap, nil
	}
	filters := map[string]any{}
	if !o.Since.IsZero() {
		filters["updatedAtFrom"] = o.Since.Unix()
	}
	rows, err := pageAll(ctx, c, PathCustomerList, filters, nil, 0)
	if err != nil {
		return nil, err
	}
	for _, raw := range rows {
		r := parseRow(raw)
		id, ok := r.id()
		if !ok {
			snap.Gaps = append(snap.Gaps, Gap{Kind: "customer", Field: "id"})
			continue
		}
		if r.Mobile == nil || *r.Mobile == "" {
			snap.Gaps = append(snap.Gaps, Gap{Kind: "customer", ID: id, Field: "mobile"})
		}
		snap.Customers = append(snap.Customers, raw)
	}
	return snap, nil
}

// pickProducts trả các mẫu được chọn (mẫu cha hay sản phẩm đơn), mới nhất trước khi chọn mặc định.
func pickProducts(ctx context.Context, c *Client, o Options, limit int) ([]Product, error) {
	var rows []json.RawMessage
	var err error
	switch {
	case len(o.ProductIDs) > 0:
		p, cerr := c.Call(ctx, PathProductList, map[string]any{
			"filters": map[string]any{"ids": o.ProductIDs}, "paginator": map[string]any{"size": pageSize},
		})
		if cerr != nil {
			return nil, cerr
		}
		if rows, err = Rows(p.Data); err != nil {
			return nil, err
		}
		order := map[int64]int{}
		for i, id := range o.ProductIDs {
			order[id] = i
		}
		slices.SortStableFunc(rows, func(a, b json.RawMessage) int {
			ia, _ := parseRow(a).id()
			ib, _ := parseRow(b).id()
			return order[ia] - order[ib]
		})
	default:
		filters := map[string]any{"status": []int{statusSell}, "gpid": gpidParents}
		if !o.Since.IsZero() {
			filters["updatedAtFrom"] = o.Since.Unix()
		}
		rows, err = pageAll(ctx, c, PathProductList, filters, map[string]string{"id": "desc"}, limit)
		if err != nil {
			return nil, err
		}
	}
	out := make([]Product, 0, min(len(rows), limit))
	for _, raw := range rows {
		r := parseRow(raw)
		id, ok := r.id()
		if !ok {
			continue
		}
		if pid, err := r.ParentID.Int64(); err == nil && pid > 0 {
			continue // biến thể: đi theo mẫu cha của nó, không đứng riêng
		}
		out = append(out, Product{ID: id, List: raw, Variants: []json.RawMessage{}, Inventory: []json.RawMessage{}})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// fillProduct nạp biến thể, chi tiết, tồn vào p; ghi Gap cho trường thiếu.
func fillProduct(ctx context.Context, c *Client, p *Product, snap *Snapshot) error {
	if pid, err := parseRow(p.List).ParentID.Int64(); err == nil && pid == parentMarker {
		vars, err := pageAll(ctx, c, PathProductList, map[string]any{"parentId": p.ID}, nil, 0)
		if err != nil {
			return err
		}
		p.Variants = append(p.Variants, vars...)
	}
	d, err := c.Call(ctx, PathProductDetail, map[string]any{"filters": map[string]any{"id": p.ID}})
	if err != nil {
		return err
	}
	drows, err := oneRow(d.Data)
	if err != nil {
		return err
	}
	if len(drows) == 0 {
		snap.Gaps = append(snap.Gaps, Gap{Kind: "product_detail", ID: p.ID, Field: "detail"})
	} else {
		p.Detail = drows[0]
		dr := parseRow(drows[0])
		if dr.Content == nil || *dr.Content == "" {
			snap.Gaps = append(snap.Gaps, Gap{Kind: "product_detail", ID: p.ID, Field: "content"})
		}
		if dr.Desc == nil || *dr.Desc == "" {
			snap.Gaps = append(snap.Gaps, Gap{Kind: "product_detail", ID: p.ID, Field: "description"})
		}
	}
	ids := []int64{p.ID}
	for _, v := range p.Variants {
		if id, ok := parseRow(v).id(); ok {
			ids = append(ids, id)
		}
	}
	inv, err := c.Call(ctx, PathProductInventory, map[string]any{
		"filters": map[string]any{"ids": ids}, "paginator": map[string]any{"size": pageSize},
	})
	if err != nil {
		return err
	}
	irows, err := Rows(inv.Data)
	if err != nil {
		return err
	}
	p.Inventory = append(p.Inventory, irows...)
	return nil
}

// pageAll đọc mọi trang của path (hay tới khi đủ max dòng nếu max > 0). Con trỏ không tiến là ErrStuckPaginator.
func pageAll(ctx context.Context, c *Client, path string, filters map[string]any, sort map[string]string, maxRows int) ([]json.RawMessage, error) {
	var out []json.RawMessage
	var next json.RawMessage
	for range maxPages {
		pg := map[string]any{"size": pageSize}
		if sort != nil {
			pg["sort"] = sort
		}
		if next != nil {
			pg["next"] = next
		}
		p, err := c.Call(ctx, path, map[string]any{"filters": filters, "paginator": pg})
		if err != nil {
			return nil, err
		}
		rows, err := Rows(p.Data)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
		if len(p.Next) == 0 || (maxRows > 0 && len(out) >= maxRows) {
			return out, nil
		}
		if bytes.Equal(p.Next, next) {
			return nil, fmt.Errorf("gọi %s: %w", path, ErrStuckPaginator)
		}
		next = p.Next
	}
	return nil, fmt.Errorf("gọi %s: quá %d trang: %w", path, maxPages, ErrStuckPaginator)
}

// oneRow: data của product/detail là một object (một mẫu) hay mảng một phần tử; trả 0 hay 1 dòng.
func oneRow(data json.RawMessage) ([]json.RawMessage, error) {
	t := bytes.TrimSpace(data)
	if len(t) > 0 && t[0] == '{' {
		return []json.RawMessage{t}, nil
	}
	rows, err := Rows(data)
	if len(rows) > 1 {
		rows = rows[:1]
	}
	return rows, err
}
