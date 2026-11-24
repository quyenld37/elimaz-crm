package customer

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Cách khớp của một dòng tìm (CustomerListItem.matchedBy).
const (
	MatchedByCode      = "code"
	MatchedByPhone     = "phone"
	MatchedByPhoneTail = "phone_tail"
	MatchedByName      = "name"
)

// Giới hạn của Search và List.
const (
	DefaultSearchLimit = 20  // Search khi limit ≤ 0
	MaxListLimit       = 100 // Search, List tối đa
	minTailDigits      = 4   // đuôi số ngắn nhất
	minNameRunes       = 2   // tên (sau chuẩn hóa) ngắn nhất
	maxQueryRunes      = 100 // chuỗi tìm dài nhất (khớp maxLength của tham số q)
	searchCandidates   = 500 // số hồ sơ khớp tối đa đọc trước khi gom theo gốc
)

// codePattern: mã khách KH + chữ số, không phân biệt hoa thường.
var codePattern = regexp.MustCompile(`^(?i)KH[0-9]+$`)

// SearchHit là một nhóm khách (theo gốc) khớp chuỗi tìm, hay một dòng của danh sách: mã, tên, chỉ số của gốc, SĐT của
// cả nhóm. MatchedBy là một hằng MatchedBy* (rỗng ở List).
type SearchHit struct {
	RootID    uuid.UUID
	Code      string
	FullName  Name
	Phones    []logging.Phone
	MatchedBy string
	Stats     Stats
}

// Search tìm khách trong tenant của tx theo q, gom theo gốc, tối đa limit nhóm (≤ 0: DefaultSearchLimit; tối đa
// MaxListLimit), mua gần nhất trước. Phân loại q:
//   - "KH" + chữ số (không phân biệt hoa thường): mã đúng;
//   - chỉ chữ số sau khi bỏ khoảng trắng, chấm, gạch, "+": SĐT chuẩn hóa được → khớp đúng SĐT; không thì từ 4 chữ số
//     → đuôi số; dưới 4 chữ số → ErrQueryTooShort;
//   - còn lại: tên không dấu (textnorm.Fold), mỗi từ phải có trong tên, không theo thứ tự; dưới 2 ký tự sau chuẩn
//     hóa → ErrQueryTooShort.
//
// Không ghi q vào log hay lỗi (có thể là SĐT, tên).
func Search(ctx context.Context, tx pgx.Tx, q string, limit int) ([]SearchHit, error) {
	return search(ctx, tx, q, nil, limit)
}

// SearchTagged như Search nhưng chỉ giữ nhóm có nhãn tagID ở bất kỳ hồ sơ nào trong nhóm (lọc trước khi cắt limit).
func SearchTagged(ctx context.Context, tx pgx.Tx, q string, tagID uuid.UUID, limit int) ([]SearchHit, error) {
	return search(ctx, tx, q, &tagID, limit)
}

func search(ctx context.Context, tx pgx.Tx, q string, tag *uuid.UUID, limit int) ([]SearchHit, error) {
	limit = clampLimit(limit, DefaultSearchLimit)
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > maxQueryRunes {
		return nil, ErrInvalidInput
	}
	roots, matched, err := matchRoots(ctx, store.New(tx), q)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return []SearchHit{}, nil
	}
	rows, err := store.New(tx).ListSearchRoots(ctx, store.ListSearchRootsParams{RootIds: roots, TagID: db.NullUUID(tag), Lim: int32(limit)}) //nolint:gosec // đã chặn ở clampLimit
	if err != nil {
		return nil, fmt.Errorf("đọc khách gốc của kết quả tìm: %w", err)
	}
	hits := make([]SearchHit, 0, len(rows))
	for _, r := range rows {
		hits = append(hits, hitOf(r.ID, r.Code, r.FullName, r.OrderCount, r.DeliveredCount, r.ReturnedCount, r.TotalSpent,
			r.LastOrderAt, matched))
	}
	return hits, fillPhones(ctx, tx, hits)
}

// matchRoots phân loại q và trả các gốc khớp (không trùng, tối đa searchCandidates) cùng cách khớp.
func matchRoots(ctx context.Context, s *store.Queries, q string) ([]uuid.UUID, string, error) {
	if codePattern.MatchString(q) {
		roots, err := s.SearchCode(ctx, strings.ToUpper(q))
		if err != nil {
			return nil, "", fmt.Errorf("tìm khách theo mã: %w", err)
		}
		return dedupe(roots), MatchedByCode, nil
	}
	if digits, ok := phoneDigits(q); ok {
		if p, err := NormalizePhone(q); err == nil {
			roots, err := s.SearchPhoneExact(ctx, string(p))
			if err != nil {
				return nil, "", fmt.Errorf("tìm khách theo SĐT: %w", err)
			}
			return dedupe(roots), MatchedByPhone, nil
		}
		if len(digits) < minTailDigits {
			return nil, "", ErrQueryTooShort
		}
		lo := ReverseDigits(logging.Phone(digits))
		hi := lo[:len(lo)-1] + string(lo[len(lo)-1]+1)
		roots, err := s.SearchPhoneTail(ctx, store.SearchPhoneTailParams{Lo: lo, Hi: hi, Lim: searchCandidates})
		if err != nil {
			return nil, "", fmt.Errorf("tìm khách theo đuôi số: %w", err)
		}
		return roots, MatchedByPhoneTail, nil
	}
	folded := NormalizeName(q)
	if utf8.RuneCountInString(folded) < minNameRunes {
		return nil, "", ErrQueryTooShort
	}
	rows, err := s.SearchByName(ctx, store.SearchByNameParams{Words: strings.Fields(folded), Lim: searchCandidates})
	if err != nil {
		return nil, "", fmt.Errorf("tìm khách theo tên: %w", err)
	}
	roots := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		roots = append(roots, r.RootID)
	}
	return dedupe(roots), MatchedByName, nil
}

// phoneDigits bỏ khoảng trắng, chấm, gạch, "+" khỏi q; ok khi phần còn lại khác rỗng và chỉ có chữ số.
func phoneDigits(q string) (string, bool) {
	s := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '.', '-', '+':
			return -1
		}
		return r
	}, q)
	return s, s != "" && !strings.ContainsFunc(s, notDigit)
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func clampLimit(limit, def int) int {
	if limit <= 0 {
		return def
	}
	return min(limit, MaxListLimit)
}

func hitOf(id uuid.UUID, code, name string, orders, delivered, returned int32, spent int64, last pgtype.Timestamptz,
	matched string,
) SearchHit {
	h := SearchHit{
		RootID: id, Code: code, FullName: Name(name), Phones: []logging.Phone{}, MatchedBy: matched,
		Stats: Stats{OrderCount: int(orders), DeliveredCount: int(delivered), ReturnedCount: int(returned), TotalSpent: spent},
	}
	if last.Valid {
		t := last.Time
		h.Stats.LastOrderAt = &t
	}
	if done := h.Stats.DeliveredCount + h.Stats.ReturnedCount; done > 0 {
		h.Stats.ReturnRateBP = h.Stats.ReturnedCount * 10000 / done
	}
	return h
}

// fillPhones đọc SĐT của mọi hồ sơ trong các nhóm của hits bằng một câu.
func fillPhones(ctx context.Context, tx pgx.Tx, hits []SearchHit) error {
	if len(hits) == 0 {
		return nil
	}
	roots := make([]uuid.UUID, len(hits))
	at := make(map[uuid.UUID]int, len(hits))
	for i, h := range hits {
		roots[i], at[h.RootID] = h.RootID, i
	}
	rows, err := store.New(tx).ListGroupPhones(ctx, roots)
	if err != nil {
		return fmt.Errorf("đọc SĐT của nhóm khách: %w", err)
	}
	for _, r := range rows {
		i := at[r.RootID]
		hits[i].Phones = append(hits[i].Phones, logging.Phone(r.Value))
	}
	return nil
}

// ListCursor là vị trí sau dòng cuối của một trang danh sách khách gốc (created_at giảm, rồi id giảm).
type ListCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// cursorLen: 8 byte micro giây Unix (big-endian) + 16 byte id.
const cursorLen = 8 + 16

// String mã hóa con trỏ thành chuỗi mờ cho API (base64url của 24 byte: micro giây Unix, id).
func (c ListCursor) String() string {
	b := binary.BigEndian.AppendUint64(make([]byte, 0, cursorLen), uint64(c.CreatedAt.UnixMicro()))
	return base64.RawURLEncoding.EncodeToString(append(b, c.ID[:]...))
}

// ParseListCursor đọc chuỗi do ListCursor.String tạo. Hỏng → ErrInvalidInput.
func ParseListCursor(s string) (ListCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) != cursorLen {
		return ListCursor{}, fmt.Errorf("đọc con trỏ: %w", ErrInvalidInput)
	}
	c := ListCursor{CreatedAt: time.UnixMicro(int64(binary.BigEndian.Uint64(raw[:8]))).UTC()} //nolint:gosec // bit do String ghi
	copy(c.ID[:], raw[8:])
	return c, nil
}

// params trả hai tham số keyset (created_at, id) của câu SQL; c nil (trang đầu) → hai giá trị NULL.
func (c *ListCursor) params() (pgtype.Timestamptz, pgtype.UUID) {
	if c == nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}
	}
	return pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}, pgtype.UUID{Bytes: c.ID, Valid: true}
}

// pageOf cắt rows (đọc limit + 1 dòng) còn limit dòng và trả con trỏ sau dòng cuối khi còn trang sau (nil khi hết).
func pageOf[T any](rows []T, limit int, key func(T) ListCursor) ([]T, *ListCursor) {
	if len(rows) <= limit {
		return rows, nil
	}
	next := key(rows[limit-1])
	return rows[:limit], &next
}

// ListFilter là bộ lọc của List. TagID: chỉ nhóm có nhãn đó ở bất kỳ hồ sơ nào. After: trang sau. Limit ≤ 0: 50; tối
// đa MaxListLimit.
type ListFilter struct {
	TagID *uuid.UUID
	After *ListCursor
	Limit int
}

// defaultListLimit khớp mặc định của tham số limit trong openapi.yaml.
const defaultListLimit = 50

// List trả một trang khách gốc của tenant của tx, tạo mới nhất trước, và con trỏ trang sau (nil khi hết).
func List(ctx context.Context, tx pgx.Tx, f ListFilter) ([]SearchHit, *ListCursor, error) {
	limit := clampLimit(f.Limit, defaultListLimit)
	p := store.ListRootsParams{TagID: db.NullUUID(f.TagID), Lim: int32(limit + 1)} //nolint:gosec // đã chặn ở clampLimit
	p.AfterCreated, p.AfterID = f.After.params()
	rows, err := store.New(tx).ListRoots(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách khách: %w", err)
	}
	rows, next := pageOf(rows, limit, func(r store.ListRootsRow) ListCursor {
		return ListCursor{CreatedAt: r.CreatedAt, ID: r.ID}
	})
	hits := make([]SearchHit, 0, len(rows))
	for _, r := range rows {
		hits = append(hits, hitOf(r.ID, r.Code, r.FullName, r.OrderCount, r.DeliveredCount, r.ReturnedCount, r.TotalSpent,
			r.LastOrderAt, ""))
	}
	return hits, next, fillPhones(ctx, tx, hits)
}
