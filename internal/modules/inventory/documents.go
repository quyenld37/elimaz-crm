package inventory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Chứng từ kho (kế hoạch M1, Task M1-29; 01 mục 3 "có nháp và duyệt", 05 mục 2): soạn nháp (CreateDocument,
// ReplaceLines), ghi sổ (Writer.PostDocument) hoặc hủy (CancelDocument). Chỉ ghi sổ mới đổi số dư.
//
// Thứ tự khóa (docs/adr/0046-thu-tu-khoa.md nhóm 11): suppliers (FOR SHARE khi tạo phiếu nhập) → stock_documents
// (dòng phiếu) → stock_document_lines → stock_balances theo variant_id tăng dần (một kho mỗi phiếu). Không đường nào
// khóa số dư trước phiếu.

// Loại phiếu (cột kind).
const (
	KindImport       = "import"        // nhập: on_hand +q
	KindExport       = "export"        // xuất: on_hand −q, không ăn vào phần giữ (KH10)
	KindDamage       = "damage"        // chuyển hàng lỗi: damaged +q (KH08)
	KindDamageExport = "damage_export" // xuất hàng lỗi: damaged −q, on_hand −q
	KindAdjust       = "adjust"        // điều chỉnh: on_hand ±q
)

// Trạng thái phiếu.
const (
	StatusDraft     = "draft"
	StatusPosted    = "posted"
	StatusCancelled = "cancelled"
)

// Giới hạn của phiếu (CHECK tương ứng ở migrations/0116_stock_documents.sql).
const (
	MaxDocumentLines = 500
	maxLineQuantity  = 1_000_000
	maxMoney         = 9007199254740991
	stepPost         = "post"
)

// Document là một phiếu kho. Lines chỉ có ở GetDocument, ReplaceLines, PostDocument (ListDocuments để trống).
// SupplierName đọc cả khi nhà cung cấp đã lưu trữ.
type Document struct {
	ID, WarehouseID     uuid.UUID
	Code, Kind, Status  string
	SupplierID          *uuid.UUID
	SupplierName        string
	Note                string
	CreatedBy, PostedBy *uuid.UUID
	PostedAt            *time.Time
	CreatedAt           time.Time
	Version             int64
	Lines               []DocumentLine
}

// DocumentInput là dữ liệu tạo phiếu nháp. SupplierID chỉ cho phiếu nhập.
type DocumentInput struct {
	WarehouseID uuid.UUID
	Kind        string
	SupplierID  *uuid.UUID
	Note        string
}

// DocumentLine là một dòng phiếu: biến thể, số lượng (> 0; adjust: ≠ 0, âm là giảm), giá nhập (đồng, tùy chọn).
type DocumentLine struct {
	VariantID uuid.UUID
	Quantity  int32
	UnitCost  *int64
}

// DocumentFilter lọc ListDocuments. AllWarehouses = false → chỉ các kho WarehouseIDs (rỗng: không phiếu nào).
// Status, Kind rỗng: mọi giá trị. Cursor: id cuối của trang trước. Limit 1–200.
type DocumentFilter struct {
	AllWarehouses bool
	WarehouseIDs  []uuid.UUID
	Status, Kind  string
	Cursor        *uuid.UUID
	Limit         int32
}

// move của từng loại phiếu cho một dòng số lượng q > 0 (adjust âm dùng adjustDown). Thứ tự áp trên cùng dòng số dư.
func movesFor(kind string, q int32) ([]move, string) {
	switch kind {
	case KindImport:
		return []move{{bucketOnHand, 1, condNone, false}}, "import"
	case KindExport:
		return []move{{bucketOnHand, -1, condAvailable, false}}, "export"
	case KindDamage:
		return []move{{bucketDamaged, 1, condAvailable, false}}, "damage"
	case KindDamageExport:
		// on_shelf (không phải on_hand ≥ q): không trừ on_hand xuống dưới phần đang giao (Task 406).
		return []move{{bucketDamaged, -1, condDamaged, false}, {bucketOnHand, -1, condOnShelf, false}}, "damage"
	case KindAdjust:
		if q < 0 {
			return []move{{bucketOnHand, -1, condAvailable, false}}, "adjust"
		}
		return []move{{bucketOnHand, 1, condNone, false}}, "adjust"
	}
	return nil, ""
}

func validKind(kind string) bool {
	m, _ := movesFor(kind, 1)
	return m != nil
}

var docPrefix = map[string]string{ //nolint:gochecknoglobals // bảng tiền tố mã phiếu cố định, chỉ đọc.
	KindImport: "NK", KindExport: "XK", KindDamage: "HL", KindDamageExport: "XL", KindAdjust: "DC",
}

// CreateDocument tạo phiếu nháp (chưa có dòng) ở kho in.WarehouseID; mã phiếu sinh từ sequence. Lỗi: ErrInvalidInput
// (loại lạ, ghi chú dài), ErrSupplierNotForKind, ErrNotFound (kho hay nhà cung cấp không có), ErrSupplierArchived.
func CreateDocument(ctx context.Context, tx pgx.Tx, in DocumentInput, a Actor) (Document, error) {
	switch {
	case !a.valid():
		return Document{}, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	case !validKind(in.Kind):
		return Document{}, fmt.Errorf("%w: loại phiếu %q", ErrInvalidInput, in.Kind)
	case utf8.RuneCountInString(in.Note) > maxNote:
		return Document{}, fmt.Errorf("%w: ghi chú phiếu", ErrInvalidInput)
	case in.SupplierID != nil && in.Kind != KindImport:
		return Document{}, ErrSupplierNotForKind
	}
	q := store.New(tx)
	if in.SupplierID != nil {
		status, err := q.ShareSupplier(ctx, *in.SupplierID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return Document{}, ErrNotFound
		case err != nil:
			return Document{}, fmt.Errorf("đọc nhà cung cấp: %w", err)
		case status != SupplierActive:
			return Document{}, ErrSupplierArchived
		}
	}
	n, err := q.NextDocumentNumber(ctx)
	if err != nil {
		return Document{}, fmt.Errorf("sinh mã phiếu: %w", err)
	}
	id, err := q.InsertDocument(ctx, store.InsertDocumentParams{
		Code: fmt.Sprintf("%s-%06d", docPrefix[in.Kind], n), Kind: in.Kind, SupplierID: pgUUID(in.SupplierID),
		Note: in.Note, CreatedBy: actorUUID(a), WarehouseID: in.WarehouseID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound // kho không có hoặc của tenant khác
	}
	if err != nil {
		return Document{}, fmt.Errorf("ghi phiếu: %w", err)
	}
	return GetDocument(ctx, tx, id)
}

// ReplaceLines thay toàn bộ dòng của phiếu nháp id khi version còn khớp (khóa dòng phiếu rồi so); version tăng 1.
// Lỗi: ErrNotFound, ErrDocumentNotDraft, ErrVersionConflict, ErrInvalidInput (quá MaxDocumentLines dòng, số lượng sai
// theo loại, trùng biến thể, giá nhập âm, biến thể không có).
func ReplaceLines(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, lines []DocumentLine) (Document, error) {
	q := store.New(tx)
	d, err := lockDraft(ctx, q, id, version)
	if err != nil {
		return Document{}, err
	}
	if err := checkLines(d.Kind, lines); err != nil {
		return Document{}, err
	}
	if err := q.DeleteDocumentLines(ctx, id); err != nil {
		return Document{}, fmt.Errorf("xóa dòng phiếu: %w", err)
	}
	for _, l := range lines {
		cost := pgtype.Int8{}
		if l.UnitCost != nil {
			cost = pgtype.Int8{Int64: *l.UnitCost, Valid: true}
		}
		n, err := q.InsertDocumentLine(ctx, store.InsertDocumentLineParams{
			Quantity: l.Quantity, UnitCost: cost, VariantID: l.VariantID, DocumentID: id,
		})
		if err != nil {
			return Document{}, fmt.Errorf("ghi dòng phiếu: %w", err)
		}
		if n == 0 {
			return Document{}, fmt.Errorf("%w: biến thể %s không có", ErrInvalidInput, l.VariantID)
		}
	}
	if _, err := q.TouchDocument(ctx, store.TouchDocumentParams{ID: id, Version: version}); err != nil {
		return Document{}, fmt.Errorf("tăng version phiếu: %w", err)
	}
	return GetDocument(ctx, tx, id)
}

// PostDocument ghi sổ phiếu nháp id đúng version trong tx của bên gọi: đổi trạng thái (một câu UPDATE có điều kiện:
// hai yêu cầu song song → một thắng), rồi mỗi dòng theo variant_id tăng dần đổi số dư theo loại phiếu qua applyDelta
// (sổ doc_type theo loại, doc_id = id phiếu, step post) và phát một stock.changed. Dòng đầu tiên không đủ hàng →
// *InsufficientError (Is ErrInsufficient): bên gọi phải lùi transaction, không dòng nào được ghi. Lỗi khác:
// ErrNotFound, ErrDocumentNotDraft, ErrVersionConflict, ErrInvalidInput (phiếu không dòng, dòng sai theo loại).
func (w Writer) PostDocument(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, a Actor) (Document, error) {
	return w.postDocument(ctx, tx, id, version, a, movesFor)
}

// movesFunc trả các thay đổi số dư của một dòng phiếu loại kind số lượng q, kèm doc_type của sổ.
type movesFunc func(kind string, q int32) ([]move, string)

// postDocument là PostDocument với bảng thay đổi số dư moves (movesFor; hàng hoàn đơn cũ dùng legacyMoves).
func (w Writer) postDocument(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, a Actor, moves movesFunc,
) (Document, error) {
	if !a.valid() {
		return Document{}, fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	q := store.New(tx)
	row, err := q.PostDocument(ctx, store.PostDocumentParams{PostedBy: actorUUID(a), ID: id, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, whyNotDraft(ctx, q, id, version)
	}
	if err != nil {
		return Document{}, fmt.Errorf("ghi sổ phiếu: %w", err)
	}
	lines, err := q.ListDocumentLines(ctx, id)
	if err != nil {
		return Document{}, fmt.Errorf("đọc dòng phiếu: %w", err)
	}
	dl := make([]DocumentLine, 0, len(lines))
	for _, l := range lines {
		dl = append(dl, DocumentLine{VariantID: l.VariantID, Quantity: l.Quantity})
	}
	if len(dl) == 0 {
		return Document{}, fmt.Errorf("%w: phiếu không có dòng", ErrInvalidInput)
	}
	if err := checkLines(row.Kind, dl); err != nil {
		return Document{}, err
	}
	// Dòng số dư chưa có thì chèn trước (theo cùng thứ tự), rồi mới đổi.
	for _, l := range dl {
		if err := q.EnsureBalance(ctx, store.EnsureBalanceParams{VariantID: l.VariantID, WarehouseID: row.WarehouseID}); err != nil {
			return Document{}, fmt.Errorf("chèn số dư: %w", err)
		}
	}
	for _, l := range dl {
		if err := w.postLine(ctx, tx, row.Kind, row.WarehouseID, id, l, a, moves); err != nil {
			return Document{}, err
		}
	}
	return GetDocument(ctx, tx, id)
}

// postLine áp các thay đổi số dư của một dòng phiếu rồi phát stock.changed cho dòng số dư đó.
func (w Writer) postLine(ctx context.Context, tx pgx.Tx, kind string, warehouseID, docID uuid.UUID, l DocumentLine, a Actor,
	movesOf movesFunc,
) error {
	moves, docType := movesOf(kind, l.Quantity)
	qty := abs(l.Quantity)
	var last balanceRow
	for _, m := range moves {
		r, ok, err := applyDelta(ctx, tx, l.VariantID, warehouseID, m.bucket, m.sign*qty, m.cond,
			doc{Type: docType, ID: docID}, stepPost, a)
		if err != nil {
			return err
		}
		if !ok {
			bs, err := Availability(ctx, tx, warehouseID, []uuid.UUID{l.VariantID})
			if err != nil {
				return err
			}
			avail := bs[0].Available
			if kind == KindDamageExport {
				// Xuất hàng lỗi: phần có thể xuất là hàng lỗi, không quá phần trên kệ (on_hand − in_transit). Bước
				// damaged đã áp (khi đủ) thì cộng lại để ra số trước phiếu.
				damaged := bs[0].Damaged
				if m.cond == condOnShelf {
					damaged += qty
				}
				avail = max(0, min(damaged, bs[0].OnHand-bs[0].InTransit))
			}
			return &InsufficientError{VariantID: l.VariantID, WarehouseID: warehouseID, Requested: qty, Available: avail}
		}
		last = r
	}
	return w.changed(ctx, tx, last)
}

// CancelDocument hủy phiếu nháp id đúng version. Lỗi: ErrNotFound, ErrDocumentNotDraft, ErrVersionConflict.
func CancelDocument(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, a Actor) error {
	if !a.valid() {
		return fmt.Errorf("%w: tác nhân %q", ErrInvalidInput, a.Type)
	}
	q := store.New(tx)
	_, err := q.CancelDocument(ctx, store.CancelDocumentParams{CancelledBy: actorUUID(a), ID: id, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return whyNotDraft(ctx, q, id, version)
	}
	if err != nil {
		return fmt.Errorf("hủy phiếu: %w", err)
	}
	return nil
}

// GetDocument trả phiếu id kèm dòng (theo variant_id). Không có hoặc của tenant khác → ErrNotFound.
func GetDocument(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Document, error) {
	q := store.New(tx)
	r, err := q.GetDocument(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("đọc phiếu: %w", err)
	}
	d := documentOf(store.ListDocumentsRow(r))
	lines, err := q.ListDocumentLines(ctx, id)
	if err != nil {
		return Document{}, fmt.Errorf("đọc dòng phiếu: %w", err)
	}
	d.Lines = make([]DocumentLine, 0, len(lines))
	for _, l := range lines {
		dl := DocumentLine{VariantID: l.VariantID, Quantity: l.Quantity}
		if l.UnitCost.Valid {
			dl.UnitCost = &l.UnitCost.Int64
		}
		d.Lines = append(d.Lines, dl)
	}
	return d, nil
}

// ListDocuments trả phiếu theo f, mới nhất trước (không kèm dòng), và id con trỏ của trang sau (nil: hết).
func ListDocuments(ctx context.Context, tx pgx.Tx, f DocumentFilter) ([]Document, *uuid.UUID, error) {
	if f.Limit < 1 || f.Limit > 200 {
		return nil, nil, fmt.Errorf("%w: limit", ErrInvalidInput)
	}
	ids := f.WarehouseIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}
	rows, err := store.New(tx).ListDocuments(ctx, store.ListDocumentsParams{
		AllWarehouses: f.AllWarehouses, WarehouseIds: ids,
		Status: pgtype.Text{String: f.Status, Valid: f.Status != ""}, Kind: pgtype.Text{String: f.Kind, Valid: f.Kind != ""},
		Cursor: pgUUID(f.Cursor), Lim: f.Limit + 1,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách phiếu: %w", err)
	}
	var next *uuid.UUID
	if len(rows) > int(f.Limit) {
		rows = rows[:f.Limit]
		next = &rows[len(rows)-1].ID
	}
	out := make([]Document, 0, len(rows))
	for _, r := range rows {
		out = append(out, documentOf(r))
	}
	return out, next, nil
}

// lockDraft khóa dòng phiếu id và kiểm còn nháp, đúng version.
func lockDraft(ctx context.Context, q *store.Queries, id uuid.UUID, version int64) (store.LockDocumentRow, error) {
	d, err := q.LockDocument(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return d, ErrNotFound
	case err != nil:
		return d, fmt.Errorf("khóa phiếu: %w", err)
	case d.Status != StatusDraft:
		return d, ErrDocumentNotDraft
	case d.Version != version:
		return d, ErrVersionConflict
	}
	return d, nil
}

// whyNotDraft đọc (khóa) phiếu sau khi câu UPDATE có điều kiện không đổi dòng nào, để trả đúng lỗi.
func whyNotDraft(ctx context.Context, q *store.Queries, id uuid.UUID, version int64) error {
	if _, err := lockDraft(ctx, q, id, version); err != nil {
		return err
	}
	return ErrVersionConflict // đã khóa mà vẫn khớp: không xảy ra; giữ để an toàn
}

// checkLines kiểm dòng theo loại phiếu: tối đa MaxDocumentLines, số lượng (> 0; adjust ≠ 0) trong giới hạn, không trùng
// biến thể, giá nhập trong 0…maxMoney.
func checkLines(kind string, lines []DocumentLine) error {
	if len(lines) > MaxDocumentLines {
		return fmt.Errorf("%w: quá %d dòng", ErrInvalidInput, MaxDocumentLines)
	}
	seen := make([]uuid.UUID, 0, len(lines))
	for i, l := range lines {
		q := l.Quantity
		switch {
		case l.VariantID == uuid.Nil || slices.Contains(seen, l.VariantID):
			return fmt.Errorf("%w: dòng %d: biến thể trống hoặc trùng", ErrInvalidInput, i)
		case q == 0 || q > maxLineQuantity || q < -maxLineQuantity || (q < 0 && kind != KindAdjust):
			return fmt.Errorf("%w: dòng %d: số lượng %d", ErrInvalidInput, i, q)
		case l.UnitCost != nil && (*l.UnitCost < 0 || *l.UnitCost > maxMoney):
			return fmt.Errorf("%w: dòng %d: giá nhập", ErrInvalidInput, i)
		}
		seen = append(seen, l.VariantID)
	}
	return nil
}

func documentOf(r store.ListDocumentsRow) Document {
	d := Document{
		ID: r.ID, WarehouseID: r.WarehouseID, Code: r.Code, Kind: r.Kind, Status: r.Status, SupplierName: r.SupplierName,
		Note: r.Note, CreatedAt: r.CreatedAt, Version: r.Version,
		SupplierID: fromPgUUID(r.SupplierID), CreatedBy: fromPgUUID(r.CreatedBy), PostedBy: fromPgUUID(r.PostedBy),
	}
	if r.PostedAt.Valid {
		t := r.PostedAt.Time.UTC()
		d.PostedAt = &t
	}
	return d
}

// pgUUID: nil → NULL.
func pgUUID(id *uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: deref(id), Valid: id != nil} }

// fromPgUUID: NULL → nil.
func fromPgUUID(u pgtype.UUID) *uuid.UUID {
	if u.Valid {
		return new(uuid.UUID(u.Bytes))
	}
	return nil
}

func deref(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

func actorUUID(a Actor) pgtype.UUID { return pgtype.UUID{Bytes: a.ID, Valid: a.ID != uuid.Nil} }
