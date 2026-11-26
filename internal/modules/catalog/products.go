package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/article"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// Giới hạn dữ liệu vào (CHECK tương ứng ở migrations/0114_catalog.sql).
const (
	maxName        = 200
	maxDescription = 1000
	maxOption      = 50 // màu, size
	maxVariants    = 200
)

// codePattern: mã mẫu và SKU sau khi viết hoa (Q5).
var codePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{0,63}$`)

// CreateProduct tạo mẫu (mặc định draft) kèm các biến thể trong tx (tenancy.WithTenant hay httpapi.WriteTx của
// actor.TenantID). Mã mẫu, SKU viết hoa; barcode rỗng → EAN-13 tự sinh. Khối image của bài viết phải trỏ tệp ảnh
// sản phẩm ready của tenant (ErrInvalidArticle). Ghi product.updated (Seq 1), báo hiệu, audit.
// Lỗi: ErrInvalidInput, ErrInvalidPrice, ErrInvalidArticle, ErrCodeTaken, ErrSKUTaken, ErrBarcodeTaken,
// ErrDuplicateOption, ErrNotFound (người bán không có). Lỗi trả về thì tx phải lùi.
func (w Writer) CreateProduct(ctx context.Context, tx pgx.Tx, in ProductInput, actor identity.AuthUser) (Product, error) {
	if err := w.check(); err != nil {
		return Product{}, err
	}
	p, stored, err := newProduct(in)
	if err != nil {
		return Product{}, err
	}
	if err := checkArticleFiles(ctx, tx, p.Article, nil); err != nil {
		return Product{}, err
	}
	vs := make([]VariantInput, 0, len(in.Variants))
	for _, v := range in.Variants {
		nv, err := normalizeVariant(v)
		if err != nil {
			return Product{}, err
		}
		vs = append(vs, nv)
	}
	q := store.New(tx)
	row, err := q.InsertProduct(ctx, store.InsertProductParams{
		ID: in.ID, TenantID: actor.TenantID, SellerID: p.SellerID, Code: p.Code, Name: p.Name, NameSearch: textnorm.Fold(p.Name),
		Description: p.Description, Article: stored, Status: string(p.Status),
	})
	if err != nil {
		return Product{}, fmt.Errorf("ghi mẫu: %w", mapWriteError(err))
	}
	p.ID, p.Version = row.ID, row.Version
	p.Variants = make([]Variant, 0, len(vs))
	for _, v := range vs {
		created, err := w.insertVariant(ctx, tx, actor.TenantID, p.SellerID, p.ID, v)
		if err != nil {
			return Product{}, err
		}
		p.Variants = append(p.Variants, created)
	}
	if err := w.productChanged(ctx, tx, actor.TenantID, p.SellerID, p.ID, p.Version); err != nil {
		return Product{}, err
	}
	if err := w.audit(ctx, tx, actor, "catalog.product.create", "catalog.product", p.ID, nil, productAudit(p)); err != nil {
		return Product{}, err
	}
	return p, nil
}

// UpdateProduct sửa tên, mô tả, bài viết, trạng thái của mẫu id của người bán sellerID khi version còn khớp; version tăng 1. Khóa dòng mẫu
// trước khi so version (hai người cùng version: đúng một người thành công). Lỗi: ErrNotFound, ErrVersionConflict,
// ErrInvalidInput, ErrInvalidArticle (gồm khối image mới trỏ tệp không phải ảnh sản phẩm ready của tenant).
func (w Writer) UpdateProduct(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, version int64, patch ProductPatch,
	actor identity.AuthUser,
) (Product, error) {
	if err := w.check(); err != nil {
		return Product{}, err
	}
	before, err := lockProduct(ctx, tx, sellerID, id, version)
	if err != nil {
		return Product{}, err
	}
	in := ProductInput{
		SellerID: before.SellerID, Code: before.Code, Name: before.Name, Description: before.Description,
		Status: before.Status,
	}
	in.Article, err = json.Marshal(before.Article)
	if err != nil {
		return Product{}, fmt.Errorf("mã hóa bài viết cũ: %w", err)
	}
	if patch.Name != nil {
		in.Name = *patch.Name
	}
	if patch.Description != nil {
		in.Description = *patch.Description
	}
	if patch.Article != nil {
		in.Article = *patch.Article
	}
	if patch.Status != nil {
		in.Status = *patch.Status
	}
	after, stored, err := newProduct(in)
	if err != nil {
		return Product{}, err
	}
	if patch.Article != nil {
		if err := checkArticleFiles(ctx, tx, after.Article, before.Article); err != nil {
			return Product{}, err
		}
	}
	q := store.New(tx)
	after.ID = id
	after.Version, err = q.UpdateProduct(ctx, store.UpdateProductParams{
		SellerID: sellerID, ID: id, Version: version, Name: after.Name, NameSearch: textnorm.Fold(after.Name), Description: after.Description,
		Article: stored, Status: string(after.Status),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrVersionConflict // không xảy ra khi đã giữ khóa dòng; giữ để an toàn
	}
	if err != nil {
		return Product{}, fmt.Errorf("sửa mẫu: %w", err)
	}
	if after.Variants, err = listVariants(ctx, q, after.SellerID, id); err != nil {
		return Product{}, err
	}
	before.Variants = after.Variants
	if err := w.productChanged(ctx, tx, actor.TenantID, after.SellerID, id, after.Version); err != nil {
		return Product{}, err
	}
	if err := w.audit(ctx, tx, actor, "catalog.product.update", "catalog.product", id, productAudit(before), productAudit(after)); err != nil {
		return Product{}, err
	}
	return after, nil
}

// GetProduct trả mẫu id của người bán sellerID kèm biến thể (cả mẫu nháp và ngừng bán); không có, đã xóa, của người
// bán hay tenant khác → ErrNotFound.
func GetProduct(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (Product, error) {
	q := store.New(tx)
	row, err := q.GetProduct(ctx, store.GetProductParams{SellerID: sellerID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, fmt.Errorf("đọc mẫu: %w", err)
	}
	p, err := productFromRow(store.LockProductRow(row))
	if err != nil {
		return Product{}, err
	}
	if p.Variants, err = listVariants(ctx, q, sellerID, id); err != nil {
		return Product{}, err
	}
	return p, nil
}

// lockProduct khóa dòng mẫu id của người bán sellerID (ADR-46 nhóm 4: mẫu trước biến thể) và so version; mẫu của
// người bán khác → ErrNotFound (ADR-29).
func lockProduct(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, version int64) (Product, error) {
	p, err := lockProductAnyVersion(ctx, tx, sellerID, id)
	if err == nil && p.Version != version {
		return Product{}, ErrVersionConflict
	}
	return p, err
}

// lockProductAnyVersion khóa dòng mẫu id của người bán sellerID mà không so version (sửa biến thể chỉ so version
// của biến thể).
func lockProductAnyVersion(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID) (Product, error) {
	row, err := store.New(tx).LockProduct(ctx, store.LockProductParams{SellerID: sellerID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, fmt.Errorf("khóa mẫu: %w", err)
	}
	return productFromRow(row)
}

func productFromRow(row store.LockProductRow) (Product, error) {
	blocks, err := DecodeArticle(row.Article)
	if err != nil {
		return Product{}, fmt.Errorf("đọc bài viết của mẫu %s: %w", row.ID, err)
	}
	return Product{
		ID: row.ID, SellerID: row.SellerID, Code: row.Code, Name: row.Name, Description: row.Description,
		Article: blocks, Status: ProductStatus(row.Status), Version: row.Version,
	}, nil
}

// newProduct kiểm và chuẩn hóa phần mẫu của in (không kể biến thể); trả kèm bài viết dạng lưu chuẩn.
func newProduct(in ProductInput) (Product, []byte, error) {
	if in.SellerID == uuid.Nil || len(in.Variants) > maxVariants {
		return Product{}, nil, ErrInvalidInput
	}
	code, ok := normalizeCode(in.Code)
	name := strings.TrimSpace(in.Name)
	status := in.Status
	if status == "" {
		status = StatusDraft
	}
	switch {
	case !ok, !validLine(name, maxName), !validDescription(in.Description),
		status != StatusDraft && status != StatusActive && status != StatusDiscontinued:
		return Product{}, nil, ErrInvalidInput
	}
	blocks, err := article.Validate(in.Article)
	if err != nil {
		return Product{}, nil, err //nolint:wrapcheck // ErrInvalidArticle
	}
	stored, err := json.Marshal(blocks)
	if err != nil {
		return Product{}, nil, fmt.Errorf("mã hóa bài viết: %w", err)
	}
	return Product{
		SellerID: in.SellerID, Code: code, Name: name, Description: in.Description, Article: blocks,
		Status: status,
	}, stored, nil
}

// normalizeCode viết hoa, bỏ khoảng trắng đầu cuối và kiểm mẫu của mã mẫu, SKU.
func normalizeCode(s string) (string, bool) {
	c := strings.ToUpper(strings.TrimSpace(s))
	return c, codePattern.MatchString(c)
}

// validLine: một dòng chữ 1..limit ký tự, không ký tự điều khiển.
func validLine(s string, limit int) bool {
	return s != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= limit && !strings.ContainsFunc(s, isControl)
}

// validDescription: mô tả ngắn chữ thường tối đa maxDescription ký tự; được xuống dòng, không ký tự điều khiển khác.
func validDescription(s string) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= maxDescription &&
		!strings.ContainsFunc(s, func(r rune) bool { return r != '\n' && isControl(r) })
}

func isControl(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) }
