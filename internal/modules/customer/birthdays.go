package customer

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Hồ sơ sinh nhật (kế hoạch M5, Task 468 / M5-56; QĐ40): website gửi form "ngày sinh nhật" sau đơn; promotion đọc qua
// view customer.birthday_profiles_v để phát quà. Ngày, tháng không có năm (không ghi đè customers.birthday).

const (
	maxBirthdayEmail = 200
	stopTokenBytes   = 32
	auditBirthday    = "customer.birthday"
)

// monthDays: số ngày lớn nhất của mỗi tháng (tháng 2 nhận 29: quà năm không nhuận phát ngày 28/2).
var monthDays = [12]int{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31} //nolint:gochecknoglobals // bảng hằng

// ValidBirthday cho biết (day, month) là một ngày có trong năm (29/2 hợp lệ).
func ValidBirthday(day, month int) bool {
	return month >= 1 && month <= 12 && day >= 1 && day <= monthDays[month-1]
}

// BirthdayInput là form đăng ký sinh nhật: SĐT (khóa tìm khách), tên (chỉ dùng khi phải tạo khách mới), ngày, tháng,
// đồng ý nhận tin qua Zalo, email (Mail cần Email). Source rỗng → SourceWeb.
type BirthdayInput struct {
	Phone, FullName string
	Day, Month      int
	Zalo, Mail      bool
	Email           string
	Source          string
}

// BirthdayProfile là hồ sơ sinh nhật đã lưu. Phone, Email là dữ liệu cá nhân (Phone tự che khi log).
type BirthdayProfile struct {
	CustomerID uuid.UUID
	Phone      logging.Phone
	Day, Month int
	Zalo, Mail bool
	Email      logging.Secret
	StoppedAt  *time.Time
	CreatedAt  time.Time
}

// RegisterBirthday lưu sinh nhật của khách có SĐT in.Phone trong tx của bên gọi: SĐT đã là danh tính → khách đó; chưa
// có → tạo khách lead có danh tính SĐT (nguồn của in.Source; hai yêu cầu song song: bên thua dùng hồ sơ bên thắng).
// Đăng ký lần đầu thắng: khách đã có hồ sơ → trả hồ sơ cũ, created false, không đổi gì. Lần đầu ghi audit
// customer.birthday.register (chỉ tên trường). Ngày không có, Mail mà không Email, email sai → ErrInvalidInput; SĐT sai →
// ErrInvalidPhone.
func (w Writer) RegisterBirthday(ctx context.Context, tx pgx.Tx, in BirthdayInput, a Actor, now time.Time,
) (BirthdayProfile, bool, error) {
	if err := w.check(); err != nil {
		return BirthdayProfile{}, false, err
	}
	email, err := birthdayEmail(in)
	if err != nil {
		return BirthdayProfile{}, false, err
	}
	source := in.Source
	if source == "" {
		source = SourceWeb
	}
	if source != SourceWeb && source != SourceImport && source != SourceStaff {
		return BirthdayProfile{}, false, ErrInvalidInput
	}
	phone, err := NormalizePhone(in.Phone)
	if err != nil {
		return BirthdayProfile{}, false, err
	}
	customerID, err := w.customerForPhone(ctx, tx, phone, in.FullName, source, a, now)
	if err != nil {
		return BirthdayProfile{}, false, err
	}
	tok, err := newStopToken()
	if err != nil {
		return BirthdayProfile{}, false, err
	}
	q := store.New(tx)
	rows, err := q.InsertBirthdayProfile(ctx, store.InsertBirthdayProfileParams{
		CustomerID: customerID, Phone: string(phone), Day: int32(in.Day), Month: int32(in.Month), //nolint:gosec // G115: đã kiểm 1–31
		ConsentZalo: in.Zalo, ConsentMail: in.Mail, Email: email, StopToken: tok, Source: source, Now: now,
	})
	if err != nil {
		return BirthdayProfile{}, false, fmt.Errorf("ghi hồ sơ sinh nhật: %w", err)
	}
	if len(rows) == 0 {
		r, err := q.GetBirthdayProfile(ctx, customerID)
		if err != nil {
			return BirthdayProfile{}, false, fmt.Errorf("đọc hồ sơ sinh nhật có sẵn: %w", err)
		}
		return birthdayProfileOf(store.InsertBirthdayProfileRow{
			CustomerID: r.CustomerID, Phone: r.Phone, Day: r.Day, Month: r.Month, ConsentZalo: r.ConsentZalo,
			ConsentMail: r.ConsentMail, Email: r.Email, StoppedAt: r.StoppedAt, CreatedAt: r.CreatedAt,
		}), false, nil
	}
	r := rows[0]
	fields := []string{"day", "month", "phone", "consent_zalo", "consent_mail"}
	if email.Valid {
		fields = append(fields, "email")
	}
	after := fieldsAudit(fields)
	after["source"] = source
	if err := w.auditEntity(ctx, tx, r.TenantID, a, "customer.birthday.register", auditBirthday, customerID, nil,
		after); err != nil {
		return BirthdayProfile{}, false, err
	}
	return birthdayProfileOf(r), true, nil
}

// birthdayEmail kiểm ngày, tháng, email (trống → không có; Mail cần email).
func birthdayEmail(in BirthdayInput) (pgtype.Text, error) {
	if !ValidBirthday(in.Day, in.Month) {
		return pgtype.Text{}, fmt.Errorf("ngày sinh nhật: %w", ErrInvalidInput)
	}
	e := strings.TrimSpace(in.Email)
	if e == "" {
		if in.Mail {
			return pgtype.Text{}, fmt.Errorf("nhận email mà không có email: %w", ErrInvalidInput)
		}
		return pgtype.Text{}, nil
	}
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || len(e) > maxBirthdayEmail {
		return pgtype.Text{}, fmt.Errorf("email: %w", ErrInvalidInput)
	}
	return pgtype.Text{String: e, Valid: true}, nil
}

// customerForPhone trả khách giữ danh tính phone; chưa ai giữ → tạo khách lead tên fullName có danh tính phone.
func (w Writer) customerForPhone(ctx context.Context, tx pgx.Tx, phone logging.Phone, fullName, source string, a Actor,
	now time.Time,
) (uuid.UUID, error) {
	o, found, err := identityOwner(ctx, tx, IdentityPhone, "", string(phone))
	if err != nil {
		return uuid.Nil, err
	}
	if found {
		return o.customerID, nil
	}
	c, taken, err := w.create(ctx, tx, CreateInput{FullName: fullName, Phone: string(phone), Source: source},
		string(LifecycleLead), a, now)
	if err != nil {
		return uuid.Nil, err
	}
	if taken != nil {
		return taken.customerID, nil
	}
	return c.ID, nil
}

func newStopToken() (string, error) {
	b := make([]byte, stopTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sinh mã dừng nhận tin: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func birthdayProfileOf(r store.InsertBirthdayProfileRow) BirthdayProfile {
	p := BirthdayProfile{
		CustomerID: r.CustomerID, Phone: logging.Phone(r.Phone), Day: int(r.Day), Month: int(r.Month),
		Zalo: r.ConsentZalo, Mail: r.ConsentMail, Email: logging.Secret(r.Email), CreatedAt: r.CreatedAt,
	}
	if r.StoppedAt.Valid {
		t := r.StoppedAt.Time
		p.StoppedAt = &t
	}
	return p
}

// BirthdayStop là kết quả dừng nhận tin: khách, lúc dừng đầu tiên, lần gọi này có vừa ghi không.
type BirthdayStop struct {
	CustomerID uuid.UUID
	StoppedAt  time.Time
	First      bool
}

// StopBirthday dừng nhận tin sinh nhật của hồ sơ có mã dừng token (link trong tin quà) lúc now; gọi lại giữ lúc dừng đầu
// tiên (First false). Lần đầu ghi audit customer.birthday.stop. Mã lạ hay rỗng → ErrNotFound.
func (w Writer) StopBirthday(ctx context.Context, tx pgx.Tx, token string, a Actor, now time.Time) (BirthdayStop, error) {
	if token == "" {
		return BirthdayStop{}, ErrNotFound
	}
	q := store.New(tx)
	rows, err := q.StopBirthdayProfile(ctx, store.StopBirthdayProfileParams{
		Now:       pgtype.Timestamptz{Time: now, Valid: true},
		StopToken: token,
	})
	if err != nil {
		return BirthdayStop{}, fmt.Errorf("dừng nhận tin sinh nhật: %w", err)
	}
	if len(rows) == 1 {
		r := rows[0]
		if err := w.auditEntity(ctx, tx, r.TenantID, a, "customer.birthday.stop", auditBirthday, r.CustomerID,
			nil, map[string]any{"stopped_at": true}); err != nil {
			return BirthdayStop{}, err
		}
		return BirthdayStop{CustomerID: r.CustomerID, StoppedAt: r.StoppedAt.Time, First: true}, nil
	}
	r, err := q.BirthdayStoppedAt(ctx, token)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return BirthdayStop{}, ErrNotFound
	case err != nil:
		return BirthdayStop{}, fmt.Errorf("đọc lúc dừng nhận tin: %w", err)
	case !r.StoppedAt.Valid:
		return BirthdayStop{}, fmt.Errorf("hồ sơ chưa dừng sau khi ghi: %w", ErrNotFound)
	}
	return BirthdayStop{CustomerID: r.CustomerID, StoppedAt: r.StoppedAt.Time}, nil
}
