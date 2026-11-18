package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// ErrInvalidAudit báo AuditEntry thiếu hay sai trường bắt buộc; RecordAudit không ghi gì. Lỗi không mang giá trị của
// Before, After.
var ErrInvalidAudit = errors.New("identity: dòng audit không hợp lệ")

// AuditEntry là một dòng nhật ký thao tác. ActorType là một trong staff, system, carrier, customer, service. ActorID,
// EntityID, IP, RequestID rỗng thì lưu NULL. IP nhận địa chỉ IPv4/IPv6 hoặc dạng ip:port (chỉ lưu ip). Before là
// trạng thái trước (nil khi tạo mới), After là trạng thái sau (nil khi xóa).
type AuditEntry struct {
	TenantID                                         uuid.UUID
	ActorType, ActorID, Action, EntityType, EntityID string
	Before, After                                    map[string]any
	IP, RequestID                                    string
}

// auditActorTypes khớp CHECK của identity.audit_logs.actor_type (migration 0004).
func auditActorTypes() []string { return []string{"staff", "system", "carrier", "customer", "service"} }

// RecordAudit ghi e vào identity.audit_logs trong tx của bên gọi (cùng transaction với thay đổi, tx phải mở bằng
// tenancy.WithTenant của e.TenantID; RLS từ chối tenant khác). changed_fields là mọi khóa cấp đầu có giá trị khác nhau
// giữa Before và After (theo dạng JSON), kể cả khóa nhạy cảm. Khi lưu before/after, giá trị của mọi khóa khớp
// logging.SensitiveKey ở mọi cấp (và kiểu tự che như logging.Phone) thành "[Redacted]"; phần lồng sâu hơn
// logging.DefaultDepth thành "[Truncated]". Chạy lại được trong db.RetryTx (không giữ trạng thái).
func RecordAudit(ctx context.Context, tx pgx.Tx, e AuditEntry) error {
	ip, err := validateAudit(e)
	if err != nil {
		return err
	}
	changed, before, after, err := auditPayload(e.Before, e.After)
	if err != nil {
		return err
	}
	if err := store.New(tx).InsertAuditLog(ctx, store.InsertAuditLogParams{
		TenantID: e.TenantID, ActorType: e.ActorType, ActorID: e.ActorID, Action: e.Action,
		EntityType: e.EntityType, EntityID: e.EntityID, ChangedFields: changed,
		Before: before, After: after, Ip: ip, RequestID: e.RequestID,
	}); err != nil {
		return fmt.Errorf("ghi audit: %w", err)
	}
	return nil
}

// validateAudit kiểm trường bắt buộc và trả IP đã chuẩn hóa ("" khi không có).
func validateAudit(e AuditEntry) (string, error) {
	switch {
	case e.TenantID == uuid.Nil:
		return "", fmt.Errorf("%w: thiếu tenant", ErrInvalidAudit)
	case !slices.Contains(auditActorTypes(), e.ActorType):
		return "", fmt.Errorf("%w: actor_type lạ", ErrInvalidAudit)
	case e.Action == "":
		return "", fmt.Errorf("%w: thiếu action", ErrInvalidAudit)
	case e.EntityType == "":
		return "", fmt.Errorf("%w: thiếu entity_type", ErrInvalidAudit)
	}
	if e.IP == "" {
		return "", nil
	}
	if a, err := netip.ParseAddr(e.IP); err == nil {
		return a.String(), nil
	}
	if ap, err := netip.ParseAddrPort(e.IP); err == nil {
		return ap.Addr().String(), nil
	}
	return "", fmt.Errorf("%w: IP không đọc được", ErrInvalidAudit)
}

// auditPayload trả changed_fields (xếp theo chữ, không nil) và JSON đã che của before, after (nil khi map nil).
func auditPayload(before, after map[string]any) (changed []string, beforeJSON, afterJSON []byte, err error) {
	changed = []string{}
	keys := slices.Collect(maps.Keys(before))
	for k := range after {
		if _, ok := before[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		bv, inBefore := before[k]
		av, inAfter := after[k]
		// Luôn so (cả khi khóa chỉ có một bên) để giá trị không đổi được sang JSON bị từ chối, không bị che lặng lẽ.
		same, err := sameJSON(bv, av)
		if err != nil {
			return nil, nil, nil, err
		}
		if !same || inBefore != inAfter {
			changed = append(changed, k)
		}
	}
	if beforeJSON, err = redactedJSON(before); err != nil {
		return nil, nil, nil, err
	}
	if afterJSON, err = redactedJSON(after); err != nil {
		return nil, nil, nil, err
	}
	return changed, beforeJSON, afterJSON, nil
}

// sameJSON so hai giá trị theo dạng JSON (encoding/json xếp khóa map, nên kết quả ổn định).
func sameJSON(a, b any) (bool, error) {
	ja, err := json.Marshal(a)
	if err != nil {
		return false, fmt.Errorf("%w: giá trị không đổi được sang JSON", ErrInvalidAudit)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		return false, fmt.Errorf("%w: giá trị không đổi được sang JSON", ErrInvalidAudit)
	}
	return string(ja) == string(jb), nil
}

func redactedJSON(m map[string]any) ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(logging.Redact(m, logging.DefaultDepth))
	if err != nil {
		return nil, fmt.Errorf("%w: giá trị không đổi được sang JSON", ErrInvalidAudit)
	}
	return b, nil
}
