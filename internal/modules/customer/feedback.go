package customer

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

// Góp ý của khách (kế hoạch M5, Task 455 / M5-43): website gửi form /gop-y bằng khóa dịch vụ; nhân viên xem, đổi trạng
// thái, ghi chú nội bộ. Nội dung, tên, liên lạc, ghi chú là dữ liệu cá nhân: kiểu tự che khi log, audit chỉ tên trường.

// Trạng thái của góp ý (cột feedback.state).
const (
	FeedbackNew      = "new"
	FeedbackSeen     = "seen"
	FeedbackAnswered = "answered"
)

// Giới hạn độ dài (ký tự) khớp CHECK của migration 0523 và maxLength trong openapi.yaml.
const (
	maxFeedbackBody     = 2000
	maxFeedbackName     = 80
	maxFeedbackContact  = 120
	maxFeedbackOrderRef = 60
	maxFeedbackPage     = 300
	maxFeedbackDevice   = 120
	maxFeedbackHash     = 128
	maxFeedbackNote     = 1000
	minMood, maxMood    = 1, 5
	auditFeedback       = "customer.feedback"
)

// FeedbackTopics là các chủ đề góp ý (cột feedback.topic), theo form của website.
func FeedbackTopics() []string { return []string{"product", "delivery", "website", "service", "other"} }

func validFeedbackState(s string) bool {
	return s == FeedbackNew || s == FeedbackSeen || s == FeedbackAnswered
}

// FeedbackInput là form góp ý của website. Mood nil: khách không chấm điểm. Chuỗi rỗng: không có trường đó. ClientHash là
// băm địa chỉ của khách do website tính.
type FeedbackInput struct {
	Topic                              string
	Mood                               *int
	Body, Name, Contact                string
	OrderRef, Page, Device, ClientHash string
}

// Feedback là một góp ý đã lưu. CustomerID, CustomerName, CustomerCode: gốc hiện tại của khách được gắn lúc gửi (nil,
// rỗng: không gắn).
type Feedback struct {
	ID                     uuid.UUID
	Topic                  string
	Mood                   *int
	Body                   NoteBody
	Name                   Name
	Contact                logging.Secret
	OrderRef, Page, Device string
	State                  string
	Note                   NoteBody
	CustomerID             *uuid.UUID
	CustomerName           Name
	CustomerCode           string
	CreatedAt, UpdatedAt   time.Time
	UpdatedBy              *uuid.UUID
	Version                int
}

// SubmitFeedback lưu góp ý in lúc now (created_at) trong tx của bên gọi và trả mã. Contact chuẩn hóa được bằng
// NormalizePhone và là danh tính phone của một khách → gắn hồ sơ giữ danh tính đó (như ghi chú: gộp, hoàn tác không dời;
// đọc luôn trả gốc hiện tại, lọc theo cả nhóm); không khớp (SĐT lạ, email, trống) → không
// gắn, không tạo khách, không thêm danh tính (QĐ27: chỉ đơn web tạo khách). Trường sai (chủ đề lạ, điểm ngoài 1–5, nội
// dung rỗng hay quá 2000 ký tự, trường khác quá dài hay có ký tự điều khiển) → ErrInvalidInput. Audit
// customer.feedback.submit chỉ ghi tên trường, chủ đề.
func (w Writer) SubmitFeedback(ctx context.Context, tx pgx.Tx, in FeedbackInput, a Actor, now time.Time) (uuid.UUID, error) {
	if err := w.check(); err != nil {
		return uuid.Nil, err
	}
	p, fields, err := feedbackParams(in)
	if err != nil {
		return uuid.Nil, err
	}
	if phone, err := NormalizePhone(p.Contact.String); p.Contact.Valid && err == nil {
		o, found, err := identityOwner(ctx, tx, IdentityPhone, "", string(phone))
		if err != nil {
			return uuid.Nil, err
		}
		if found {
			p.CustomerID = pgtype.UUID{Bytes: o.customerID, Valid: true}
			fields = append(fields, "customer_id")
		}
	}
	p.Now = now
	r, err := store.New(tx).InsertFeedback(ctx, p)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi góp ý: %w", err)
	}
	after := fieldsAudit(fields)
	after["topic"] = in.Topic
	if err := w.auditEntity(ctx, tx, r.TenantID, a, "customer.feedback.submit", auditFeedback, r.ID, nil, after); err != nil {
		return uuid.Nil, err
	}
	return r.ID, nil
}

// feedbackParams kiểm in và dựng tham số chèn (chưa có khách, giờ) kèm tên các trường có giá trị.
func feedbackParams(in FeedbackInput) (store.InsertFeedbackParams, []string, error) {
	if !slices.Contains(FeedbackTopics(), in.Topic) {
		return store.InsertFeedbackParams{}, nil, fmt.Errorf("chủ đề góp ý: %w", ErrInvalidInput)
	}
	body, err := validMultiline(in.Body, maxFeedbackBody)
	if err != nil {
		return store.InsertFeedbackParams{}, nil, fmt.Errorf("nội dung góp ý: %w", err)
	}
	p := store.InsertFeedbackParams{Topic: in.Topic, Body: body}
	fields := []string{"topic", "body"}
	if in.Mood != nil {
		if *in.Mood < minMood || *in.Mood > maxMood {
			return store.InsertFeedbackParams{}, nil, fmt.Errorf("điểm góp ý: %w", ErrInvalidInput)
		}
		p.Mood = pgtype.Int4{Int32: int32(*in.Mood), Valid: true}
		fields = append(fields, "mood")
	}
	for _, f := range []struct {
		name string
		raw  string
		max  int
		dst  *pgtype.Text
	}{
		{"name", in.Name, maxFeedbackName, &p.Name},
		{"contact", in.Contact, maxFeedbackContact, &p.Contact},
		{"order_ref", in.OrderRef, maxFeedbackOrderRef, &p.OrderRef},
		{"page", in.Page, maxFeedbackPage, &p.Page},
		{"device", in.Device, maxFeedbackDevice, &p.Device},
		{"client_hash", in.ClientHash, maxFeedbackHash, &p.ClientHash},
	} {
		if strings.TrimSpace(f.raw) == "" {
			continue
		}
		v, err := validText(f.raw, f.max)
		if err != nil {
			return store.InsertFeedbackParams{}, nil, fmt.Errorf("trường %s của góp ý: %w", f.name, err)
		}
		*f.dst = pgtype.Text{String: v, Valid: true}
		fields = append(fields, f.name)
	}
	return p, fields, nil
}

// UpdateFeedback đổi trạng thái (state nil: giữ) và ghi chú nội bộ (note nil: giữ; "": xóa; tối đa 1000 ký tự) của góp
// ý id khi version còn khớp, người sửa a, lúc now, trong tx của bên gọi; trả góp ý sau khi sửa. Không trường nào, trạng
// thái lạ, ghi chú sai → ErrInvalidInput; version cũ → ErrVersionConflict; không có (hay của tenant khác) → ErrNotFound.
// Audit customer.feedback.update ghi trạng thái mới và tên trường (không ghi chú).
func (w Writer) UpdateFeedback(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int, state, note *string, a Actor,
	now time.Time,
) (Feedback, error) {
	if err := w.check(); err != nil {
		return Feedback{}, err
	}
	if state == nil && note == nil {
		return Feedback{}, fmt.Errorf("sửa góp ý không có trường nào: %w", ErrInvalidInput)
	}
	p := store.UpdateFeedbackParams{ID: id, Version: int32(version), Now: now, UpdatedBy: a.createdBy()} //nolint:gosec // G115: version nhỏ
	after := map[string]any{}
	if state != nil {
		if !validFeedbackState(*state) {
			return Feedback{}, fmt.Errorf("trạng thái góp ý: %w", ErrInvalidInput)
		}
		p.State = pgtype.Text{String: *state, Valid: true}
		after["state"] = *state
	}
	if note != nil {
		p.SetNote = true
		after["note"] = true
		if strings.TrimSpace(*note) != "" {
			v, err := validMultiline(*note, maxFeedbackNote)
			if err != nil {
				return Feedback{}, fmt.Errorf("ghi chú góp ý: %w", err)
			}
			p.Note = pgtype.Text{String: v, Valid: true}
		}
	}
	q := store.New(tx)
	rows, err := q.UpdateFeedback(ctx, p)
	if err != nil {
		return Feedback{}, fmt.Errorf("sửa góp ý: %w", err)
	}
	if len(rows) == 0 {
		exists, err := q.FeedbackExists(ctx, id)
		switch {
		case err != nil:
			return Feedback{}, fmt.Errorf("đọc góp ý: %w", err)
		case exists:
			return Feedback{}, ErrVersionConflict
		default:
			return Feedback{}, ErrNotFound
		}
	}
	if err := w.auditEntity(ctx, tx, rows[0].TenantID, a, "customer.feedback.update", auditFeedback, id, nil,
		after); err != nil {
		return Feedback{}, err
	}
	out, err := q.ListFeedback(ctx, store.ListFeedbackParams{ID: pgtype.UUID{Bytes: id, Valid: true}, Lim: 1})
	if err != nil || len(out) == 0 {
		return Feedback{}, fmt.Errorf("đọc lại góp ý: %w", errors.Join(err, ErrNotFound))
	}
	return feedbackOf(out[0]), nil
}

// FeedbackFilter là bộ lọc của ListFeedback: State, Topic rỗng là mọi giá trị; CustomerID: góp ý gắn với nhóm của khách
// đó (gốc hay hồ sơ con); After: trang sau; Limit ≤ 0: 50, tối đa MaxListLimit.
type FeedbackFilter struct {
	State, Topic string
	CustomerID   *uuid.UUID
	After        *ListCursor
	Limit        int
}

// ListFeedback trả một trang góp ý của tenant của tx, mới nhất trước, và con trỏ trang sau (nil khi hết). Trạng thái hay
// chủ đề lạ → ErrInvalidInput; khách không có (hay của tenant khác) → ErrNotFound.
func ListFeedback(ctx context.Context, tx pgx.Tx, f FeedbackFilter) ([]Feedback, *ListCursor, error) {
	if (f.State != "" && !validFeedbackState(f.State)) || (f.Topic != "" && !slices.Contains(FeedbackTopics(), f.Topic)) {
		return nil, nil, fmt.Errorf("lọc góp ý: %w", ErrInvalidInput)
	}
	limit := clampLimit(f.Limit, defaultListLimit)
	p := store.ListFeedbackParams{Lim: int32(limit + 1)} //nolint:gosec // đã chặn ở clampLimit
	if f.State != "" {
		p.State = pgtype.Text{String: f.State, Valid: true}
	}
	if f.Topic != "" {
		p.Topic = pgtype.Text{String: f.Topic, Valid: true}
	}
	if f.CustomerID != nil {
		root, err := RootOf(ctx, tx, *f.CustomerID)
		if err != nil {
			return nil, nil, err
		}
		p.RootID = db.NullUUID(&root)
	}
	p.AfterCreated, p.AfterID = f.After.params()
	rows, err := store.New(tx).ListFeedback(ctx, p)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách góp ý: %w", err)
	}
	rows, next := pageOf(rows, limit, func(r store.ListFeedbackRow) ListCursor {
		return ListCursor{CreatedAt: r.CreatedAt, ID: r.ID}
	})
	out := make([]Feedback, 0, len(rows))
	for _, r := range rows {
		out = append(out, feedbackOf(r))
	}
	return out, next, nil
}

// FeedbackCounts là số góp ý của tenant theo trạng thái (đủ ba trạng thái, kể cả 0) và tổng.
type FeedbackCounts struct {
	ByState map[string]int
	Total   int
}

// CountFeedback đếm góp ý của tenant đang chạy theo trạng thái (thanh lọc của màn Góp ý).
func CountFeedback(ctx context.Context, tx pgx.Tx) (FeedbackCounts, error) {
	rows, err := store.New(tx).CountFeedback(ctx)
	if err != nil {
		return FeedbackCounts{}, fmt.Errorf("đếm góp ý: %w", err)
	}
	c := FeedbackCounts{ByState: map[string]int{FeedbackNew: 0, FeedbackSeen: 0, FeedbackAnswered: 0}}
	for _, r := range rows {
		c.ByState[r.State] = int(r.N)
		c.Total += int(r.N)
	}
	return c, nil
}

func feedbackOf(r store.ListFeedbackRow) Feedback {
	f := Feedback{
		ID: r.ID, Topic: r.Topic, Body: NoteBody(r.Body), Name: Name(r.Name.String), Contact: logging.Secret(r.Contact.String),
		OrderRef: r.OrderRef.String, Page: r.Page.String, Device: r.Device.String, State: r.State,
		Note: NoteBody(r.Note.String), CustomerName: Name(r.RootName.String), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		Version: int(r.Version),
	}
	if r.Mood.Valid {
		m := int(r.Mood.Int32)
		f.Mood = &m
	}
	if r.RootID.Valid {
		id := uuid.UUID(r.RootID.Bytes)
		f.CustomerID, f.CustomerCode = &id, r.RootCode.String
	}
	if r.UpdatedBy.Valid {
		id := uuid.UUID(r.UpdatedBy.Bytes)
		f.UpdatedBy = &id
	}
	return f
}

// validMultiline bỏ khoảng trắng hai đầu; phải 1–maxRunes ký tự, UTF-8 hợp lệ, không ký tự điều khiển ngoài xuống dòng,
// tab.
func validMultiline(s string, maxRunes int) (string, error) {
	v := strings.TrimSpace(s)
	bad := func(r rune) bool { return r != '\n' && r != '\t' && r != '\r' && isControl(r) }
	if n := utf8.RuneCountInString(v); n == 0 || n > maxRunes || !utf8.ValidString(v) || strings.ContainsFunc(v, bad) {
		return "", ErrInvalidInput
	}
	return v, nil
}
