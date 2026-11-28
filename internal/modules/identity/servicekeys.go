package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/identity/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Khóa dịch vụ của website (kế hoạch M5 Task 189, ADR-18): website gọi các route /api/v1/web/* bằng header
// Authorization: Bearer ek_<tiền tố>_<bí mật>. CRM chỉ lưu SHA-256 của khóa đầy đủ; tiền tố (không bí mật) định tuyến
// khóa → tenant qua platform.tenant_routes (kind service_key). Khóa chỉ mang quyền web.* (ServiceKeyPermissions).

const (
	// RouteKindServiceKey là loại khóa của platform.tenant_routes cho tiền tố khóa dịch vụ.
	RouteKindServiceKey = "service_key"
	// TopicServiceKeyRevoked là chủ đề báo hiệu hệ thống (realtime, System: true) khi một khóa bị thu hồi; Entity là
	// {"service_key", <tiền tố>}. Lớp HTTP phát trong transaction thu hồi, bộ kiểm khóa xóa đệm khi nhận.
	TopicServiceKeyRevoked = "identity.service_key_revoked"

	serviceKeyScheme = "ek_"
	// prefixBytes: 8 byte ngẫu nhiên → 13 ký tự base32, giữ 12 (60 bit). secretBytes: 32 byte → 52 ký tự base32.
	prefixBytes      = 8
	prefixLen        = 12
	secretBytes      = 32
	secretLen        = 52
	maxKeyName       = 80
	maxKeyCIDRs      = 32
	keyCreateRetries = 2
	auditServiceKey  = "identity.service_key"
)

// ServiceKeyPermissions là các quyền chỉ khóa dịch vụ giữ được (không nằm trong Permissions của nhân viên, không vai
// trò nào nhận, đều ScopeNone). Mốc sau thêm quyền web.* của mình vào đây và vào docs/quyen.md.
var ServiceKeyPermissions = []Permission{ //nolint:gochecknoglobals // danh sách hằng, không sửa lúc chạy
	"web.catalog.read",
	"web.orders.create",
	"web.orders.read",
	"web.orders.cancel",
	"web.requests.write",
	"web.coupons.issue",
	"web.birthdays.write",
	"web.feedback.write",
	"web.shipping.quote",
	"web.flash.buy",
	// M7 (Task 260; Task 261 dùng): khóa của website thử đọc thêm sản phẩm nháp kèm preview: true.
	"web.catalog.preview",
}

// IsServiceKeyPermission cho biết p là quyền của khóa dịch vụ (ServiceKeyPermissions).
func IsServiceKeyPermission(p Permission) bool { return slices.Contains(ServiceKeyPermissions, p) }

// Lỗi của khóa dịch vụ. Lớp HTTP đổi sang mã ở internal/httpapi/errors_identity.go. Lỗi không mang khóa.
var (
	// ErrPermissionNotForKey: quyền xin cấp không thuộc ServiceKeyPermissions.
	ErrPermissionNotForKey = errors.New("identity: quyền không cấp được cho khóa dịch vụ")
	// ErrInvalidKey: khóa sai dạng, tiền tố lạ hay bí mật không khớp.
	ErrInvalidKey = errors.New("identity: khóa dịch vụ không hợp lệ")
	// ErrRevoked: khóa đúng nhưng đã thu hồi.
	ErrRevoked = errors.New("identity: khóa dịch vụ đã thu hồi")
	// ErrIPNotAllowed: khóa có danh sách CIDR mà IP của máy gọi không nằm trong (hoặc không có IP).
	ErrIPNotAllowed = errors.New("identity: IP không được dùng khóa này")
	// ErrServiceKeyNotFound: không có khóa với mã này trong tenant.
	ErrServiceKeyNotFound = errors.New("identity: không có khóa dịch vụ này")
)

var keyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding) //nolint:gochecknoglobals // bảng mã hằng

// GenerateServiceKey đọc 40 byte từ r (crypto/rand ở mã thật) và trả khóa đầy đủ "ek_<tiền tố>_<bí mật>" (tiền tố 12
// ký tự base32 thường — 60 bit, bí mật 32 byte base32 thường không đệm), tiền tố và sha256(khóa đầy đủ).
func GenerateServiceKey(r io.Reader) (full, prefix string, hash [32]byte, err error) {
	buf := make([]byte, prefixBytes+secretBytes)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", "", [32]byte{}, fmt.Errorf("sinh khóa dịch vụ: %w", err)
	}
	prefix = strings.ToLower(keyEncoding.EncodeToString(buf[:prefixBytes]))[:prefixLen]
	secret := strings.ToLower(keyEncoding.EncodeToString(buf[prefixBytes:]))
	full = serviceKeyScheme + prefix + "_" + secret
	return full, prefix, sha256.Sum256([]byte(full)), nil
}

// SplitServiceKey trả tiền tố của khóa đầy đủ khi khóa đúng dạng "ek_" + 12 ký tự + "_" + 52 ký tự (a-z, 2-7).
func SplitServiceKey(full string) (prefix string, ok bool) {
	rest, ok := strings.CutPrefix(full, serviceKeyScheme)
	if !ok {
		return "", false
	}
	prefix, secret, ok := strings.Cut(rest, "_")
	if !ok || len(prefix) != prefixLen || len(secret) != secretLen || !keyAlphabet(prefix) || !keyAlphabet(secret) {
		return "", false
	}
	return prefix, true
}

func keyAlphabet(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '2' || r > '7') })
}

// CreateServiceKeyInput là đầu vào của CreateServiceKey. AllowedCIDRs rỗng: mọi IP. Actor là nhân viên tạo khóa.
type CreateServiceKeyInput struct {
	Name         string
	Permissions  []Permission
	AllowedCIDRs []netip.Prefix
	Actor        AuthUser
}

// ServiceKey là một khóa dịch vụ (không bao giờ kèm băm hay bí mật).
type ServiceKey struct {
	ID                    uuid.UUID
	Name, KeyPrefix       string
	Permissions           []Permission
	AllowedCIDRs          []netip.Prefix
	CreatedAt             time.Time
	RevokedAt, LastUsedAt *time.Time
	Version               int
}

// CreateServiceKey tạo khóa trong tx của bên gọi (tenancy.WithTenant của in.Actor.TenantID): kiểm đầu vào (tên 1–80
// ký tự, ít nhất một quyền, quyền ngoài ServiceKeyPermissions → ErrPermissionNotForKey, tối đa 32 CIDR hợp lệ), sinh
// khóa, ghi tuyến tenancy.PutRoute(service_key, tiền tố), dòng khóa (chỉ băm) và một dòng audit (không có khóa). Tiền
// tố đã thuộc tenant khác (ErrRouteTaken) hay trùng trong tenant → sinh lại một lần trong cùng tx (savepoint); lần hai
// vẫn trùng → lỗi. Trả khóa đầy đủ: bên gọi chỉ đưa nó ra sau khi commit, đúng một lần.
func CreateServiceKey(ctx context.Context, tx pgx.Tx, in CreateServiceKeyInput) (ServiceKey, string, error) {
	return createServiceKey(ctx, tx, in, rand.Reader)
}

func createServiceKey(ctx context.Context, tx pgx.Tx, in CreateServiceKeyInput, r io.Reader) (ServiceKey, string, error) {
	k, err := normalizeKeyInput(in)
	if err != nil {
		return ServiceKey{}, "", err
	}
	var full string
	for attempt := range keyCreateRetries {
		f, prefix, hash, err := GenerateServiceKey(r)
		if err != nil {
			return ServiceKey{}, "", err
		}
		row, err := insertServiceKey(ctx, tx, in, k, prefix, hash)
		if errors.Is(err, errKeyTaken) && attempt+1 < keyCreateRetries {
			continue
		}
		if errors.Is(err, errKeyTaken) {
			return ServiceKey{}, "", fmt.Errorf("tạo khóa dịch vụ: %w", tenancy.ErrRouteTaken)
		}
		if err != nil {
			return ServiceKey{}, "", err
		}
		full, k.ID, k.KeyPrefix, k.CreatedAt, k.Version = f, row.ID, prefix, row.CreatedAt, int(row.Version)
		break
	}
	if err := RecordAudit(ctx, tx, AuditEntry{
		TenantID: in.Actor.TenantID, ActorType: auditActorType(in.Actor), ActorID: auditActorID(in.Actor),
		Action: "identity.service_key.create", EntityType: auditServiceKey, EntityID: k.ID.String(), After: keyAudit(k),
	}); err != nil {
		return ServiceKey{}, "", err
	}
	return k, full, nil
}

// errKeyTaken: tiền tố (hay băm) vừa sinh đã có — thuộc tenant khác hoặc chính tenant này; sinh lại.
var errKeyTaken = errors.New("identity: tiền tố khóa đã có")

// insertServiceKey ghi tuyến và dòng khóa trong một savepoint (db.Savepoint): trùng thì chỉ savepoint lùi (tx của
// bên gọi còn dùng được) và trả errKeyTaken.
func insertServiceKey(ctx context.Context, tx pgx.Tx, in CreateServiceKeyInput, k ServiceKey, prefix string, hash [32]byte,
) (store.InsertServiceKeyRow, error) {
	var row store.InsertServiceKeyRow
	err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		if err := tenancy.PutRoute(ctx, sp, RouteKindServiceKey, prefix); errors.Is(err, tenancy.ErrRouteTaken) {
			return errKeyTaken
		} else if err != nil {
			return err //nolint:wrapcheck // tenancy đã bọc
		}
		var err error
		row, err = store.New(sp).InsertServiceKey(ctx, store.InsertServiceKeyParams{
			TenantID: in.Actor.TenantID, Name: k.Name, KeyPrefix: prefix, KeyHash: hash[:],
			Permissions: permStrings(k.Permissions), AllowedCidrs: prefixStrings(k.AllowedCIDRs), CreatedBy: in.Actor.UserID,
		})
		if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == "23505" {
			return errKeyTaken
		}
		if err != nil {
			return fmt.Errorf("ghi khóa dịch vụ: %w", err)
		}
		return nil
	})
	if err != nil {
		return store.InsertServiceKeyRow{}, err //nolint:wrapcheck // lỗi của fn trả nguyên hoặc db đã bọc
	}
	return row, nil
}

// normalizeKeyInput kiểm và chuẩn hóa đầu vào: tên bỏ khoảng trắng hai đầu, quyền sắp và bỏ trùng, CIDR che bit host,
// sắp và bỏ trùng.
func normalizeKeyInput(in CreateServiceKeyInput) (ServiceKey, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > maxKeyName || !utf8.ValidString(name) ||
		strings.ContainsFunc(name, unicode.IsControl) || len(in.Permissions) == 0 || len(in.AllowedCIDRs) > maxKeyCIDRs ||
		in.Actor.TenantID == uuid.Nil || in.Actor.UserID == uuid.Nil {
		return ServiceKey{}, ErrInvalidInput
	}
	ps := slices.Clone(in.Permissions)
	for _, p := range ps {
		if !IsServiceKeyPermission(p) {
			return ServiceKey{}, ErrPermissionNotForKey
		}
	}
	slices.Sort(ps)
	cidrs := make([]netip.Prefix, 0, len(in.AllowedCIDRs))
	for _, c := range in.AllowedCIDRs {
		if !c.IsValid() {
			return ServiceKey{}, ErrInvalidInput
		}
		cidrs = append(cidrs, c.Masked())
	}
	slices.SortFunc(cidrs, func(a, b netip.Prefix) int { return strings.Compare(a.String(), b.String()) })
	return ServiceKey{Name: name, Permissions: slices.Compact(ps), AllowedCIDRs: slices.Compact(cidrs)}, nil
}

// RevokeServiceKey thu hồi khóa id khi version còn khớp và khóa chưa thu hồi (cập nhật có điều kiện), ghi audit; trả
// khóa sau thu hồi (KeyPrefix để bên gọi phát báo hiệu TopicServiceKeyRevoked trong cùng tx). Không có (kể cả của
// tenant khác) → ErrServiceKeyNotFound; lệch version hay đã thu hồi → ErrVersionConflict. actor không phải nhân viên
// (UserID rỗng, lệnh crm service-key revoke) → revoked_by NULL, audit actor system.
func RevokeServiceKey(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int, actor AuthUser) (ServiceKey, error) {
	q := store.New(tx)
	by := pgtype.UUID{Bytes: actor.UserID, Valid: actor.Kind != KindService && actor.UserID != uuid.Nil}
	v := int32(max(0, min(version, maxVersion))) // kẹp trong [0, 2^31−1]: ngoài khoảng → không dòng nào khớp
	r, err := q.RevokeServiceKey(ctx, store.RevokeServiceKeyParams{RevokedBy: by, ID: id, Version: v})
	if errors.Is(err, pgx.ErrNoRows) {
		exists, err := q.ServiceKeyExists(ctx, id)
		switch {
		case err != nil:
			return ServiceKey{}, fmt.Errorf("đọc khóa dịch vụ: %w", err)
		case !exists:
			return ServiceKey{}, ErrServiceKeyNotFound
		}
		return ServiceKey{}, ErrVersionConflict
	}
	if err != nil {
		return ServiceKey{}, fmt.Errorf("thu hồi khóa dịch vụ: %w", err)
	}
	k, err := keyFromRow(store.ListServiceKeysRow(r))
	if err != nil {
		return ServiceKey{}, err
	}
	if err := RecordAudit(ctx, tx, AuditEntry{
		TenantID: actor.TenantID, ActorType: auditActorType(actor), ActorID: auditActorID(actor),
		Action: "identity.service_key.revoke", EntityType: auditServiceKey, EntityID: id.String(),
		Before: map[string]any{"keyPrefix": k.KeyPrefix, "version": version, "revoked": false},
		After:  map[string]any{"keyPrefix": k.KeyPrefix, "version": k.Version, "revoked": true},
	}); err != nil {
		return ServiceKey{}, err
	}
	return k, nil
}

const maxVersion = 1<<31 - 1

// ListServiceKeys trả mọi khóa của tenant trong ngữ cảnh của tx (cũ trước), không có băm.
func ListServiceKeys(ctx context.Context, tx pgx.Tx) ([]ServiceKey, error) {
	rows, err := store.New(tx).ListServiceKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("liệt kê khóa dịch vụ: %w", err)
	}
	out := make([]ServiceKey, 0, len(rows))
	for _, r := range rows {
		k, err := keyFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

func keyFromRow(r store.ListServiceKeysRow) (ServiceKey, error) {
	cidrs, err := parsePrefixes(r.AllowedCidrs)
	if err != nil {
		return ServiceKey{}, err
	}
	return ServiceKey{
		ID: r.ID, Name: r.Name, KeyPrefix: r.KeyPrefix, Permissions: toPermissions(r.Permissions), AllowedCIDRs: cidrs,
		CreatedAt: r.CreatedAt, RevokedAt: optTime(r.RevokedAt), LastUsedAt: optTime(r.LastUsedAt), Version: int(r.Version),
	}, nil
}

// ServiceKeyRecord là dòng khóa cho bước kiểm (có băm; chỉ bộ kiểm khóa giữ, không đưa ra API).
type ServiceKeyRecord struct {
	ID, TenantID uuid.UUID
	Hash         [32]byte
	Permissions  []Permission
	AllowedCIDRs []netip.Prefix
	RevokedAt    *time.Time
}

// LoadServiceKey đọc khóa theo tiền tố trong tx của tenant (tenancy.Resolve(service_key, tiền tố) cho tenant). Không
// có → ErrInvalidKey.
func LoadServiceKey(ctx context.Context, tx pgx.Tx, prefix string) (ServiceKeyRecord, error) {
	r, err := store.New(tx).GetServiceKeyByPrefix(ctx, prefix)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServiceKeyRecord{}, ErrInvalidKey
	}
	if err != nil {
		return ServiceKeyRecord{}, fmt.Errorf("đọc khóa dịch vụ: %w", err)
	}
	cidrs, err := parsePrefixes(r.AllowedCidrs)
	if err != nil {
		return ServiceKeyRecord{}, err
	}
	rec := ServiceKeyRecord{
		ID: r.ID, TenantID: r.TenantID, Permissions: toPermissions(r.Permissions), AllowedCIDRs: cidrs, RevokedAt: optTime(r.RevokedAt),
	}
	copy(rec.Hash[:], r.KeyHash)
	return rec, nil
}

// CheckServiceKey so khóa đầy đủ full với rec (SHA-256, so hằng thời gian), rồi thu hồi (ErrRevoked), rồi IP của máy
// gọi (CIDR có mà IP không nằm trong hay không có → ErrIPNotAllowed). Bí mật sai → ErrInvalidKey (kể cả khóa đã thu
// hồi: không cho biết trạng thái khi không giữ khóa). Trả người gọi KindService, quyền của khóa phạm vi all.
func CheckServiceKey(rec ServiceKeyRecord, full string, ip netip.Addr) (AuthUser, error) {
	sum := sha256.Sum256([]byte(full))
	if subtle.ConstantTimeCompare(sum[:], rec.Hash[:]) != 1 {
		return AuthUser{}, ErrInvalidKey
	}
	if rec.RevokedAt != nil {
		return AuthUser{}, ErrRevoked
	}
	if len(rec.AllowedCIDRs) > 0 {
		a := ip.Unmap()
		if !a.IsValid() || !slices.ContainsFunc(rec.AllowedCIDRs, func(p netip.Prefix) bool { return p.Contains(a) }) {
			return AuthUser{}, ErrIPNotAllowed
		}
	}
	grants := make([]Grant, 0, len(rec.Permissions))
	for _, p := range rec.Permissions {
		grants = append(grants, Grant{Permission: p, Scope: Scope{Type: ScopeAll}})
	}
	return AuthUser{TenantID: rec.TenantID, Grants: grants, Kind: KindService, ServiceKeyID: rec.ID}, nil
}

// TouchServiceKeys đặt last_used_at = at cho các khóa ids của tenant trong tx khi mốc cũ hơn (chỉ tiến lên, không đổi
// version). Bộ kiểm khóa gọi tối đa một lần mỗi phút mỗi khóa, ngoài đường yêu cầu.
func TouchServiceKeys(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, at time.Time) error {
	if err := store.New(tx).TouchServiceKeys(ctx, store.TouchServiceKeysParams{
		At: pgtype.Timestamptz{Time: at.UTC(), Valid: true}, Ids: ids,
	}); err != nil {
		return fmt.Errorf("ghi lần dùng khóa dịch vụ: %w", err)
	}
	return nil
}

// auditActorType, auditActorID: người làm của audit — khóa dịch vụ (service, mã khóa), nhân viên (staff, mã nhân
// viên), không có cả hai (system, rỗng).
func auditActorType(u AuthUser) string {
	switch {
	case u.Kind == KindService:
		return "service"
	case u.UserID != uuid.Nil:
		return "staff"
	}
	return "system"
}

func auditActorID(u AuthUser) string {
	switch {
	case u.Kind == KindService:
		return u.ServiceKeyID.String()
	case u.UserID != uuid.Nil:
		return u.UserID.String()
	}
	return ""
}

// keyAudit: trạng thái của khóa ghi vào audit (tiền tố không bí mật; không băm, không khóa).
func keyAudit(k ServiceKey) map[string]any {
	return map[string]any{
		"name": k.Name, "keyPrefix": k.KeyPrefix, "permissions": permStrings(k.Permissions),
		"allowedCidrs": prefixStrings(k.AllowedCIDRs), "version": k.Version,
	}
}

func permStrings(ps []Permission) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, string(p))
	}
	return out
}

func toPermissions(ss []string) []Permission {
	out := make([]Permission, 0, len(ss))
	for _, s := range ss {
		out = append(out, Permission(s))
	}
	return out
}

func prefixStrings(ps []netip.Prefix) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.String())
	}
	return out
}

func parsePrefixes(ss []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("đọc CIDR của khóa dịch vụ: %w", err)
		}
		out = append(out, p)
	}
	return out, nil
}

func optTime(t pgtype.Timestamptz) *time.Time {
	if t.Valid {
		return new(t.Time.UTC())
	}
	return nil
}
