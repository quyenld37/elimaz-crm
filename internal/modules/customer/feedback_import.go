package customer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
)

// ImportedFeedback là một góp ý cũ của website (Task 467): form như lúc khách gửi (Input, không có ClientHash) cùng mã,
// trạng thái, ghi chú nội bộ (rỗng: không có), giờ tạo, giờ sửa giữ nguyên.
type ImportedFeedback struct {
	ID                   uuid.UUID
	State, Note          string
	CreatedAt, UpdatedAt time.Time
	Input                FeedbackInput
}

// ImportFeedback ghi góp ý in trong tx của bên gọi, cùng luật trường và cách gắn khách với SubmitFeedback (liên lạc là
// SĐT đang là danh tính phone của một khách → gắn hồ sơ đó; không tạo khách). Trả created false khi id đã có ở tenant
// này (chạy lại 0 thay đổi). id rỗng, giờ rỗng, trạng thái lạ, ghi chú sai, trường sai → ErrInvalidInput; id của tenant
// khác (id là khóa chính toàn bảng) → ErrInvalidInput kèm "mã góp ý đã được dùng" (lỗi chỉ của lệnh nhập). Audit customer.feedback.import chỉ tên trường, chủ đề, trạng thái (now chỉ để chữ ký
// thống nhất với SubmitFeedback, giờ ghi là giờ của website).
func (w Writer) ImportFeedback(ctx context.Context, tx pgx.Tx, in ImportedFeedback, a Actor, _ time.Time) (bool, error) {
	if err := w.check(); err != nil {
		return false, err
	}
	if in.ID == uuid.Nil || in.CreatedAt.IsZero() || in.UpdatedAt.IsZero() || !validFeedbackState(in.State) {
		return false, fmt.Errorf("mã, giờ, trạng thái của góp ý nhập: %w", ErrInvalidInput)
	}
	q := store.New(tx)
	exists, err := q.FeedbackExists(ctx, in.ID)
	if err != nil {
		return false, fmt.Errorf("kiểm góp ý có sẵn: %w", err)
	}
	if exists {
		return false, nil
	}
	if in.Input.ClientHash != "" {
		return false, fmt.Errorf("góp ý nhập không mang băm địa chỉ: %w", ErrInvalidInput)
	}
	p, fields, err := feedbackParams(in.Input)
	if err != nil {
		return false, err
	}
	var note pgtype.Text
	if strings.TrimSpace(in.Note) != "" {
		v, err := validMultiline(in.Note, maxFeedbackNote)
		if err != nil {
			return false, fmt.Errorf("ghi chú góp ý: %w", err)
		}
		note, fields = pgtype.Text{String: v, Valid: true}, append(fields, "note")
	}
	if phone, err := NormalizePhone(p.Contact.String); p.Contact.Valid && err == nil {
		o, found, err := identityOwner(ctx, tx, IdentityPhone, "", string(phone))
		if err != nil {
			return false, err
		}
		if found {
			p.CustomerID = pgtype.UUID{Bytes: o.customerID, Valid: true}
			fields = append(fields, "customer_id")
		}
	}
	r, err := q.ImportFeedback(ctx, store.ImportFeedbackParams{
		ID: in.ID, Topic: p.Topic, Mood: p.Mood, Body: p.Body, Name: p.Name, Contact: p.Contact, OrderRef: p.OrderRef,
		Page: p.Page, Device: p.Device, CustomerID: p.CustomerID, State: in.State, Note: note,
		CreatedAt: in.CreatedAt, UpdatedAt: in.UpdatedAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("mã góp ý đã được dùng ở tenant khác: %w", ErrInvalidInput)
	}
	if err != nil {
		return false, fmt.Errorf("ghi góp ý nhập: %w", err)
	}
	after := fieldsAudit(fields)
	after["topic"], after["state"] = in.Input.Topic, in.State
	if err := w.auditEntity(ctx, tx, r.TenantID, a, "customer.feedback.import", auditFeedback, r.ID, nil, after); err != nil {
		return false, err
	}
	return true, nil
}
