package inventory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Hàng hoàn của đơn cũ (trước CRM) về kho (kế hoạch M7, Task 264 = M7-19; MO08, Q11, G05, KH08). Không tạo đơn, vận
// đơn, khách; không đọc Nhanh. Mỗi kiện: phiếu nhập (import) mọi dòng, phiếu hỏng (damage) cho dòng hỏng, dòng nối
// inventory.legacy_returns có tham chiếu chuẩn hóa duy nhất, ghi sổ cả hai phiếu — một transaction của bên gọi.
//
// Thứ tự khóa (ADR-46 nhóm 11): stock_documents, stock_document_lines (dòng mới của tx này) → legacy_returns (chèn;
// trùng chờ tx kia) → stock_balances theo variant_id tăng dần (PostDocument phiếu nhập, rồi phiếu hỏng trên tập con).

// Tình trạng của một dòng hàng hoàn.
const (
	ConditionSellable = "sellable" // bán lại được: chỉ nhập
	ConditionDamaged  = "damaged"  // hỏng: nhập rồi chuyển hàng lỗi (không bán được, KH08)
)

// Giới hạn của phiếu hàng hoàn (hợp đồng CreateLegacyReturnRequest; CHECK ở migrations/0706_inventory_legacy_returns.sql).
const (
	legacyRefMin    = 3
	legacyRefMax    = 64
	legacyNoteMax   = 500
	legacyQtyMax    = 1000
	legacyLinesMax  = 200
	legacyNotePrefx = "Hàng hoàn đơn cũ "
)

// ErrDuplicateReference báo tham chiếu (sau chuẩn hóa) đã nhập; lỗi cụ thể là *DuplicateReferenceError.
var ErrDuplicateReference = errors.New("inventory: kiện hàng hoàn đã nhập")

// DuplicateReferenceError là chi tiết của ErrDuplicateReference: phiếu hàng hoàn đã có cùng tham chiếu.
type DuplicateReferenceError struct {
	ExistingID uuid.UUID
}

func (e *DuplicateReferenceError) Error() string {
	return fmt.Sprintf("%v: %s", ErrDuplicateReference, e.ExistingID)
}

// Is cho errors.Is(err, ErrDuplicateReference).
func (e *DuplicateReferenceError) Is(target error) bool { return target == ErrDuplicateReference }

// LegacyLine là một dòng hàng hoàn: biến thể, số lượng 1–1000, tình trạng (ConditionSellable | ConditionDamaged).
type LegacyLine struct {
	VariantID uuid.UUID
	Qty       int32
	Condition string
}

// LegacyReturnInput là một kiện hàng hoàn: kho nhận, tham chiếu trên kiện (mã vận đơn, mã đơn cũ; 3–64 ký tự), ghi
// chú (tối đa 500 ký tự, chỉ nằm trong chứng từ), dòng (1–200; không hai dòng cùng biến thể cùng tình trạng).
type LegacyReturnInput struct {
	WarehouseID     uuid.UUID
	Reference, Note string
	Lines           []LegacyLine
}

// LegacyReturn là một phiếu hàng hoàn đã ghi sổ. DamageDocumentID nil khi mọi dòng bán được. Lines dựng lại từ dòng
// phiếu nhập trừ dòng phiếu hỏng (theo variant_id, tốt trước hỏng).
type LegacyReturn struct {
	ID, WarehouseID  uuid.UUID
	Reference        string
	StockDocumentID  uuid.UUID
	DamageDocumentID *uuid.UUID
	Lines            []LegacyLine
	CreatedBy        uuid.UUID
	CreatedAt        time.Time
}

// LegacyReturnFilter lọc ListLegacyReturns. AllWarehouses = false → chỉ các kho WarehouseIDs (rỗng: không phiếu nào).
// From, To: khoảng giờ tạo [From, To) (nil: không chặn). Cursor: id cuối của trang trước. Limit 1–200.
type LegacyReturnFilter struct {
	AllWarehouses bool
	WarehouseIDs  []uuid.UUID
	From, To      *time.Time
	Cursor        *uuid.UUID
	Limit         int32
}

// normalizeReference bỏ mọi khoảng trắng (kể cả tab, khoảng trắng không ngắt) và viết hoa: "vtp 1234 5678" và
// "VTP12345678" là một kiện.
func normalizeReference(s string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s))
}

// checkLegacy kiểm dữ liệu trước mọi câu database; trả tham chiếu đã bỏ khoảng trắng hai đầu và bản chuẩn hóa.
func checkLegacy(in LegacyReturnInput, a Actor) (ref, norm string, err error) {
	ref = strings.TrimSpace(in.Reference)
	norm = normalizeReference(ref)
	switch n := utf8.RuneCountInString(ref); {
	case a.Type != ActorStaff || a.ID == uuid.Nil:
		return "", "", fmt.Errorf("%w: hàng hoàn cần nhân viên", ErrInvalidInput)
	case n < legacyRefMin || n > legacyRefMax || utf8.RuneCountInString(norm) < legacyRefMin:
		return "", "", fmt.Errorf("%w: tham chiếu", ErrInvalidInput)
	case utf8.RuneCountInString(in.Note) > legacyNoteMax:
		return "", "", fmt.Errorf("%w: ghi chú hàng hoàn", ErrInvalidInput)
	case len(in.Lines) == 0 || len(in.Lines) > legacyLinesMax:
		return "", "", fmt.Errorf("%w: số dòng hàng hoàn", ErrInvalidInput)
	}
	seen := make(map[LegacyLine]bool, len(in.Lines))
	for _, l := range in.Lines {
		key := LegacyLine{VariantID: l.VariantID, Condition: l.Condition}
		switch {
		case l.Condition != ConditionSellable && l.Condition != ConditionDamaged:
			return "", "", fmt.Errorf("%w: tình trạng %q", ErrInvalidInput, l.Condition)
		case l.Qty < 1 || l.Qty > legacyQtyMax:
			return "", "", fmt.Errorf("%w: số lượng hàng hoàn", ErrInvalidInput)
		case seen[key]:
			return "", "", fmt.Errorf("%w: trùng dòng biến thể %s", ErrInvalidInput, l.VariantID)
		}
		seen[key] = true
	}
	return ref, norm, nil
}

// legacyDocLines gộp dòng hàng hoàn thành dòng phiếu nhập (mọi dòng, cộng theo biến thể) và dòng phiếu hỏng.
func legacyDocLines(lines []LegacyLine) (imports, damaged []DocumentLine) {
	sum := map[uuid.UUID]int32{}
	for _, l := range lines {
		sum[l.VariantID] += l.Qty
		if l.Condition == ConditionDamaged {
			damaged = append(damaged, DocumentLine{VariantID: l.VariantID, Quantity: l.Qty})
		}
	}
	for v, q := range sum {
		imports = append(imports, DocumentLine{VariantID: v, Quantity: q})
	}
	byVariant := func(a, b DocumentLine) int { return compareUUID(a.VariantID, b.VariantID) }
	slices.SortFunc(imports, byVariant)
	slices.SortFunc(damaged, byVariant)
	return imports, damaged
}

// CreateLegacyReturn nhập kiện hàng hoàn in vào kho trong tx của bên gọi: phiếu nhập mọi dòng (on_hand +qty), phiếu hỏng
// cho dòng hỏng (damaged +qty), dòng legacy_returns, ghi sổ hai phiếu (sổ doc_type import, damage). Tham chiếu đã dùng
// (sau chuẩn hóa, kể cả yêu cầu song song vừa commit) → *DuplicateReferenceError. Lỗi khác: ErrInvalidInput (dữ liệu
// sai, biến thể không có), ErrNotFound (kho không có). Mọi lỗi: bên gọi lùi transaction. Audit do bên gọi ghi.
func (w Writer) CreateLegacyReturn(ctx context.Context, tx pgx.Tx, in LegacyReturnInput, a Actor) (LegacyReturn, error) {
	ref, norm, err := checkLegacy(in, a)
	if err != nil {
		return LegacyReturn{}, err
	}
	q := store.New(tx)
	if err := duplicateOf(ctx, q, norm); err != nil {
		return LegacyReturn{}, err
	}
	note := legacyNotePrefx + ref
	if in.Note != "" {
		note += " — " + in.Note
	}
	imports, damaged := legacyDocLines(in.Lines)
	imp, err := draftWithLines(ctx, tx, DocumentInput{WarehouseID: in.WarehouseID, Kind: KindImport, Note: note}, imports, a)
	if err != nil {
		return LegacyReturn{}, err
	}
	var dmg *Document
	if len(damaged) > 0 {
		d, err := draftWithLines(ctx, tx, DocumentInput{WarehouseID: in.WarehouseID, Kind: KindDamage, Note: note}, damaged, a)
		if err != nil {
			return LegacyReturn{}, err
		}
		dmg = &d
	}
	var dmgID *uuid.UUID
	if dmg != nil {
		dmgID = &dmg.ID
	}
	row, err := q.InsertLegacyReturn(ctx, store.InsertLegacyReturnParams{
		Reference: ref, ReferenceNorm: norm, StockDocumentID: imp.ID, DamageDocumentID: pgUUID(dmgID),
		CreatedBy: a.ID, WarehouseID: in.WarehouseID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Yêu cầu song song cùng tham chiếu đã commit trước (kho có sẵn: phiếu nháp vừa tạo ở kho này).
		if err := duplicateOf(ctx, q, norm); err != nil {
			return LegacyReturn{}, err
		}
		return LegacyReturn{}, errors.New("ghi hàng hoàn: không chèn được mà không thấy dòng trùng")
	}
	if err != nil {
		return LegacyReturn{}, fmt.Errorf("ghi hàng hoàn: %w", err)
	}
	if _, err := w.PostDocument(ctx, tx, imp.ID, imp.Version, a); err != nil {
		return LegacyReturn{}, fmt.Errorf("ghi sổ phiếu nhập hàng hoàn: %w", err)
	}
	if dmg != nil {
		if _, err := w.postDocument(ctx, tx, dmg.ID, dmg.Version, a, legacyMoves); err != nil {
			return LegacyReturn{}, fmt.Errorf("ghi sổ phiếu hỏng hàng hoàn: %w", err)
		}
	}
	return LegacyReturn{
		ID: row.ID, WarehouseID: in.WarehouseID, Reference: ref, StockDocumentID: imp.ID, DamageDocumentID: dmgID,
		Lines: legacyLinesOf(imports, damaged), CreatedBy: a.ID, CreatedAt: row.CreatedAt,
	}, nil
}

// legacyMoves: phiếu hỏng của hàng hoàn chuyển phần vừa nhập trong cùng tx (số hỏng ≤ số nhập của biến thể) nên
// không xét số bán được của tồn cũ (có thể âm sau kiểm kho thiếu khi đang giữ hàng) — như hàng khách trả
// (ReceiveCustomerReturn, condNone). Loại khác theo movesFor.
func legacyMoves(kind string, q int32) ([]move, string) {
	if kind == KindDamage {
		return []move{{bucketDamaged, 1, condNone, false}}, "damage"
	}
	return movesFor(kind, q)
}

// duplicateOf: tham chiếu norm đã có → *DuplicateReferenceError; chưa có → nil.
func duplicateOf(ctx context.Context, q *store.Queries, norm string) error {
	id, err := q.FindLegacyReturnByNorm(ctx, norm)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("đọc hàng hoàn cùng tham chiếu: %w", err)
	}
	return &DuplicateReferenceError{ExistingID: id}
}

// draftWithLines tạo phiếu nháp in và đặt dòng lines (CreateDocument, ReplaceLines của M1).
func draftWithLines(ctx context.Context, tx pgx.Tx, in DocumentInput, lines []DocumentLine, a Actor) (Document, error) {
	d, err := CreateDocument(ctx, tx, in, a)
	if err != nil {
		return Document{}, err
	}
	return ReplaceLines(ctx, tx, d.ID, d.Version, lines)
}

// legacyLinesOf dựng dòng hàng hoàn từ dòng phiếu nhập và phiếu hỏng: phần tốt = nhập − hỏng (theo biến thể).
func legacyLinesOf(imports, damaged []DocumentLine) []LegacyLine {
	bad := make(map[uuid.UUID]int32, len(damaged))
	for _, l := range damaged {
		bad[l.VariantID] += l.Quantity
	}
	out := make([]LegacyLine, 0, len(imports)+len(damaged))
	for _, l := range imports {
		if good := l.Quantity - bad[l.VariantID]; good > 0 {
			out = append(out, LegacyLine{VariantID: l.VariantID, Qty: good, Condition: ConditionSellable})
		}
		if b := bad[l.VariantID]; b > 0 {
			out = append(out, LegacyLine{VariantID: l.VariantID, Qty: b, Condition: ConditionDamaged})
		}
	}
	return out
}

// GetLegacyReturn đọc phiếu hàng hoàn id kèm dòng. Không có (hoặc của tenant khác) → ErrNotFound.
func GetLegacyReturn(ctx context.Context, tx pgx.Tx, id uuid.UUID) (LegacyReturn, error) {
	q := store.New(tx)
	r, err := q.GetLegacyReturn(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return LegacyReturn{}, ErrNotFound
	}
	if err != nil {
		return LegacyReturn{}, fmt.Errorf("đọc hàng hoàn: %w", err)
	}
	out, err := withLegacyLines(ctx, q, []LegacyReturn{legacyOf(store.ListLegacyReturnsRow(r))})
	if err != nil {
		return LegacyReturn{}, err
	}
	return out[0], nil
}

// ListLegacyReturns trả một trang phiếu hàng hoàn theo f, mới nhất trước, kèm dòng; next là con trỏ trang sau (nil: hết).
func ListLegacyReturns(ctx context.Context, tx pgx.Tx, f LegacyReturnFilter) ([]LegacyReturn, *uuid.UUID, error) {
	if f.Limit < 1 || f.Limit > legacyLinesMax {
		return nil, nil, fmt.Errorf("%w: limit", ErrInvalidInput)
	}
	q := store.New(tx)
	rows, err := q.ListLegacyReturns(ctx, store.ListLegacyReturnsParams{
		AllWarehouses: f.AllWarehouses, WarehouseIds: append([]uuid.UUID{}, f.WarehouseIDs...),
		FromAt: pgTime(f.From), ToAt: pgTime(f.To), Cursor: pgUUID(f.Cursor), Lim: f.Limit + 1,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách hàng hoàn: %w", err)
	}
	var next *uuid.UUID
	if len(rows) > int(f.Limit) {
		rows = rows[:f.Limit]
		next = &rows[len(rows)-1].ID
	}
	rs := make([]LegacyReturn, 0, len(rows))
	for _, r := range rows {
		rs = append(rs, legacyOf(r))
	}
	rs, err = withLegacyLines(ctx, q, rs)
	return rs, next, err
}

func legacyOf(r store.ListLegacyReturnsRow) LegacyReturn {
	return LegacyReturn{
		ID: r.ID, WarehouseID: r.WarehouseID, Reference: r.Reference, StockDocumentID: r.StockDocumentID,
		DamageDocumentID: fromPgUUID(r.DamageDocumentID), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
	}
}

// withLegacyLines đọc dòng phiếu của mọi phiếu trong rs bằng một câu rồi dựng Lines.
func withLegacyLines(ctx context.Context, q *store.Queries, rs []LegacyReturn) ([]LegacyReturn, error) {
	docs := make([]uuid.UUID, 0, 2*len(rs))
	for _, r := range rs {
		docs = append(docs, r.StockDocumentID)
		if r.DamageDocumentID != nil {
			docs = append(docs, *r.DamageDocumentID)
		}
	}
	rows, err := q.LegacyReturnDocumentLines(ctx, docs)
	if err != nil {
		return nil, fmt.Errorf("đọc dòng hàng hoàn: %w", err)
	}
	byDoc := map[uuid.UUID][]DocumentLine{}
	for _, l := range rows {
		byDoc[l.DocumentID] = append(byDoc[l.DocumentID], DocumentLine{VariantID: l.VariantID, Quantity: l.Quantity})
	}
	for i, r := range rs {
		var damaged []DocumentLine
		if r.DamageDocumentID != nil {
			damaged = byDoc[*r.DamageDocumentID]
		}
		rs[i].Lines = legacyLinesOf(byDoc[r.StockDocumentID], damaged)
	}
	return rs, nil
}

func pgTime(t *time.Time) pgtype.Timestamptz {
	var v time.Time
	if t != nil {
		v = *t
	}
	return pgtype.Timestamptz{Time: v, Valid: t != nil}
}
