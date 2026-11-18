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
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Sự kiện và báo hiệu của customer (docs/views.md mục "Sự kiện công bố"); payload chỉ mang mã và số.
const (
	// TopicCustomerCreated phát khi tạo khách (aggregate customer, Seq = version của hồ sơ mới).
	TopicCustomerCreated = "customer.created"

	aggregateCustomer = "customer"
	signalTopic       = "customer.changed"
	permRead          = identity.Permission("customer.read")
)

// Người làm của Actor.Type (khớp CHECK identity.audit_logs.actor_type).
const (
	ActorStaff   = "staff"
	ActorSystem  = "system"
	ActorService = "service"
)

// Nguồn của danh tính (cột customer_identities.source; Q29).
const (
	SourceOrder  = "order"
	SourceChat   = "chat"
	SourceStaff  = "staff"
	SourceWeb    = "web"
	SourceImport = "import"
)

// Created là payload của customer.created.
type Created struct {
	CustomerID uuid.UUID `json:"customerId"`
	Version    int64     `json:"version"`
}

// Writer là đường ghi của customer (mẫu catalog.Writer, Q-T57-1): mỗi phương thức ghi trong tx của bên gọi
// (tenancy.WithTenant) kèm sự kiện (Events.Emit qua Queue), báo hiệu customer.changed (Realtime nil → bỏ qua: báo hiệu
// được phép mất) và dòng audit (chỉ tên trường, không giá trị cá nhân).
type Writer struct {
	Events   *events.Registry
	Queue    queue.JobQueue
	Realtime realtime.Bus
}

var (
	errWriterNotWired = errors.New("customer: Writer thiếu Events hoặc Queue")
	// errPhoneTaken chỉ dùng để lùi savepoint của lần tạo khi SĐT đã có chủ; không ra khỏi gói.
	errPhoneTaken = errors.New("customer: SĐT đã có chủ")
	errRootChurn  = errors.New("customer: gốc của hồ sơ đổi liên tục")
)

// check: Writer phải có sổ consumer và hàng đợi (Emit cần cả hai); Realtime được phép nil.
func (w Writer) check() error {
	if w.Events != nil && w.Queue != nil {
		return nil
	}
	return errWriterNotWired
}

// Actor là người làm một thao tác ghi: Type là ActorStaff, ActorSystem hay ActorService; ID uuid.Nil → không ghi
// created_by, actor_id. RequestID, IP chỉ vào audit.
type Actor struct {
	Type          string
	ID            uuid.UUID
	RequestID, IP string
}

func (a Actor) createdBy() pgtype.UUID {
	if a.ID == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: a.ID, Valid: true}
}

// Stats là chỉ số mua hàng của một hồ sơ gốc (chỉ đúng ở gốc). ReturnRateBP tính khi đọc: hoàn × 10000 / (giao xong +
// hoàn), 0 khi chưa có đơn kết thúc.
type Stats struct {
	OrderCount, DeliveredCount, ReturnedCount int
	TotalSpent                                int64
	LastOrderAt                               *time.Time
	ReturnRateBP                              int
}

// Customer là một hồ sơ khách. Lifecycle là "lead" hay "customer".
type Customer struct {
	ID, RootID uuid.UUID
	Code       string
	FullName   Name
	Lifecycle  string
	Birthday   *Birthday
	Stats      Stats
	Version    int64
	CreatedAt  time.Time
}

// CreateInput là dữ liệu tạo khách. Phone rỗng → không danh tính. Source là nguồn của danh tính phone (rỗng →
// SourceStaff).
type CreateInput struct {
	FullName string
	Phone    string
	Birthday *time.Time
	Address  *AddressInput
	Source   string
}

// UpdateInput là phần sửa hồ sơ: trường nil giữ nguyên; ClearBirthday xóa ngày sinh.
type UpdateInput struct {
	FullName      *string
	Birthday      *time.Time
	ClearBirthday bool
}

// owner là hồ sơ đang giữ một danh tính.
type owner struct{ customerID, rootID uuid.UUID }

// CreateCustomer tạo khách mới (lifecycle lead, root_id = id, code KH… từ sequence, name_search = NormalizeName) trong
// tx của bên gọi; có Phone thì chèn danh tính phone (đã thuộc hồ sơ khác → *IdentityTakenError, không để lại gì); có
// Address thì chèn địa chỉ mặc định (kiểm bằng ValidateUnits). Ghi audit customer.create (tên trường), sự kiện
// customer.created (Seq = version), báo hiệu customer.changed.
func (w Writer) CreateCustomer(ctx context.Context, tx pgx.Tx, in CreateInput, a Actor, now time.Time) (Customer, error) {
	c, taken, err := w.create(ctx, tx, in, string(LifecycleLead), a, now)
	if err != nil {
		return Customer{}, err
	}
	if taken != nil {
		return Customer{}, &IdentityTakenError{OwnerRootID: taken.rootID}
	}
	return c, nil
}

// create là CreateCustomer với lifecycle cho trước. SĐT đã có chủ → trả chủ (taken), savepoint đã lùi: không còn hồ
// sơ, địa chỉ, sự kiện hay audit của lần này.
func (w Writer) create(ctx context.Context, tx pgx.Tx, in CreateInput, lifecycle string, a Actor, now time.Time,
) (c Customer, taken *owner, err error) {
	if err := w.check(); err != nil {
		return Customer{}, nil, err
	}
	name, err := validName(in.FullName)
	if err != nil {
		return Customer{}, nil, err
	}
	source := in.Source
	if source == "" {
		source = SourceStaff
	}
	if !validSource(source) {
		return Customer{}, nil, ErrInvalidInput
	}
	var phone logging.Phone
	if in.Phone != "" {
		if phone, err = NormalizePhone(in.Phone); err != nil {
			return Customer{}, nil, err
		}
	}
	var addr *AddressInput
	if in.Address != nil {
		v, err := validAddress(ctx, tx, *in.Address)
		if err != nil {
			return Customer{}, nil, err
		}
		addr = &v
	}
	// Savepoint (db.Savepoint): SĐT bị chiếm (kể cả bởi một yêu cầu song song vừa commit) thì lùi đúng phần của lần
	// tạo này; tx của bên gọi dùng tiếp được.
	err = db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		var spErr error
		c, taken, spErr = w.createIn(ctx, sp, name, phone, source, addr, in.Birthday, lifecycle, a, now)
		if spErr == nil && taken != nil {
			return errPhoneTaken
		}
		return spErr
	})
	switch {
	case errors.Is(err, errPhoneTaken):
		return Customer{}, taken, nil
	case err != nil:
		return Customer{}, nil, err //nolint:wrapcheck // lỗi của createIn đã bọc; lỗi savepoint do db bọc
	}
	return c, nil, nil
}

func (w Writer) createIn(ctx context.Context, tx pgx.Tx, name string, phone logging.Phone, source string,
	addr *AddressInput, birthday *time.Time, lifecycle string, a Actor, now time.Time,
) (Customer, *owner, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Customer{}, nil, fmt.Errorf("sinh mã khách: %w", err)
	}
	q := store.New(tx)
	row, err := q.InsertCustomerProfile(ctx, store.InsertCustomerProfileParams{
		ID: id, FullName: name, NameSearch: NormalizeName(name), Lifecycle: lifecycle, Birthday: dateOf(birthday),
		CreatedBy: a.createdBy(), Now: now,
	})
	if err != nil {
		return Customer{}, nil, fmt.Errorf("ghi khách: %w", err)
	}
	fields := []string{"full_name"}
	if phone != "" {
		_, _, taken, err := insertIdentity(ctx, tx, id, IdentityPhone, "", string(phone), source, a)
		if err != nil || taken != nil {
			return Customer{}, taken, err
		}
		fields = append(fields, "phone")
	}
	if birthday != nil {
		fields = append(fields, "birthday")
	}
	if addr != nil {
		if _, err := q.InsertAddress(ctx, insertAddressParams(id, *addr, true, now)); err != nil {
			return Customer{}, nil, fmt.Errorf("ghi địa chỉ của khách mới: %w", err)
		}
		fields = append(fields, "address")
	}
	c := Customer{
		ID: id, RootID: row.RootID, Code: row.Code, FullName: Name(name), Lifecycle: lifecycle,
		Birthday: birthdayOf(dateOf(birthday)), Version: row.Version, CreatedAt: row.CreatedAt,
	}
	after := fieldsAudit(fields)
	after["code"], after["lifecycle"], after["version"] = c.Code, lifecycle, c.Version
	if err := w.audit(ctx, tx, row.TenantID, a, "customer.create", id, nil, after); err != nil {
		return Customer{}, nil, err
	}
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: row.TenantID, AggregateType: aggregateCustomer, AggregateID: id.String(), Seq: c.Version,
		Topic: TopicCustomerCreated, Payload: Created{CustomerID: id, Version: c.Version},
	}); err != nil {
		return Customer{}, nil, fmt.Errorf("phát %s: %w", TopicCustomerCreated, err)
	}
	if err := w.signal(ctx, tx, row.TenantID, c.RootID); err != nil {
		return Customer{}, nil, err
	}
	return c, nil, nil
}

// Update sửa hồ sơ id khi version còn khớp (khóa dòng rồi so; lệch → ErrVersionConflict, không có → ErrNotFound).
// Đổi tên thì đổi name_search. Trả hồ sơ sau khi sửa (version mới).
func (w Writer) Update(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, p UpdateInput, a Actor,
	now time.Time,
) (Customer, error) {
	if err := w.check(); err != nil {
		return Customer{}, err
	}
	q := store.New(tx)
	cur, err := q.LockCustomer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Customer{}, ErrNotFound
	}
	if err != nil {
		return Customer{}, fmt.Errorf("khóa khách: %w", err)
	}
	if cur.Version != version {
		return Customer{}, ErrVersionConflict
	}
	name, birthday := cur.FullName, cur.Birthday
	var fields []string
	if p.FullName != nil {
		n, err := validName(*p.FullName)
		if err != nil {
			return Customer{}, err
		}
		if n != name {
			name = n
			fields = append(fields, "full_name")
		}
	}
	switch {
	case p.ClearBirthday && p.Birthday != nil:
		return Customer{}, ErrInvalidInput
	case p.ClearBirthday:
		if birthday.Valid {
			fields = append(fields, "birthday")
		}
		birthday = pgtype.Date{}
	case p.Birthday != nil:
		if nb := dateOf(p.Birthday); nb != birthday {
			birthday = nb
			fields = append(fields, "birthday")
		}
	}
	newVersion, err := q.UpdateCustomerProfile(ctx, store.UpdateCustomerProfileParams{
		FullName: name, NameSearch: NormalizeName(name), Birthday: birthday, Now: now, ID: id, Version: version,
	})
	if errors.Is(err, pgx.ErrNoRows) { // không xảy ra khi đã khóa dòng; giữ cho chắc
		return Customer{}, ErrVersionConflict
	}
	if err != nil {
		return Customer{}, fmt.Errorf("sửa khách: %w", err)
	}
	after := fieldsAudit(fields)
	after["version"] = newVersion
	if err := w.audit(ctx, tx, cur.TenantID, a, "customer.update", id, map[string]any{"version": version}, after); err != nil {
		return Customer{}, err
	}
	if err := w.signal(ctx, tx, cur.TenantID, cur.RootID); err != nil {
		return Customer{}, err
	}
	return getOne(ctx, tx, id)
}

// lockOwner khóa dòng khách id (đầu nhóm 5 của ADR-46) trước khi đổi danh tính, địa chỉ của nó.
func lockOwner(ctx context.Context, tx pgx.Tx, id uuid.UUID) (store.LockCustomerRow, error) {
	row, err := store.New(tx).LockCustomer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.LockCustomerRow{}, ErrNotFound
	}
	if err != nil {
		return store.LockCustomerRow{}, fmt.Errorf("khóa khách: %w", err)
	}
	return row, nil
}

// signal báo customer.changed cho hồ sơ gốc rootID (không mang dữ liệu cá nhân).
func (w Writer) signal(ctx context.Context, tx pgx.Tx, tenantID, rootID uuid.UUID) error {
	if w.Realtime == nil {
		return nil
	}
	if err := w.Realtime.Publish(ctx, tx, realtime.Signal{
		TenantID: tenantID, Topic: signalTopic, Requires: permRead,
		Entity: realtime.Entity{Type: aggregateCustomer, ID: rootID.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu %s: %w", signalTopic, err)
	}
	return nil
}

// audit ghi một dòng audit; before, after chỉ mang tên trường đã đổi (giá trị true), mã và số.
func (w Writer) audit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, a Actor, action string, id uuid.UUID,
	before, after map[string]any,
) error {
	return w.auditEntity(ctx, tx, tenantID, a, action, aggregateCustomer, id, before, after)
}

// auditEntity là audit với loại đối tượng cho trước (customer, customer_merge).
func (w Writer) auditEntity(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, a Actor, action, entityType string,
	id uuid.UUID, before, after map[string]any,
) error {
	actorType, actorID := a.Type, ""
	if actorType == "" {
		actorType = ActorStaff
	}
	if a.ID != uuid.Nil {
		actorID = a.ID.String()
	}
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: tenantID, ActorType: actorType, ActorID: actorID, Action: action, EntityType: entityType,
		EntityID: id.String(), Before: before, After: after, IP: a.IP, RequestID: a.RequestID,
	}); err != nil {
		return fmt.Errorf("ghi audit %s: %w", action, err)
	}
	return nil
}

// fieldsAudit: mỗi tên trường đã đổi → true (không giá trị, 6.12).
func fieldsAudit(fields []string) map[string]any {
	m := make(map[string]any, len(fields)+3)
	for _, f := range fields {
		m[f] = true
	}
	return m
}

// validName bỏ khoảng trắng hai đầu; phải 1–120 ký tự, UTF-8 hợp lệ, không ký tự điều khiển.
func validName(s string) (string, error) {
	return validText(s, maxNameRunes)
}

func validText(s string, maxRunes int) (string, error) {
	v := strings.TrimSpace(s)
	if n := utf8.RuneCountInString(v); n == 0 || n > maxRunes || !utf8.ValidString(v) || strings.ContainsFunc(v, isControl) {
		return "", ErrInvalidInput
	}
	return v, nil
}

func validSource(s string) bool {
	return slices.Contains([]string{SourceOrder, SourceChat, SourceStaff, SourceWeb, SourceImport}, s)
}

func dateOf(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	y, m, d := t.Date()
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

func birthdayOf(d pgtype.Date) *Birthday {
	if !d.Valid {
		return nil
	}
	b := Birthday(d.Time)
	return &b
}
