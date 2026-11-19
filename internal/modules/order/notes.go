package order

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// maxNoteRunes khớp CHECK của ord.order_notes.body và maxLength của AddOrderNoteRequest.body.
const maxNoteRunes = 2000

// NoteBody là nội dung ghi chú nội bộ (có thể có SĐT, tên): ghi log luôn bị che.
type NoteBody string

// LogValue che nội dung ghi chú.
func (NoteBody) LogValue() slog.Value { return logging.Address("").LogValue() }

// Note là một ghi chú nội bộ của đơn (chỉ ghi thêm).
type Note struct {
	ID, OrderID, AuthorID uuid.UUID
	Body                  NoteBody
	CreatedAt             time.Time
}

// AddNote thêm ghi chú nội bộ body của người a lúc now vào đơn id của người bán sellerID (ADR-29). Ghi chú nội bộ không
// bao giờ gửi cho hãng (ghi chú cho hãng là CarrierNote); chỉ ghi thêm, không sửa, xóa. Thân rỗng (sau khi bỏ khoảng
// trắng đầu cuối), quá 2.000 ký tự hay a không có người → ErrInvalidInput; không có đơn → ErrNotFound. Audit do bên gọi
// ghi (order không nhập identity), không chép nội dung ghi chú vào audit.
func AddNote(ctx context.Context, tx pgx.Tx, sellerID, id uuid.UUID, body string, a Actor, now time.Time) (Note, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > maxNoteRunes || a.UserID == uuid.Nil {
		return Note{}, fmt.Errorf("ghi chú đơn: %w", ErrInvalidInput)
	}
	if err := orderOfSeller(ctx, tx, sellerID, id); err != nil {
		return Note{}, err
	}
	noteID, err := store.New(tx).InsertOrderNote(ctx, store.InsertOrderNoteParams{
		OrderID: id, Body: body, AuthorID: a.UserID, CreatedAt: now,
	})
	if err != nil {
		return Note{}, fmt.Errorf("ghi ghi chú đơn: %w", err)
	}
	return Note{ID: noteID, OrderID: id, AuthorID: a.UserID, Body: NoteBody(body), CreatedAt: now}, nil
}
