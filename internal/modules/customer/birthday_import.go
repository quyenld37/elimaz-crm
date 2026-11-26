package customer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Nhập hồ sơ sinh nhật cũ của website (Task 491; website Task 213): giữ mã dừng nhận tin để link trong tin quà cũ vẫn
// dừng được hồ sơ trong CRM. Task 467 (lệnh nhập dữ liệu website) dùng lại hàm này.

const (
	maxBirthdayImport   = 500
	defaultImportedName = "Khách website"
)

var stopTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,100}$`)

// BirthdayImport là một hồ sơ sinh nhật của website: SĐT (khóa tìm khách), tên (chỉ dùng khi tạo khách mới; rỗng →
// "Khách website"), ngày, tháng, kênh đồng ý, email, mã dừng (giữ nguyên), lúc đăng ký, lúc dừng (nil: chưa dừng), cờ
// khách đã có đơn web giao thành công trên website cũ. Phone, Email, StopToken là dữ liệu cá nhân.
type BirthdayImport struct {
	Phone, FullName string
	Day, Month      int
	Zalo, Mail      bool
	Email           string
	StopToken       string
	SavedAt         time.Time
	StoppedAt       *time.Time
	LegacyDelivered bool
}

// BirthdayImportReport là kết quả một lần nhập: hồ sơ chèn mới, hồ sơ đã có cùng mã dừng (nhập lại), số hồ sơ đang dừng
// do lần nhập này, vị trí mục (từ 0, tăng dần) không giữ được mã dừng (khách đã có hồ sơ khác mã, hay mã thuộc khách
// khác).
type BirthdayImportReport struct {
	Inserted, Skipped, Stopped int
	Conflicts                  []int
}

// importEntry là một mục đã kiểm: vị trí gốc, SĐT chuẩn hóa, email.
type importEntry struct {
	BirthdayImport

	pos   int
	phone logging.Phone
	email pgtype.Text
}

// ImportBirthdays nhập 1–500 hồ sơ sinh nhật của website trong tx của bên gọi, theo SĐT tăng dần (ADR-46 nhóm 5: khách
// → birthday_profiles; hai lần nhập trùng SĐT không khóa ngược). Mỗi mục: tìm hay tạo khách theo SĐT (nguồn import);
// khách chưa có hồ sơ → hồ sơ nguồn import giữ mã dừng, lúc đăng ký, lúc dừng, cờ đơn cũ (audit
// customer.birthday.import, chỉ tên trường); đã có cùng mã → Skipped; khác mã hay mã thuộc khách khác → Conflicts, hồ sơ
// giữ nguyên trừ việc website ghi đã dừng thì dừng hồ sơ của khách đó (audit customer.birthday.stop). Đầu vào sai (số
// mục, SĐT hay mã dừng trùng, mã dừng sai mẫu, thiếu lúc đăng ký, ngày, email) → ErrInvalidInput; SĐT sai →
// ErrInvalidPhone; bên gọi lùi transaction.
func (w Writer) ImportBirthdays(ctx context.Context, tx pgx.Tx, items []BirthdayImport, a Actor, now time.Time,
) (BirthdayImportReport, error) {
	if err := w.check(); err != nil {
		return BirthdayImportReport{}, err
	}
	entries, err := importEntries(items)
	if err != nil {
		return BirthdayImportReport{}, err
	}
	rep := BirthdayImportReport{Conflicts: []int{}}
	for _, e := range entries {
		if err := w.importBirthday(ctx, tx, e, a, now, &rep); err != nil {
			return BirthdayImportReport{}, err
		}
	}
	slices.Sort(rep.Conflicts)
	return rep, nil
}

// importEntries kiểm items và trả mục đã chuẩn hóa, sắp theo SĐT.
func importEntries(items []BirthdayImport) ([]importEntry, error) {
	if len(items) < 1 || len(items) > maxBirthdayImport {
		return nil, fmt.Errorf("số hồ sơ sinh nhật nhập: %w", ErrInvalidInput)
	}
	out := make([]importEntry, 0, len(items))
	phones, tokens := make(map[string]bool, len(items)), make(map[string]bool, len(items))
	for i, it := range items {
		email, err := birthdayEmail(BirthdayInput{Day: it.Day, Month: it.Month, Mail: it.Mail, Email: it.Email})
		if err != nil {
			return nil, err
		}
		p, err := NormalizePhone(it.Phone)
		if err != nil {
			return nil, err
		}
		switch {
		case !stopTokenPattern.MatchString(it.StopToken) || tokens[it.StopToken]:
			return nil, fmt.Errorf("mã dừng nhận tin nhập sai mẫu hay trùng: %w", ErrInvalidInput)
		case phones[string(p)]:
			return nil, fmt.Errorf("SĐT trùng trong lần nhập sinh nhật: %w", ErrInvalidInput)
		case it.SavedAt.IsZero() || (it.StoppedAt != nil && it.StoppedAt.IsZero()):
			return nil, fmt.Errorf("lúc đăng ký, lúc dừng của hồ sơ nhập: %w", ErrInvalidInput)
		}
		phones[string(p)], tokens[it.StopToken] = true, true
		e := importEntry{BirthdayImport: it, pos: i, phone: p, email: email}
		if strings.TrimSpace(e.FullName) == "" {
			e.FullName = defaultImportedName
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b importEntry) int { return cmp.Compare(string(a.phone), string(b.phone)) })
	return out, nil
}

// importBirthday nhập một mục và cộng vào rep.
func (w Writer) importBirthday(ctx context.Context, tx pgx.Tx, e importEntry, a Actor, now time.Time,
	rep *BirthdayImportReport,
) error {
	customerID, err := w.customerForPhone(ctx, tx, e.phone, e.FullName, SourceImport, a, now)
	if err != nil {
		return err
	}
	var stopped pgtype.Timestamptz
	if e.StoppedAt != nil {
		stopped = pgtype.Timestamptz{Time: *e.StoppedAt, Valid: true}
	}
	q := store.New(tx)
	rows, err := q.ImportBirthdayProfile(ctx, store.ImportBirthdayProfileParams{
		CustomerID: customerID, Phone: string(e.phone), Day: int32(e.Day), Month: int32(e.Month), //nolint:gosec // G115: đã kiểm 1–31
		ConsentZalo: e.Zalo, ConsentMail: e.Mail, Email: e.email, StopToken: e.StopToken, StoppedAt: stopped,
		LegacyDelivered: e.LegacyDelivered, SavedAt: e.SavedAt, Now: now,
	})
	if err != nil {
		return fmt.Errorf("ghi hồ sơ sinh nhật nhập: %w", err)
	}
	if len(rows) == 1 {
		rep.Inserted++
		if stopped.Valid {
			rep.Stopped++
		}
		fields := []string{"day", "month", "phone", "consent_zalo", "consent_mail", "stop_token", "created_at"}
		if e.email.Valid {
			fields = append(fields, "email")
		}
		if stopped.Valid {
			fields = append(fields, "stopped_at")
		}
		after := fieldsAudit(fields)
		after["source"], after["legacy_delivered"] = SourceImport, e.LegacyDelivered
		return w.auditEntity(ctx, tx, rows[0].TenantID, a, "customer.birthday.import", auditBirthday, customerID, nil,
			after)
	}
	have, err := q.BirthdayProfileToken(ctx, customerID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		rep.Conflicts = append(rep.Conflicts, e.pos) // mã dừng đã thuộc hồ sơ của khách khác
		return nil
	case err != nil:
		return fmt.Errorf("đọc hồ sơ sinh nhật có sẵn: %w", err)
	case have.StopToken == e.StopToken:
		rep.Skipped++
	default:
		rep.Conflicts = append(rep.Conflicts, e.pos)
	}
	if !stopped.Valid || have.StoppedAt.Valid {
		return nil
	}
	return w.stopImported(ctx, tx, customerID, stopped, a, now, rep)
}

// stopImported dừng nhận tin hồ sơ của khách customerID lúc at (website ghi khách đã dừng).
func (w Writer) stopImported(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, at pgtype.Timestamptz, a Actor,
	now time.Time, rep *BirthdayImportReport,
) error {
	rows, err := store.New(tx).StopBirthdayProfileOf(ctx, store.StopBirthdayProfileOfParams{
		At: at, Now: now, CustomerID: customerID,
	})
	if err != nil {
		return fmt.Errorf("dừng nhận tin hồ sơ sinh nhật nhập: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	rep.Stopped++
	return w.auditEntity(ctx, tx, rows[0].TenantID, a, "customer.birthday.stop", auditBirthday, customerID, nil,
		map[string]any{"stopped_at": true, "source": SourceImport})
}
