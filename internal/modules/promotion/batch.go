package promotion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/promotion/internal/store"
)

// Giới hạn của đợt mã (CHECK của coupon_batches; kế hoạch M2-14: chèn theo lô 1.000).
const (
	maxBatchQuantity = 100000
	batchChunk       = 1000
)

// CouponTemplate là điều khoản chung của mọi mã trong đợt (mỗi mã dùng một lần, không gắn khách).
type CouponTemplate struct {
	Name             string
	DiscountKind     string
	Value            int64
	MaxDiscount      *int64
	MinSubtotal      int64
	Channels         []string
	PerCustomerLimit *int32
	ValidFrom        time.Time
	ValidTo          time.Time
}

// BatchInput là đợt mã mới: Quantity mã theo Template; BoundPhones khác rỗng thì đúng Quantity SĐT khác nhau, mỗi SĐT
// một mã gắn SĐT đó (Q11). Prefix (chữ hoa, số, tối đa 8) khác rỗng → mã "<Prefix>-<8 ký tự>".
type BatchInput struct {
	Name        string
	Prefix      string
	Quantity    int
	Template    CouponTemplate
	BoundPhones []string
}

// Batch là một đợt mã đã tạo.
type Batch struct {
	ID        uuid.UUID
	Name      string
	Prefix    string
	Quantity  int
	Template  CouponTemplate
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

// templateJSON là cột template của coupon_batches: khóa đúng tên cột của coupons (Q-X2).
type templateJSON struct {
	Name             string    `json:"name"`
	DiscountKind     string    `json:"discount_kind"`
	Value            int64     `json:"value"`
	MaxDiscount      *int64    `json:"max_discount"`
	MinSubtotal      int64     `json:"min_subtotal"`
	Channels         []string  `json:"channels"`
	PerCustomerLimit *int32    `json:"per_customer_limit"`
	ValidFrom        time.Time `json:"valid_from"`
	ValidTo          time.Time `json:"valid_to"`
}

// CreateBatch tạo đợt mã in trong tx của bên gọi: ghi đợt rồi chèn mã theo lô 1.000 (ADR-46 nhóm 10: coupon_batches →
// coupons); mã sinh trùng mã có sẵn thì chỉ mã đó được sinh lại, tối đa 5 lần mỗi mã. Đầu vào sai → ErrInvalidInput;
// SĐT sai → customer.ErrInvalidPhone (lỗi không nêu số).
func (w Writer) CreateBatch(ctx context.Context, tx pgx.Tx, a Actor, in BatchInput) (Batch, error) {
	tpl := in.Template
	tpl.Name = strings.TrimSpace(tpl.Name)
	tpl.Channels = channelsOrEmpty(tpl.Channels)
	name := strings.TrimSpace(in.Name)
	one := int32(1)
	if err := validateTerms(Coupon{
		Name: tpl.Name, DiscountKind: tpl.DiscountKind, Value: tpl.Value, MaxDiscount: tpl.MaxDiscount,
		MinSubtotal: tpl.MinSubtotal, Channels: tpl.Channels, MaxUses: &one, PerCustomerLimit: tpl.PerCustomerLimit,
		ValidFrom: tpl.ValidFrom, ValidTo: tpl.ValidTo, Status: StatusActive,
	}); err != nil {
		return Batch{}, err
	}
	switch {
	case !validName(name):
		return Batch{}, fmt.Errorf("tên đợt mã: %w", ErrInvalidInput)
	case !prefixPattern.MatchString(in.Prefix):
		return Batch{}, fmt.Errorf("tiền tố đợt mã: %w", ErrInvalidInput)
	case in.Quantity < 1 || in.Quantity > maxBatchQuantity:
		return Batch{}, fmt.Errorf("số lượng mã: %w", ErrInvalidInput)
	}
	phones, err := batchPhones(in)
	if err != nil {
		return Batch{}, err
	}
	raw, err := json.Marshal(templateJSON(tpl))
	if err != nil {
		return Batch{}, fmt.Errorf("ghi mẫu đợt mã: %w", err)
	}
	var maxDiscount pgtype.Int8
	var perCustomer pgtype.Int4
	if tpl.MaxDiscount != nil {
		maxDiscount = pgtype.Int8{Int64: *tpl.MaxDiscount, Valid: true}
	}
	if tpl.PerCustomerLimit != nil {
		perCustomer = pgtype.Int4{Int32: *tpl.PerCustomerLimit, Valid: true}
	}
	q := store.New(tx)
	row, err := q.InsertBatch(ctx, store.InsertBatchParams{
		Name: name, CodePrefix: in.Prefix, Quantity: int32(in.Quantity), Template: raw,
		CreatedBy: a.UserID,
	})
	if err != nil {
		return Batch{}, fmt.Errorf("ghi đợt mã: %w", err)
	}
	if err := w.insertBatchCoupons(ctx, q, in.Prefix, phones, store.InsertCouponsBulkParams{
		BatchID: row.ID, Name: tpl.Name, DiscountKind: tpl.DiscountKind, Value: tpl.Value,
		MaxDiscount: maxDiscount, MinSubtotal: tpl.MinSubtotal, Channels: tpl.Channels,
		PerCustomerLimit: perCustomer, ValidFrom: tpl.ValidFrom, ValidTo: tpl.ValidTo,
		CreatedBy: a.UserID,
	}); err != nil {
		return Batch{}, err
	}
	b, err := batchFrom(store.GetBatchRow(row))
	if err != nil {
		return Batch{}, err
	}
	return b, w.signal(ctx, tx, a.TenantID, "coupon_batch", b.ID)
}

// batchPhones trả SĐT (dạng +84…) của từng mã trong đợt; đợt không gắn SĐT → Quantity chuỗi rỗng.
func batchPhones(in BatchInput) ([]string, error) {
	if len(in.BoundPhones) == 0 {
		return make([]string, in.Quantity), nil
	}
	if len(in.BoundPhones) != in.Quantity {
		return nil, fmt.Errorf("số SĐT khác số lượng mã: %w", ErrInvalidInput)
	}
	out := make([]string, 0, len(in.BoundPhones))
	seen := make(map[string]bool, len(in.BoundPhones))
	for _, raw := range in.BoundPhones {
		p, err := customer.NormalizePhone(raw)
		if err != nil {
			return nil, fmt.Errorf("SĐT của đợt mã: %w", err)
		}
		if seen[string(p)] {
			return nil, fmt.Errorf("SĐT trùng trong đợt mã: %w", ErrInvalidInput)
		}
		seen[string(p)] = true
		out = append(out, string(p))
	}
	return out, nil
}

// insertBatchCoupons chèn một mã cho mỗi phần tử của phones (rỗng: không gắn SĐT). Mỗi vòng sinh mã cho các phần tử
// chưa có mã rồi chèn theo lô; mã trùng (trong vòng hay với mã có sẵn) để lại cho vòng sau; tối đa codeAttempts vòng.
func (w Writer) insertBatchCoupons(ctx context.Context, q *store.Queries, prefix string, phones []string,
	p store.InsertCouponsBulkParams,
) error {
	pending := phones
	for range codeAttempts {
		if len(pending) == 0 {
			return nil
		}
		var err error
		if pending, err = w.insertRound(ctx, q, prefix, pending, p); err != nil {
			return err
		}
	}
	if len(pending) > 0 {
		return errCodeSpace
	}
	return nil
}

// insertRound sinh một mã cho mỗi phần tử của pending, chèn theo lô batchChunk; trả các phần tử chưa có mã (mã sinh
// trùng trong vòng hay trùng mã có sẵn).
func (w Writer) insertRound(ctx context.Context, q *store.Queries, prefix string, pending []string,
	p store.InsertCouponsBulkParams,
) (retry []string, err error) {
	byCode := make(map[string]string, len(pending)) // mã viết hoa → SĐT (so như citext)
	codes, codePhones := make([]string, 0, len(pending)), make([]string, 0, len(pending))
	for _, phone := range pending {
		code, err := w.generate(prefix)
		if err != nil {
			return nil, err
		}
		key := strings.ToUpper(code)
		if _, dup := byCode[key]; dup {
			retry = append(retry, phone)
			continue
		}
		byCode[key] = phone
		codes, codePhones = append(codes, code), append(codePhones, phone)
	}
	for start := 0; start < len(codes); start += batchChunk {
		end := min(start+batchChunk, len(codes))
		p.Codes, p.Phones = codes[start:end], codePhones[start:end]
		inserted, err := q.InsertCouponsBulk(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("ghi mã của đợt: %w", err)
		}
		for _, c := range inserted {
			delete(byCode, strings.ToUpper(c))
		}
	}
	for _, phone := range byCode { // trùng mã có sẵn: sinh lại vòng sau
		retry = append(retry, phone)
	}
	return retry, nil
}

// GetBatch đọc đợt mã id; không có → ErrNotFound.
func GetBatch(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Batch, error) {
	row, err := store.New(tx).GetBatch(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, ErrNotFound
	}
	if err != nil {
		return Batch{}, fmt.Errorf("đọc đợt mã: %w", err)
	}
	return batchFrom(row)
}

func batchFrom(r store.GetBatchRow) (Batch, error) {
	var t templateJSON
	if err := json.Unmarshal(r.Template, &t); err != nil {
		return Batch{}, fmt.Errorf("đọc mẫu đợt mã: %w", err)
	}
	return Batch{
		ID: r.ID, Name: r.Name, Prefix: r.CodePrefix, Quantity: int(r.Quantity), Template: CouponTemplate(t),
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
	}, nil
}
