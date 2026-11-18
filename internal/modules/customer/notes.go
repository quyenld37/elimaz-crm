package customer

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
)

// maxNoteRunes khớp CHECK customer_notes.body (1–4000) và maxLength của AddCustomerNoteRequest.body.
const maxNoteRunes = 4000

// Note là một ghi chú về khách (chỉ ghi thêm): CustomerID là hồ sơ được mở khi viết (gốc hay hồ sơ con).
type Note struct {
	ID, CustomerID, AuthorID uuid.UUID
	Body                     NoteBody
	CreatedAt                time.Time
}

// AddNote thêm ghi chú body (bỏ khoảng trắng hai đầu; rỗng, quá 4000 ký tự, UTF-8 hỏng hay có ký tự điều khiển ngoài
// xuống dòng, tab → ErrInvalidInput) cho hồ sơ customerID (giữ nguyên khi gộp, hoàn tác), người viết là a (phải có ID; không →
// ErrInvalidInput), lúc now, trong tx của bên gọi. Khách không có (hay của tenant khác) → ErrNotFound. Audit
// customer.note_add chỉ ghi changed_fields ["note"], không ghi nội dung (6.12); báo hiệu customer.changed.
// Ghi chú không sửa, không xóa (trigger của bảng chặn mọi role).
func (w Writer) AddNote(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, body string, a Actor, now time.Time,
) (Note, error) {
	if err := w.check(); err != nil {
		return Note{}, err
	}
	text, err := validNote(body)
	if err != nil {
		return Note{}, err
	}
	if a.ID == uuid.Nil {
		return Note{}, fmt.Errorf("ghi chú cần người viết: %w", ErrInvalidInput)
	}
	// Ghi chú giữ customer_id của hồ sơ được mở (05 mục 1, B5: gộp, hoàn tác không dời ghi chú); dòng thời gian đọc
	// theo cả nhóm. Báo hiệu và audit theo gốc hiện tại.
	root, err := RootOf(ctx, tx, customerID)
	if err != nil {
		return Note{}, err
	}
	r, err := store.New(tx).InsertNote(ctx, store.InsertNoteParams{CustomerID: customerID, Body: text, AuthorID: a.ID, Now: now})
	if err != nil {
		return Note{}, fmt.Errorf("ghi ghi chú khách: %w", err)
	}
	// changed_fields của dòng audit = ["note"]; giá trị chỉ là true (khóa "note" còn bị RecordAudit che).
	if err := w.audit(ctx, tx, r.TenantID, a, "customer.note_add", root, nil, fieldsAudit([]string{"note"})); err != nil {
		return Note{}, err
	}
	if err := w.signal(ctx, tx, r.TenantID, root); err != nil {
		return Note{}, err
	}
	return Note{ID: r.ID, CustomerID: r.CustomerID, AuthorID: r.AuthorID, Body: NoteBody(r.Body), CreatedAt: r.CreatedAt}, nil
}

func validNote(s string) (string, error) {
	v := strings.TrimSpace(s)
	bad := func(r rune) bool { return r != '\n' && r != '\t' && r != '\r' && isControl(r) }
	if n := utf8.RuneCountInString(v); n == 0 || n > maxNoteRunes || !utf8.ValidString(v) || strings.ContainsFunc(v, bad) {
		return "", ErrInvalidInput
	}
	return v, nil
}
