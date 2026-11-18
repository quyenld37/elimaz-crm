package inventory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/textnorm"
)

// Danh bạ nhà cung cấp tối thiểu (kế hoạch M1, Task M1-29; Q33, Q-X6): tên, SĐT, ghi chú, trạng thái. Chỉ gắn với
// phiếu nhập. Công nợ NCC: P3.

// Trạng thái nhà cung cấp.
const (
	SupplierActive   = "active"
	SupplierArchived = "archived"
)

// Giới hạn dữ liệu vào (CHECK tương ứng ở migrations/0116_stock_documents.sql).
const (
	maxSupplierName = 200
	maxNote         = 1000
)

var phoneRe = regexp.MustCompile(`^\+?[0-9]{8,15}$`)

// Supplier là một nhà cung cấp. Phone là dữ liệu cá nhân: kiểu logging.Phone tự che khi ghi log.
type Supplier struct {
	ID      uuid.UUID
	Name    string
	Phone   logging.Phone
	Note    string
	Status  string
	Version int64
}

// LogValue ghi nhà cung cấp vào log không có SĐT, ghi chú (chỉ mã, tên, trạng thái, version).
func (s Supplier) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", s.ID.String()), slog.String("name", s.Name),
		slog.String("status", s.Status), slog.Int64("version", s.Version))
}

// SupplierInput là dữ liệu tạo nhà cung cấp. Phone rỗng hoặc 8–15 chữ số (có thể có + đầu); dấu cách, chấm, gạch bỏ đi.
type SupplierInput struct {
	Name, Phone, Note string
}

// SupplierPatch là phần sửa của nhà cung cấp; nil là giữ nguyên. Status: active | archived (lưu trữ).
type SupplierPatch struct {
	Name, Phone, Note, Status *string
}

// CreateSupplier tạo nhà cung cấp (đang dùng) trong tx (tenancy.WithTenant). Lỗi: ErrInvalidInput,
// ErrSupplierNameTaken. Lỗi trả về thì tx phải lùi.
func CreateSupplier(ctx context.Context, tx pgx.Tx, in SupplierInput, a Actor) (Supplier, error) {
	if !a.valid() {
		return Supplier{}, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	s, err := normalizeSupplier(Supplier{Name: in.Name, Phone: logging.Phone(in.Phone), Note: in.Note, Status: SupplierActive})
	if err != nil {
		return Supplier{}, err
	}
	row, err := store.New(tx).InsertSupplier(ctx, store.InsertSupplierParams{
		Name: s.Name, NameSearch: textnorm.Fold(s.Name), Phone: string(s.Phone), Note: s.Note,
	})
	if err != nil {
		return Supplier{}, fmt.Errorf("ghi nhà cung cấp: %w", mapSupplierError(err))
	}
	return supplierOf(store.ListSuppliersRow(row)), nil
}

// UpdateSupplier sửa nhà cung cấp id khi version còn khớp (khóa dòng rồi so); version tăng 1. Lưu trữ là Status =
// archived (phiếu cũ vẫn đọc ra tên). Lỗi: ErrNotFound, ErrVersionConflict, ErrInvalidInput, ErrSupplierNameTaken.
func UpdateSupplier(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, p SupplierPatch, a Actor) (Supplier, error) {
	if !a.valid() {
		return Supplier{}, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	q := store.New(tx)
	cur, err := q.LockSupplier(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Supplier{}, ErrNotFound
	}
	if err != nil {
		return Supplier{}, fmt.Errorf("khóa nhà cung cấp: %w", err)
	}
	if cur.Version != version {
		return Supplier{}, ErrVersionConflict
	}
	s := supplierOf(store.ListSuppliersRow(cur))
	if p.Name != nil {
		s.Name = *p.Name
	}
	if p.Phone != nil {
		s.Phone = logging.Phone(*p.Phone)
	}
	if p.Note != nil {
		s.Note = *p.Note
	}
	if p.Status != nil {
		s.Status = *p.Status
	}
	if s, err = normalizeSupplier(s); err != nil {
		return Supplier{}, err
	}
	row, err := q.UpdateSupplier(ctx, store.UpdateSupplierParams{
		Name: s.Name, NameSearch: textnorm.Fold(s.Name), Phone: string(s.Phone), Note: s.Note, Status: s.Status,
		ID: id, Version: version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Supplier{}, ErrVersionConflict // không xảy ra khi đã giữ khóa dòng; giữ để an toàn
	}
	if err != nil {
		return Supplier{}, fmt.Errorf("sửa nhà cung cấp: %w", mapSupplierError(err))
	}
	return supplierOf(store.ListSuppliersRow(row)), nil
}

// ListSuppliers trả tối đa 200 nhà cung cấp theo tên; q (bỏ dấu, không phân biệt hoa thường) lọc theo tên chứa q;
// includeArchived = false bỏ nhà cung cấp đã lưu trữ.
func ListSuppliers(ctx context.Context, tx pgx.Tx, q string, includeArchived bool) ([]Supplier, error) {
	pattern := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(textnorm.Fold(q))
	rows, err := store.New(tx).ListSuppliers(ctx, store.ListSuppliersParams{Q: pattern, IncludeArchived: includeArchived})
	if err != nil {
		return nil, fmt.Errorf("đọc nhà cung cấp: %w", err)
	}
	out := make([]Supplier, 0, len(rows))
	for _, r := range rows {
		out = append(out, supplierOf(r))
	}
	return out, nil
}

// normalizeSupplier cắt khoảng trắng, chuẩn hóa SĐT và kiểm giới hạn.
func normalizeSupplier(s Supplier) (Supplier, error) {
	s.Name = strings.TrimSpace(s.Name)
	s.Note = strings.TrimSpace(s.Note)
	s.Phone = logging.Phone(strings.NewReplacer(" ", "", ".", "", "-", "").Replace(string(s.Phone)))
	switch {
	case s.Name == "" || utf8.RuneCountInString(s.Name) > maxSupplierName:
		return Supplier{}, fmt.Errorf("%w: tên nhà cung cấp", ErrInvalidInput)
	case s.Phone != "" && !phoneRe.MatchString(string(s.Phone)):
		return Supplier{}, fmt.Errorf("%w: SĐT nhà cung cấp", ErrInvalidInput)
	case utf8.RuneCountInString(s.Note) > maxNote:
		return Supplier{}, fmt.Errorf("%w: ghi chú nhà cung cấp", ErrInvalidInput)
	case s.Status != SupplierActive && s.Status != SupplierArchived:
		return Supplier{}, fmt.Errorf("%w: trạng thái nhà cung cấp", ErrInvalidInput)
	}
	return s, nil
}

func mapSupplierError(err error) error {
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == "23505" && pe.ConstraintName == "suppliers_name_key" {
		return ErrSupplierNameTaken
	}
	return err
}

// supplierOf đổi một dòng của store (các câu trả cùng bộ cột) sang Supplier.
func supplierOf(r store.ListSuppliersRow) Supplier {
	return Supplier{ID: r.ID, Name: r.Name, Phone: logging.Phone(r.Phone), Note: r.Note, Status: r.Status, Version: r.Version}
}
