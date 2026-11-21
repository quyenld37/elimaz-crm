package order

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Giới hạn của q (khớp tham số q của listOrders trong openapi.yaml).
const (
	maxListQueryRunes = 100
	minTailDigits     = 4
)

// orderCodePattern: mã đơn DH + chữ số, không phân biệt hoa thường.
var orderCodePattern = regexp.MustCompile(`^(?i)DH[0-9]+$`)

// Cursor là vị trí sau dòng cuối của một trang danh sách đơn (created_at giảm, rồi id giảm): cùng khóa keyset và cùng
// cách mã hóa với danh sách khách nên dùng lại customer.ListCursor. Giá trị rỗng: trang đầu.
type Cursor = customer.ListCursor

// ParseCursor đọc chuỗi do Cursor.String tạo. Hỏng → ErrInvalidInput (của order).
func ParseCursor(s string) (Cursor, error) {
	c, err := customer.ParseListCursor(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("đọc con trỏ danh sách đơn: %w", ErrInvalidInput)
	}
	return c, nil
}

// ListSearch là phần tìm của danh sách đơn đã giải (cùng nghĩa với queries.OrderSearch; lớp HTTP chép sang, hai gói
// không nhập được nhau). CustomerRoots nil: không lọc khách; NameRoots nil: q không phải tên. Tối đa một trong Code (mã
// đơn viết hoa), Phone (SĐT chuẩn hóa), PhoneTail (chữ số đảo ngược của đuôi SĐT) khác rỗng. Empty: khách lọc không
// có, danh sách chắc chắn rỗng.
type ListSearch struct {
	CustomerRoots, NameRoots []uuid.UUID
	Code, Phone, PhoneTail   string
	Empty                    bool
}

// ResolveListSearch giải q và khách lọc customerID của danh sách đơn (cần module customer nên ở order; câu danh sách
// ghép view ở internal/queries, spec 04 mục 3): q là mã đơn DH…, SĐT người nhận (đủ, hay đuôi từ 4 chữ số) hoặc tên
// khách (customer.Search, tối đa customer.MaxListLimit nhóm); customerID: cả nhóm gốc của khách đó (gồm hồ sơ đã gộp),
// không có → Empty. Lỗi: ErrInvalidInput (q quá dài), customer.ErrQueryTooShort (q chỉ chữ số dưới 4 số; q tên quá
// ngắn). Không đưa q vào lỗi. Chạy trong tenancy.WithTenant.
func ResolveListSearch(ctx context.Context, tx pgx.Tx, q string, customerID *uuid.UUID) (ListSearch, error) {
	m, err := classifyQ(q)
	if err != nil {
		return ListSearch{}, err
	}
	s := ListSearch{Code: m.code, Phone: m.phone, PhoneTail: m.tail}
	if m.name != "" {
		hits, err := customer.Search(ctx, tx, m.name, customer.MaxListLimit)
		if err != nil {
			return ListSearch{}, fmt.Errorf("tìm khách của danh sách đơn: %w", err)
		}
		s.NameRoots = make([]uuid.UUID, 0, len(hits))
		for _, h := range hits {
			s.NameRoots = append(s.NameRoots, h.RootID)
		}
	}
	if customerID != nil {
		root, err := customer.RootOf(ctx, tx, *customerID)
		if errors.Is(err, customer.ErrNotFound) {
			return ListSearch{Empty: true}, nil
		}
		if err != nil {
			return ListSearch{}, fmt.Errorf("đọc gốc của khách lọc: %w", err)
		}
		s.CustomerRoots = []uuid.UUID{root}
	}
	return s, nil
}

// qMatch là q đã phân loại: đúng một trong code (mã đơn viết hoa), phone (SĐT chuẩn hóa), tail (chữ số đảo ngược của
// đuôi SĐT), name (chuỗi tìm tên).
type qMatch struct {
	code, phone, tail, name string
}

// classifyQ phân loại q như Task M2-10 (tìm khách) nhưng cho đơn: "DH" + chữ số → mã; chỉ chữ số (bỏ khoảng trắng,
// chấm, gạch, "+") chuẩn hóa được thành SĐT → SĐT đủ, không thì từ 4 chữ số → đuôi, dưới 4 → customer.ErrQueryTooShort;
// còn lại → tên. Không đưa q vào lỗi.
func classifyQ(q string) (qMatch, error) {
	q = strings.TrimSpace(q)
	switch {
	case q == "":
		return qMatch{}, nil
	case utf8.RuneCountInString(q) > maxListQueryRunes:
		return qMatch{}, fmt.Errorf("chuỗi tìm đơn quá dài: %w", ErrInvalidInput)
	case orderCodePattern.MatchString(q):
		return qMatch{code: strings.ToUpper(q)}, nil
	}
	digits, ok := onlyDigits(q)
	if !ok {
		return qMatch{name: q}, nil
	}
	if p, err := customer.NormalizePhone(q); err == nil {
		return qMatch{phone: string(p)}, nil
	}
	if len(digits) < minTailDigits {
		return qMatch{}, fmt.Errorf("đuôi SĐT của đơn: %w", customer.ErrQueryTooShort)
	}
	return qMatch{tail: customer.ReverseDigits(logging.Phone(digits))}, nil
}

// onlyDigits bỏ khoảng trắng, chấm, gạch, "+" của s và báo phần còn lại có phải toàn chữ số (ít nhất một).
func onlyDigits(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '.' || r == '-' || r == '+' || r == '\t':
		default:
			return "", false
		}
	}
	return b.String(), b.Len() > 0
}
