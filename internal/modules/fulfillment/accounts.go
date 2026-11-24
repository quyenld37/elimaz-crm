package fulfillment

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Tài khoản hãng của M3 (kế hoạch M3, Task M3-09; spec 05 mục 2): tạo, sửa có version, ghi khóa API (mã hóa bằng
// fieldcrypt, AAD gắn đúng dòng), đổi trạng thái, đọc cho màn hình (AccountView, không bao giờ có khóa) và cho worker
// (ResolveAccount, ActiveAccounts: đã giải mã). Tuyến webhook ghi qua tenancy.PutRoute (platform.put_tenant_route của
// task chung), gọi đầu transaction (ADR-46 nhóm 1) rồi mới tới carrier_accounts (nhóm 8).

// Trạng thái tài khoản hãng (CHECK của migration 0300).
const (
	AccountDraft      = "draft"
	AccountChecking   = "checking"
	AccountActive     = "active"
	AccountAuthFailed = "auth_failed"
	AccountPaused     = "paused"
	AccountRevoked    = "revoked"
)

// Chế độ tài khoản: api gọi API hãng; manual nhân viên nhập mã hãng (hãng self luôn manual).
const (
	ModeAPI    = "api"
	ModeManual = "manual"
	// SelfCarrierKey là khóa hãng của giao tay (shop tự giao), chỉ đi với ModeManual.
	SelfCarrierKey = "self"
)

const (
	envSandbox    = "sandbox"
	envProduction = "production"
	// accountsTable, credentialColumn: AAD của bản mã khóa API.
	accountsTable    = "fulfillment.carrier_accounts"
	credentialColumn = "credential_enc" //nolint:gosec // G101: tên cột của AAD, không phải bí mật
	routeKeyLen      = 32
	maxAccountRef    = 120
	maxWebSort       = 1000
	// accountSignalTopic là chủ đề báo hiệu realtime khi tài khoản hãng đổi; chỉ người có quyền đọc tài khoản nhận.
	accountSignalTopic = "fulfillment.carrier_account"
	permAccountsRead   = "fulfillment.carrier_accounts.read"
	localRefPrefix     = "local:"
	pgUniqueViolation  = "23505"
	pgFKViolation      = "23503"
	routeKeyAlphabet   = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

// Lỗi của tài khoản hãng (đổi sang HTTP ở internal/httpapi/errors.go).
var (
	// ErrCarrierNotSupported: khóa hãng không có adapter trong Registry (mode manual: cũng không có trong danh mục nhập
	// tay), hãng self với mode api, hay tra ở hãng một vận đơn nhập tay.
	ErrCarrierNotSupported = errors.New("fulfillment: hãng không được hỗ trợ")
	// ErrAccountUnavailable: tài khoản không active (worker không tự đổi sang tài khoản khác, VC15).
	ErrAccountUnavailable = errors.New("fulfillment: tài khoản hãng không dùng được")
	// ErrAccountTransition: đổi trạng thái không được phép (active khi chưa có khóa, mọi thay đổi khi đã revoked, kiểm
	// tài khoản manual hay chưa có khóa).
	ErrAccountTransition = errors.New("fulfillment: không đổi được trạng thái tài khoản hãng")
	// ErrAccountTaken: trùng tài khoản mặc định của (người bán, kho, hãng) hay trùng mã tài khoản ở hãng.
	ErrAccountTaken = errors.New("fulfillment: tài khoản hãng đã có")
	// ErrAccountInvalid: dữ liệu sai (mode, môi trường, mã ở hãng dài hay bị đổi, khóa cho tài khoản manual…).
	ErrAccountInvalid = errors.New("fulfillment: tài khoản hãng không hợp lệ")
	// ErrAccountTargetNotFound: người bán hay kho của tài khoản không có (hay của tenant khác).
	ErrAccountTargetNotFound = errors.New("fulfillment: không có người bán hay kho của tài khoản hãng")
	// ErrKeysMissing: tiến trình không có FIELD_ENC_KEYS (Deps.Keys nil) mà cần mã hóa, giải mã khóa API.
	ErrKeysMissing = errors.New("fulfillment: chưa cấu hình khóa mã hóa trường")
	// ErrCredentialsUnreadable: không giải mã được khóa API (bản mã bị sửa, chép từ dòng khác, khóa đã bỏ).
	ErrCredentialsUnreadable = errors.New("fulfillment: không giải mã được khóa API")
)

// AccountInput là tài khoản hãng cần tạo. SellerID nil là lỗi (lớp HTTP điền người bán của shop, catalog.DefaultSeller);
// Credentials chỉ khi Mode = api; ExternalAccountRef rỗng → "local:<id>".
type AccountInput struct {
	SellerID                                    *uuid.UUID
	CarrierKey                                  string
	Mode, Environment, Name, ExternalAccountRef string
	WarehouseID                                 uuid.UUID
	DefaultService                              string
	DefaultOptions                              Options
	IsDefaultForWarehouse, ShowOnWeb            bool
	WebSort                                     int
	Credentials                                 Credentials
}

// AccountPatch là phần sửa của UpdateAccount: trường nil giữ nguyên. ExternalAccountRef chỉ được gửi đúng giá trị cũ
// (mã ở hãng là khóa, không sửa).
type AccountPatch struct {
	Name, Environment, ExternalAccountRef, DefaultService *string
	WarehouseID                                           *uuid.UUID
	DefaultOptions                                        *Options
	IsDefaultForWarehouse, ShowOnWeb                      *bool
	WebSort                                               *int
}

// AccountView là tài khoản hãng cho màn hình: không bao giờ có khóa API (chỉ HasCredentials, NeedsRewrap). WebhookURL
// (/hooks/<hãng>/<route_key>) chỉ lớp HTTP trả cho người có quyền manage.
type AccountView struct {
	ID, SellerID                                uuid.UUID
	CarrierKey, Mode, Environment, Name, Status string
	WarehouseID                                 uuid.UUID
	DefaultService                              string
	DefaultOptions                              Options
	IsDefaultForWarehouse, ShowOnWeb            bool
	WebSort                                     int
	LastCheckAt                                 *time.Time
	LastCheckResult                             string
	HasCredentials, NeedsRewrap                 bool
	WebhookURL                                  string
	Capabilities                                Capabilities
	Version                                     int64
}

// AuditFields là phần trước/sau của dòng audit: không khóa API, không route_key; "credentials" chỉ là dấu có khóa.
func (v AccountView) AuditFields() map[string]any {
	m := map[string]any{
		"carrierKey": v.CarrierKey, "mode": v.Mode, "environment": v.Environment, "name": v.Name, "status": v.Status,
		"warehouseId": v.WarehouseID.String(), "sellerId": v.SellerID.String(), "defaultService": v.DefaultService,
		"defaultOptions": optionsJSON(v.DefaultOptions), "isDefaultForWarehouse": v.IsDefaultForWarehouse,
		"showOnWeb": v.ShowOnWeb, "webSort": v.WebSort, "version": v.Version,
	}
	if v.HasCredentials {
		m["credentials"] = "set"
	}
	return m
}

// AccountFilter lọc ActiveAccounts: kho, cờ hiện trên web, khả năng của adapter (tên JSON của Capabilities, ví dụ
// "quote"); trường rỗng không lọc.
type AccountFilter struct {
	WarehouseID *uuid.UUID
	ShowOnWeb   *bool
	Capability  string
}

// AccountListFilter lọc ListAccounts: kho, trạng thái, con trỏ (id của dòng cuối trang trước), số dòng (1–200).
type AccountListFilter struct {
	WarehouseID *uuid.UUID
	Status      string
	After       *uuid.UUID
	Limit       int
}

// optionsDoc là dạng JSON của Options trong cột default_options.
type optionsDoc struct {
	Inspection  string `json:"inspection,omitempty"`
	FeeOnRefuse int64  `json:"feeOnRefuse,omitempty"`
	Payer       string `json:"payer,omitempty"`
	Pickup      bool   `json:"pickup,omitempty"`
	Note        string `json:"note,omitempty"`
}

func optionsJSON(o Options) optionsDoc {
	return optionsDoc(o)
}

func (d optionsDoc) options() Options {
	return Options(d)
}

// pickupDoc là cột inventory.warehouses_v.pickup_address.
type pickupDoc struct {
	Name         string `json:"name"`
	Line         string `json:"line"`
	ProvinceCode string `json:"province_code"`
	WardCode     string `json:"ward_code"`
}

// CreateAccount tạo tài khoản hãng trong tx của bên gọi: kiểm hãng có adapter (mode manual: hoặc là self, hoặc có trong
// Deps.ManualCarriers; self chỉ manual), ghi tuyến webhook
// route_key (32 ký tự từ crypto/rand) qua tenancy.PutRoute trước tiên, mã hóa Credentials (AAD gắn mã dòng), chèn
// dòng (api: draft; manual: active), báo hiệu. Lỗi: ErrCarrierNotSupported, ErrAccountInvalid, ErrKeysMissing,
// ErrAccountTaken (trùng mặc định hay mã ở hãng), ErrAccountTargetNotFound (người bán, kho không có). Audit do bên gọi ghi.
func (s *Service) CreateAccount(ctx context.Context, tx pgx.Tx, in AccountInput, a Actor) (AccountView, error) {
	if err := s.validateCreate(in); err != nil {
		return AccountView{}, err
	}
	env := in.Environment
	if env == "" {
		env = envSandbox
	}
	id, err := uuid.NewV7()
	if err != nil {
		return AccountView{}, fmt.Errorf("sinh mã tài khoản hãng: %w", err)
	}
	routeKey, err := newRouteKey(rand.Reader)
	if err != nil {
		return AccountView{}, err
	}
	if err := tenancy.PutRoute(ctx, tx, RouteKindCarrierAccount, routeKey); err != nil {
		return AccountView{}, fmt.Errorf("ghi tuyến webhook của tài khoản hãng: %w", err)
	}
	var enc []byte
	var kid pgtype.Text
	if len(in.Credentials) > 0 {
		if enc, kid.String, err = s.seal(in.Credentials, a.TenantID, id); err != nil {
			return AccountView{}, err
		}
		kid.Valid = true
	}
	ref, status := in.ExternalAccountRef, AccountDraft
	if ref == "" {
		ref = localRefPrefix + id.String()
	}
	if in.Mode == ModeManual {
		status = AccountActive
	}
	opts, err := json.Marshal(optionsJSON(in.DefaultOptions))
	if err != nil {
		return AccountView{}, fmt.Errorf("mã hóa tùy chọn mặc định: %w", err)
	}
	if _, err := store.New(tx).InsertAccount(ctx, store.InsertAccountParams{
		ID: id, TenantID: a.TenantID, SellerID: *in.SellerID, CarrierKey: in.CarrierKey, Mode: in.Mode, Environment: env,
		Name: in.Name, ExternalAccountRef: ref, WarehouseID: in.WarehouseID, RouteKey: routeKey,
		DefaultService: in.DefaultService, DefaultOptions: opts, IsDefaultForWarehouse: in.IsDefaultForWarehouse,
		ShowOnWeb: in.ShowOnWeb, WebSort: int32(in.WebSort), CredentialEnc: enc, CredentialKeyID: kid, Status: status, //nolint:gosec // validateCreate giới hạn 0–1000
	}); err != nil {
		return AccountView{}, accountWriteError("chèn tài khoản hãng", err)
	}
	return s.afterWrite(ctx, tx, a.TenantID, id)
}

func (s *Service) validateCreate(in AccountInput) error {
	if in.SellerID == nil || *in.SellerID == uuid.Nil {
		return fmt.Errorf("thiếu người bán: %w", ErrAccountInvalid)
	}
	if in.Mode != ModeAPI && in.Mode != ModeManual {
		return fmt.Errorf("mode %q: %w", in.Mode, ErrAccountInvalid)
	}
	// Tài khoản manual không gọi mạng nên không cần adapter: tự giao (self, Task M3-15) và hãng trong danh mục nhập tay
	// (Deps.ManualCarriers, Task 448) tạo được cả khi bộ hãng rỗng (production); mode api vẫn đòi adapter.
	manualOK := in.Mode == ModeManual && (in.CarrierKey == SelfCarrierKey || slices.Contains(s.d.ManualCarriers, in.CarrierKey))
	if (!s.hasAdapter(in.CarrierKey) && !manualOK) || (in.CarrierKey == SelfCarrierKey && in.Mode != ModeManual) {
		return fmt.Errorf("hãng %q, mode %s: %w", in.CarrierKey, in.Mode, ErrCarrierNotSupported)
	}
	if in.Mode == ModeManual && len(in.Credentials) > 0 {
		return fmt.Errorf("tài khoản manual không có khóa API: %w", ErrAccountInvalid)
	}
	if len(in.Credentials) > 0 && s.d.Keys == nil {
		return ErrKeysMissing
	}
	return validateFields(in.Environment, in.ExternalAccountRef, in.WebSort, true)
}

// validateFields kiểm môi trường (rỗng chỉ khi tạo), độ dài mã ở hãng, thứ tự hiện trên web.
func validateFields(env, ref string, webSort int, create bool) error {
	switch {
	case env != envSandbox && env != envProduction && (env != "" || !create):
		return fmt.Errorf("môi trường %q: %w", env, ErrAccountInvalid)
	case len(ref) > maxAccountRef:
		return fmt.Errorf("mã tài khoản ở hãng quá dài: %w", ErrAccountInvalid)
	case webSort < 0 || webSort > maxWebSort:
		return fmt.Errorf("thứ tự hiện trên web %d: %w", webSort, ErrAccountInvalid)
	}
	return nil
}

func (s *Service) hasAdapter(key string) bool {
	if s.d.Carriers == nil {
		return false
	}
	_, ok := s.d.Carriers.Get(key)
	return ok
}

// UpdateAccount sửa tài khoản id khi version còn khớp (khóa dòng rồi so). Lỗi: ErrAccountNotFound, ErrVersionConflict,
// ErrAccountTransition (đã revoked), ErrAccountInvalid (đổi mã ở hãng, môi trường lạ), ErrAccountTaken,
// ErrAccountTargetNotFound (kho mới không có), ErrLastWebCarrier (tắt show_on_web của tài khoản active cuối cùng hiện
// trên web). Tắt show_on_web của tài khoản đang là ưu tiên trên web thì bỏ ưu tiên cùng transaction (webCarrierHidden).
func (s *Service) UpdateAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID, p AccountPatch, version int64, a Actor,
) (AccountView, error) {
	q := store.New(tx)
	r, err := lockWritable(ctx, q, id, "sửa")
	if err != nil {
		return AccountView{}, err
	}
	switch {
	case r.Version != version:
		return AccountView{}, ErrVersionConflict
	case p.ExternalAccountRef != nil && *p.ExternalAccountRef != r.ExternalAccountRef:
		return AccountView{}, fmt.Errorf("mã tài khoản ở hãng không sửa được: %w", ErrAccountInvalid)
	}
	next := store.UpdateAccountParams{
		ID: id, Name: pick(p.Name, r.Name), Environment: pick(p.Environment, r.Environment),
		WarehouseID: pick(p.WarehouseID, r.WarehouseID), DefaultService: pick(p.DefaultService, r.DefaultService),
		DefaultOptions: r.DefaultOptions, IsDefaultForWarehouse: pick(p.IsDefaultForWarehouse, r.IsDefaultForWarehouse),
		ShowOnWeb: pick(p.ShowOnWeb, r.ShowOnWeb),
	}
	webSort := pick(p.WebSort, int(r.WebSort))
	if err := validateFields(next.Environment, "", webSort, false); err != nil {
		return AccountView{}, err
	}
	next.WebSort = int32(webSort) //nolint:gosec // validateFields giới hạn 0–1000
	if p.DefaultOptions != nil {
		if next.DefaultOptions, err = json.Marshal(optionsJSON(*p.DefaultOptions)); err != nil {
			return AccountView{}, fmt.Errorf("mã hóa tùy chọn mặc định: %w", err)
		}
	}
	if r.ShowOnWeb && !next.ShowOnWeb { // luật hãng trên web (Task 198, bổ sung Task 450)
		if err := s.webCarrierHidden(ctx, tx, q, id, r.Status == AccountActive, a); err != nil {
			return AccountView{}, err
		}
	}
	if _, err := q.UpdateAccount(ctx, next); err != nil {
		return AccountView{}, accountWriteError("sửa tài khoản hãng", err)
	}
	return s.afterWrite(ctx, tx, a.TenantID, id)
}

func pick[T any](p *T, cur T) T { //nolint:ireturn // T là kiểu cụ thể của bên gọi
	if p != nil {
		return *p
	}
	return cur
}

// PutCredentials ghi lại khóa API của tài khoản api id (mã hóa bằng khóa đầu của FIELD_ENC_KEYS), đặt checking (tài
// khoản paused giữ paused) và ghi lệnh kiểm kết nối trong tx của bên gọi. Lỗi: ErrAccountNotFound, ErrAccountTransition (revoked), ErrAccountInvalid
// (manual, bộ khóa rỗng), ErrKeysMissing.
func (s *Service) PutCredentials(ctx context.Context, tx pgx.Tx, id uuid.UUID, creds Credentials, a Actor) (AccountView, error) {
	q := store.New(tx)
	r, err := lockWritable(ctx, q, id, "ghi khóa cho")
	if err != nil {
		return AccountView{}, err
	}
	switch {
	case r.Mode != ModeAPI || len(creds) == 0:
		return AccountView{}, fmt.Errorf("khóa API chỉ cho tài khoản api, không rỗng: %w", ErrAccountInvalid)
	case s.d.Keys == nil:
		return AccountView{}, ErrKeysMissing
	}
	enc, kid, err := s.seal(creds, r.TenantID, id)
	if err != nil {
		return AccountView{}, err
	}
	if _, err := q.SetAccountCredentials(ctx, store.SetAccountCredentialsParams{
		CredentialEnc: enc, CredentialKeyID: pgtype.Text{String: kid, Valid: true}, ID: id,
	}); err != nil {
		return AccountView{}, fmt.Errorf("ghi khóa API: %w", err)
	}
	if _, err := s.enqueueCheck(ctx, tx, r.TenantID, id); err != nil {
		return AccountView{}, err
	}
	return s.afterWrite(ctx, tx, a.TenantID, id)
}

// CheckAccount ghi lệnh kiểm kết nối tài khoản id (không đổi trạng thái: tài khoản active vẫn giao hàng trong lúc
// kiểm); trả mã việc. Lỗi: ErrAccountNotFound; ErrAccountTransition khi tài khoản manual, chưa có khóa hay đã revoked.
func (s *Service) CheckAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID, a Actor) (uuid.UUID, error) {
	r, err := lockAccount(ctx, store.New(tx), id)
	if err != nil {
		return uuid.Nil, err
	}
	if r.Mode != ModeAPI || len(r.CredentialEnc) == 0 || r.Status == AccountRevoked {
		return uuid.Nil, fmt.Errorf("kiểm tài khoản %s/%s: %w", r.Mode, r.Status, ErrAccountTransition)
	}
	return s.enqueueCheck(ctx, tx, a.TenantID, id)
}

func (s *Service) enqueueCheck(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (uuid.UUID, error) {
	n, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("sinh khóa lệnh kiểm: %w", err)
	}
	job, _, err := s.queue().Enqueue(ctx, tx, CheckKind(), queue.NewJob{
		TenantID: &tenantID, Payload: checkPayload{AccountID: id},
		DedupeKey: "check:" + id.String() + ":" + n.String(), OrderKey: "carrier_account:" + id.String(),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi lệnh kiểm kết nối: %w", err)
	}
	return job, nil
}

// SetStatus đổi trạng thái tài khoản id sang to (active | paused | revoked); version khác nil thì phải khớp. Cùng trạng
// thái → không đổi gì. Lỗi: ErrAccountInvalid (to lạ), ErrAccountNotFound, ErrVersionConflict, ErrAccountTransition
// (đã revoked; active khi mode api mà chưa có khóa). Tài khoản không bao giờ bị xóa (vận đơn cũ vẫn đọc được); revoked
// giữ tuyến webhook để route_key không cấp lại cho tenant khác.
func (s *Service) SetStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, to string, version *int64, a Actor) (AccountView, error) {
	if to != AccountActive && to != AccountPaused && to != AccountRevoked {
		return AccountView{}, fmt.Errorf("trạng thái %q: %w", to, ErrAccountInvalid)
	}
	q := store.New(tx)
	r, err := lockAccount(ctx, q, id)
	if err != nil {
		return AccountView{}, err
	}
	switch {
	case version != nil && *version != r.Version:
		return AccountView{}, ErrVersionConflict
	case r.Status == to:
		return s.view(store.GetAccountRow(r)), nil
	case r.Status == AccountRevoked:
		return AccountView{}, fmt.Errorf("đổi tài khoản đã thu hồi: %w", ErrAccountTransition)
	case to == AccountActive && r.Mode == ModeAPI && len(r.CredentialEnc) == 0:
		return AccountView{}, fmt.Errorf("bật tài khoản api chưa có khóa: %w", ErrAccountTransition)
	}
	if _, err := q.SetAccountStatus(ctx, store.SetAccountStatusParams{Status: to, ID: id}); err != nil {
		return AccountView{}, fmt.Errorf("đổi trạng thái tài khoản hãng: %w", err)
	}
	return s.afterWrite(ctx, tx, a.TenantID, id)
}

// GetAccount đọc tài khoản id cho màn hình; không có → ErrAccountNotFound.
func (s *Service) GetAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) (AccountView, error) {
	r, err := getAccount(ctx, store.New(tx), id)
	if err != nil {
		return AccountView{}, err
	}
	return s.view(r), nil
}

// ListAccounts trả một trang tài khoản theo f (id tăng dần) và còn trang sau hay không.
func (s *Service) ListAccounts(ctx context.Context, tx pgx.Tx, f AccountListFilter) ([]AccountView, bool, error) {
	p := store.ListAccountsParams{Lim: int32(f.Limit) + 1} //nolint:gosec // lớp HTTP giới hạn 1–200
	if f.WarehouseID != nil {
		p.WarehouseID = pgtype.UUID{Bytes: *f.WarehouseID, Valid: true}
	}
	if f.Status != "" {
		p.Status = pgtype.Text{String: f.Status, Valid: true}
	}
	if f.After != nil {
		p.After = pgtype.UUID{Bytes: *f.After, Valid: true}
	}
	rows, err := store.New(tx).ListAccounts(ctx, p)
	if err != nil {
		return nil, false, fmt.Errorf("đọc danh sách tài khoản hãng: %w", err)
	}
	more := len(rows) > f.Limit
	if more {
		rows = rows[:f.Limit]
	}
	out := make([]AccountView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.view(store.GetAccountRow(r)))
	}
	return out, more, nil
}

// ResolveAccount trả tài khoản id đã giải mã, kèm địa chỉ lấy hàng của kho (inventory.warehouses_v), cho lệnh gọi hãng
// (worker). Tài khoản không active → ErrAccountUnavailable: không đổi sang tài khoản khác (VC15). Lỗi khác:
// ErrAccountNotFound, ErrKeysMissing, ErrCredentialsUnreadable.
func (s *Service) ResolveAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Account, error) {
	q := store.New(tx)
	r, err := getAccount(ctx, q, id)
	if err != nil {
		return Account{}, err
	}
	if r.Status != AccountActive {
		return Account{}, fmt.Errorf("tài khoản hãng %s đang %s: %w", id, r.Status, ErrAccountUnavailable)
	}
	return s.account(ctx, q, r)
}

// ActiveAccounts trả mọi tài khoản active đã giải mã theo f (xếp web_sort, id); chỉ dùng trong tiến trình (báo phí trên
// web, worker), không trả ra API. Capability lạ → ErrAccountInvalid; tài khoản không có adapter bị bỏ khi lọc khả năng.
func (s *Service) ActiveAccounts(ctx context.Context, tx pgx.Tx, f AccountFilter) ([]Account, error) {
	p := store.ActiveAccountsParams{}
	if f.WarehouseID != nil {
		p.WarehouseID = pgtype.UUID{Bytes: *f.WarehouseID, Valid: true}
	}
	if f.ShowOnWeb != nil {
		p.ShowOnWeb = pgtype.Bool{Bool: *f.ShowOnWeb, Valid: true}
	}
	if f.Capability != "" {
		if _, ok := capabilityByName(Capabilities{}, f.Capability); !ok {
			return nil, fmt.Errorf("khả năng %q: %w", f.Capability, ErrAccountInvalid)
		}
	}
	q := store.New(tx)
	rows, err := q.ActiveAccounts(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("đọc tài khoản hãng active: %w", err)
	}
	out := make([]Account, 0, len(rows))
	for _, r := range rows {
		if f.Capability != "" {
			ad, ok := s.adapter(r.CarrierKey)
			if !ok {
				continue
			}
			if has, _ := capabilityByName(ad.Capabilities(), f.Capability); !has {
				continue
			}
		}
		acc, err := s.account(ctx, q, store.GetAccountRow(r))
		if err != nil {
			return nil, err
		}
		out = append(out, acc)
	}
	return out, nil
}

// capabilityByName đọc khả năng theo tên JSON của hợp đồng (CarrierCapabilities); ok = false khi tên lạ.
func capabilityByName(c Capabilities, name string) (has, ok bool) {
	m := map[string]bool{
		"quote": c.Quote, "create": c.Create, "lookupByRef": c.LookupByRef, "cancel": c.Cancel,
		"cancelIdempotent": c.CancelIdempotent, "track": c.Track, "webhook": c.Webhook, "labels": c.Labels,
		"labelsBatch": c.LabelsBatch, "updateAddress": c.UpdateAddress, "redeliver": c.Redeliver,
		"locations": c.Locations, "statement": c.Statement, "inspection": c.Inspection,
		"declaredValue": c.DeclaredValue, "feeOnRefuse": c.FeeOnRefuse,
	}
	has, ok = m[name]
	return has, ok
}

func (s *Service) adapter(key string) (CarrierAdapter, bool) { //nolint:ireturn // adapter theo khóa hãng
	if s.d.Carriers == nil {
		return nil, false
	}
	return s.d.Carriers.Get(key)
}

// account dựng Account (đã giải mã, kèm địa chỉ lấy hàng của kho) từ dòng r.
func (s *Service) account(ctx context.Context, q *store.Queries, r store.GetAccountRow) (Account, error) {
	creds, err := s.open(r)
	if err != nil {
		return Account{}, err
	}
	acc := Account{
		ID: r.ID, ExternalAccountRef: r.ExternalAccountRef, CarrierKey: r.CarrierKey, Environment: r.Environment,
		RouteKey: r.RouteKey, Creds: creds,
	}
	p, err := q.WarehousePickup(ctx, r.WarehouseID)
	if err != nil {
		return Account{}, fmt.Errorf("đọc địa chỉ lấy hàng của kho: %w", err)
	}
	var d pickupDoc
	if err := json.Unmarshal(p.PickupAddress, &d); err != nil {
		return Account{}, fmt.Errorf("đọc địa chỉ lấy hàng của kho: %w", err)
	}
	acc.Pickup = Address{
		Name: d.Name, Phone: logging.Phone(p.PickupPhone), Line: d.Line, ProvinceCode: d.ProvinceCode, WardCode: d.WardCode,
	}
	return acc, nil
}

// seal mã hóa bộ khóa (JSON) bằng khóa đầu, AAD gắn tenant, bảng, cột, mã dòng.
func (s *Service) seal(c Credentials, tenantID, rowID uuid.UUID) ([]byte, string, error) {
	if s.d.Keys == nil {
		return nil, "", ErrKeysMissing
	}
	plain, err := json.Marshal(map[string]string(c))
	if err != nil {
		return nil, "", fmt.Errorf("mã hóa bộ khóa: %w", err)
	}
	ct, kid, err := s.d.Keys.Seal(plain, credentialAAD(tenantID, rowID))
	clear(plain)
	if err != nil {
		return nil, "", fmt.Errorf("mã hóa khóa API: %w", err)
	}
	return ct, kid, nil
}

// open giải mã khóa API của dòng r (chưa có khóa → nil).
func (s *Service) open(r store.GetAccountRow) (Credentials, error) {
	if len(r.CredentialEnc) == 0 {
		return nil, nil
	}
	if s.d.Keys == nil {
		return nil, ErrKeysMissing
	}
	plain, err := s.d.Keys.Open(r.CredentialEnc, r.CredentialKeyID.String, credentialAAD(r.TenantID, r.ID))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCredentialsUnreadable, err)
	}
	var c map[string]string
	err = json.Unmarshal(plain, &c)
	clear(plain)
	if err != nil {
		return nil, ErrCredentialsUnreadable
	}
	return Credentials(c), nil
}

func credentialAAD(tenantID, rowID uuid.UUID) fieldcrypt.AAD {
	return fieldcrypt.AAD{TenantID: tenantID, Table: accountsTable, Column: credentialColumn, RowID: rowID}
}

// view dựng AccountView (không khóa API) từ dòng r.
func (s *Service) view(r store.GetAccountRow) AccountView {
	v := AccountView{
		ID: r.ID, SellerID: r.SellerID, CarrierKey: r.CarrierKey, Mode: r.Mode, Environment: r.Environment, Name: r.Name,
		Status: r.Status, WarehouseID: r.WarehouseID, DefaultService: r.DefaultService,
		IsDefaultForWarehouse: r.IsDefaultForWarehouse, ShowOnWeb: r.ShowOnWeb, WebSort: int(r.WebSort),
		LastCheckResult: r.LastCheckResult.String, HasCredentials: len(r.CredentialEnc) > 0,
		WebhookURL: "/hooks/" + r.CarrierKey + "/" + r.RouteKey, Version: r.Version,
	}
	var d optionsDoc
	if json.Unmarshal(r.DefaultOptions, &d) == nil {
		v.DefaultOptions = d.options()
	}
	if r.LastCheckAt.Valid {
		t := r.LastCheckAt.Time.UTC()
		v.LastCheckAt = &t
	}
	if v.HasCredentials && s.d.Keys != nil {
		v.NeedsRewrap = s.d.Keys.NeedsRewrap(r.CredentialKeyID.String)
	}
	if ad, ok := s.adapter(r.CarrierKey); ok {
		v.Capabilities = ad.Capabilities()
	}
	return v
}

// afterWrite đọc lại tài khoản vừa ghi và báo hiệu fulfillment.carrier_account trong tx (Bus nil → bỏ qua).
func (s *Service) afterWrite(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (AccountView, error) {
	r, err := getAccount(ctx, store.New(tx), id)
	if err != nil {
		return AccountView{}, err
	}
	if s.d.Bus != nil {
		if err := s.d.Bus.Publish(ctx, tx, realtime.Signal{
			TenantID: tenantID, Topic: accountSignalTopic, Requires: permAccountsRead,
			Entity: realtime.Entity{Type: "carrier_account", ID: id.String()},
		}); err != nil {
			return AccountView{}, fmt.Errorf("báo hiệu tài khoản hãng: %w", err)
		}
	}
	return s.view(r), nil
}

func (s *Service) queue() queue.JobQueue { //nolint:ireturn // Deps.Queue là interface
	if s.d.Queue != nil {
		return s.d.Queue
	}
	return queue.NewQueue()
}

func getAccount(ctx context.Context, q *store.Queries, id uuid.UUID) (store.GetAccountRow, error) {
	r, err := q.GetAccount(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrAccountNotFound
	}
	if err != nil {
		return r, fmt.Errorf("đọc tài khoản hãng: %w", err)
	}
	return r, nil
}

func lockAccount(ctx context.Context, q *store.Queries, id uuid.UUID) (store.LockAccountRow, error) {
	r, err := q.LockAccount(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrAccountNotFound
	}
	if err != nil {
		return r, fmt.Errorf("khóa tài khoản hãng: %w", err)
	}
	return r, nil
}

// lockWritable khóa dòng tài khoản id để sửa: không có → ErrAccountNotFound; đã revoked → ErrAccountTransition.
func lockWritable(ctx context.Context, q *store.Queries, id uuid.UUID, what string) (store.LockAccountRow, error) {
	r, err := lockAccount(ctx, q, id)
	if err == nil && r.Status == AccountRevoked {
		err = fmt.Errorf("%s tài khoản đã thu hồi: %w", what, ErrAccountTransition)
	}
	return r, err
}

// accountWriteError đổi lỗi ràng buộc của câu chèn, sửa: trùng (mặc định của kho, mã ở hãng) → ErrAccountTaken; khóa
// ngoại (người bán, kho) → ErrAccountTargetNotFound.
func accountWriteError(what string, err error) error {
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pe.Code {
		case pgUniqueViolation:
			return fmt.Errorf("%s: %w", what, ErrAccountTaken)
		case pgFKViolation:
			return fmt.Errorf("%s: %w", what, ErrAccountTargetNotFound)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

// newRouteKey sinh khóa tuyến webhook routeKeyLen ký tự [A-Za-z0-9] từ r (crypto/rand), không lệch phân bố (bỏ byte
// ≥ 248 = 4·62).
func newRouteKey(r io.Reader) (string, error) {
	const limit = 256 - 256%len(routeKeyAlphabet)
	out := make([]byte, 0, routeKeyLen)
	buf := make([]byte, routeKeyLen)
	for len(out) < routeKeyLen {
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", fmt.Errorf("sinh khóa tuyến webhook: %w", err)
		}
		for _, b := range buf {
			if int(b) < limit && len(out) < routeKeyLen {
				out = append(out, routeKeyAlphabet[int(b)%len(routeKeyAlphabet)])
			}
		}
	}
	return string(out), nil
}
