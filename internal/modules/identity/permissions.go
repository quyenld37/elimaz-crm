// Package identity sở hữu người dùng, vai trò và quyền theo phạm vi của CRM (spec 04 mục 3, mục 11; ADR-18, ADR-35).
//
// Phần công khai: danh sách quyền (Permissions), người dùng đã xác thực (AuthUser) và câu hỏi duy nhất về quyền
// (Can); nạp người dùng theo email (LoadUser); luật thẻ Cloudflare Access (Verifier, CheckMFA). Gói không biết HTTP:
// phần tải khóa và kiểm chữ ký thẻ nằm ở internal/adapters/cfaccess, lớp HTTP ở internal/httpapi.
//
// Đây là module mẫu cho module và đường đọc mới: phần công khai ở gói này, câu SQL trong queries/identity sinh ra
// internal/store (sqlc), mọi truy vấn trong tenancy.WithTenant, test trên PostgreSQL thật qua internal/testkit.
package identity

import (
	"slices"

	"github.com/google/uuid"
)

// Permission là khóa quyền, ví dụ "audit.read". Route khai bằng x-permission trong openapi.yaml.
type Permission string

// ScopeKind cho biết quyền được kiểm theo đích (một Page, một kho) hay không.
type ScopeKind int

const (
	// ScopeNone là quyền không gắn với đích: có hiệu lực toàn shop khi người dùng có quyền này trong bất kỳ grant nào
	// (gán theo kho, theo Page cũng được; Task 315). Phạm vi chỉ thu hẹp quyền ScopeByTarget.
	ScopeNone ScopeKind = iota
	// ScopeByTarget là quyền trên từng đích; grant all hoặc grant đúng đích cho quyền.
	ScopeByTarget
	// ScopeShop là quyền quản trị (nhân viên, vai trò, nền tảng, nhật ký, chính sách toàn shop): chỉ có hiệu lực từ
	// grant phạm vi all; grant theo kho, theo Page của quyền này không có hiệu lực (Task 315 lượt 2, docs/quyen.md).
	ScopeShop
)

// PermissionDef là một quyền của hệ thống và cách kiểm phạm vi của nó.
type PermissionDef struct {
	Key   Permission
	Scope ScopeKind
}

// Permissions là mọi quyền của CRM: M0 (kế hoạch M0 Task 7), M1 (kế hoạch M1 Task M1-26), M2 (Task M2-01), M3
// (Task M3-04), M4 (Task 143) và M5. Bảng chuẩn kèm việc
// của từng quyền: docs/quyen.md (test giữ hai bên khớp). Mốc sau chỉ thêm quyền, không đổi loại phạm vi của quyền
// đã có (ngoại lệ đã duyệt: Task 315 lượt 2 chuyển sáu quyền quản trị sang ScopeShop). Quyền mới ghi chính sách
// toàn shop hay quản trị là ScopeShop. Route khai quyền ngoài danh sách này thì tests/arch đỏ.
var Permissions = []PermissionDef{ //nolint:gochecknoglobals // danh sách hằng, không sửa lúc chạy
	{"realtime.subscribe", ScopeNone},
	{"audit.read", ScopeShop},
	{"platform.jobs.manage", ScopeShop},
	{"platform.health.read", ScopeShop},
	{"identity.users.manage", ScopeShop},
	{"conversation.read", ScopeByTarget},
	// M1.
	{"catalog.read", ScopeNone},
	{"catalog.manage", ScopeNone},
	{"catalog.import", ScopeNone},
	{"order.create", ScopeNone},
	{"order.read", ScopeNone},
	{"files.upload", ScopeNone},
	{"platform.files.manage", ScopeShop},
	{"inventory.suppliers.manage", ScopeNone},
	{"identity.roles.manage", ScopeShop},
	{"inventory.read", ScopeByTarget},
	{"inventory.documents.create", ScopeByTarget},
	{"inventory.documents.post", ScopeByTarget},
	{"inventory.stocktake", ScopeByTarget},
	{"inventory.stocktake.apply", ScopeByTarget},
	{"fulfillment.read", ScopeByTarget},
	{"fulfillment.ship", ScopeByTarget},
	// M2 (kế hoạch M2 Task M2-01). Tạo, sửa mã giảm giá và cài đặt đơn là chính sách toàn shop: ScopeShop.
	{"customer.read", ScopeNone},
	{"customer.write", ScopeNone},
	{"customer.merge", ScopeNone},
	{"customer.import", ScopeNone},
	{"customer.tags.manage", ScopeNone},
	{"order.update", ScopeNone},
	{"order.confirm", ScopeNone},
	{"order.cancel", ScopeNone},
	{"order.discount.approve", ScopeNone},
	{"order.return", ScopeNone},
	{"order.return.receive", ScopeNone},
	{"order.duplicates.resolve", ScopeNone},
	{"order.settings.manage", ScopeShop},
	{"promotion.read", ScopeNone},
	{"promotion.manage", ScopeShop},
	// M3 (kế hoạch M3 Task M3-04). Kết nối hãng và cài đặt vận chuyển là chính sách toàn shop: ScopeShop. Thao tác
	// của kho (báo phí, hủy, in tem, đóng gói, cập nhật tay, giữ hàng quá hạn) theo kho như fulfillment.ship.
	{"fulfillment.carrier_accounts.read", ScopeNone},
	{"fulfillment.carrier_accounts.manage", ScopeShop},
	{"fulfillment.settings.manage", ScopeShop},
	{"fulfillment.quote", ScopeByTarget},
	{"fulfillment.cancel", ScopeByTarget},
	{"fulfillment.print", ScopeByTarget},
	{"fulfillment.pack", ScopeByTarget},
	{"fulfillment.manual_status", ScopeByTarget},
	{"fulfillment.correct", ScopeNone},
	{"fulfillment.callbacks.work", ScopeNone},
	{"inventory.aged.read", ScopeByTarget},
	{"cash.cod.read", ScopeNone},
	{"cash.statements.import", ScopeNone},
	{"cash.statements.confirm", ScopeNone},
	{"cash.adjust", ScopeNone},
	{"cash.payments.record", ScopeNone},
	{"cash.refunds.record", ScopeNone},
	{"cash.refunds.waive", ScopeNone},
	// M5 (kế hoạch M5 Task 189): tạo, xem, thu hồi khóa dịch vụ của website là quản trị: ScopeShop, chỉ Chủ.
	{"identity.service_keys.manage", ScopeShop},
	// M5 (Task 456): tạm dừng, mở lại nhận đơn web là chính sách toàn shop: ScopeShop (đọc dùng order.read).
	{"order.web_intake.manage", ScopeShop},
	// M5 (Task 195): yêu cầu về đơn hàng của khách (xem; trả lời, đổi trạng thái) là việc hằng ngày của bán hàng.
	{"order.requests.read", ScopeNone},
	{"order.requests.manage", ScopeNone},
	// M5 (Task 468): cài đặt quà sinh nhật, Tạo mã ngay, Đã nhắn, xuất danh sách là chính sách toàn shop (đọc dùng
	// promotion.read).
	{"promotion.birthday.manage", ScopeShop},
	// M5 (Task 199): soạn đợt flash sale đặt giá bán cho cả shop: ScopeShop như promotion.manage (luật Task 315);
	// xem đợt ScopeNone như promotion.read.
	{"promotion.flash.read", ScopeNone},
	{"promotion.flash.manage", ScopeShop},
	// M5 (Task 453): nội dung trang chủ website (banner; bộ phối ở Task 454) là chính sách toàn shop.
	{"content.read", ScopeShop},
	{"content.manage", ScopeShop},
	// M5 (Task 455): góp ý của khách (xem; đổi trạng thái, ghi chú nội bộ) là việc của chủ, quản lý cho cả shop.
	{"customer.feedback.read", ScopeShop},
	{"customer.feedback.manage", ScopeShop},
	// M4 (kế hoạch M4 Task 143): hội thoại, Live theo đích Page (phạm vi page:<channel_accounts.id>, như
	// conversation.read của M0); kết nối kênh, chế độ gửi là chính sách toàn shop: ScopeShop (luật Task 315).
	{"channel.manage", ScopeShop},
	{"conversation.reply", ScopeByTarget},
	{"conversation.assign", ScopeByTarget},
	{"conversation.settings", ScopeByTarget},
	{"live.operate", ScopeByTarget},
}

// Loại phạm vi của một grant (cột user_role_grants.scope_type).
const (
	ScopeAll       = "all"
	ScopePage      = "page"
	ScopeWarehouse = "warehouse"
)

// Scope là phạm vi của một grant hoặc đích của một lần kiểm quyền. Type all thì ID rỗng.
type Scope struct {
	Type string
	ID   string
}

// Grant là một cặp (quyền, phạm vi) của người dùng.
type Grant struct {
	Permission Permission
	Scope      Scope
}

// Loại người gọi của AuthUser.Kind.
const (
	// KindStaff: nhân viên (thẻ Cloudflare Access, LoadUser).
	KindStaff = "staff"
	// KindService: khóa dịch vụ của website (CheckServiceKey); UserID rỗng, ServiceKeyID là mã khóa.
	KindService = "service"
)

// AuthUser là người gọi đã xác thực của một yêu cầu, kèm mọi grant tại thời điểm yêu cầu. Kind là KindStaff (UserID
// là nhân viên) hoặc KindService (khóa dịch vụ: UserID rỗng, ServiceKeyID là mã khóa, Grants là quyền web.* của khóa
// phạm vi all); rỗng ở mã dựng AuthUser bằng tay (test) coi như nhân viên.
type AuthUser struct {
	UserID       uuid.UUID
	TenantID     uuid.UUID
	Email        string
	Grants       []Grant
	Kind         string
	ServiceKeyID uuid.UUID
}

// IsKnown cho biết p có trong Permissions (quyền nhân viên) hoặc ServiceKeyPermissions (quyền của khóa dịch vụ) không.
func IsKnown(p Permission) bool {
	return IsServiceKeyPermission(p) || slices.ContainsFunc(Permissions, func(d PermissionDef) bool { return d.Key == p })
}

// KindOf trả cách kiểm phạm vi của p (quyền của khóa dịch vụ đều ScopeNone); quyền lạ coi là ScopeNone (Can từ chối quyền lạ trước khi xét loại).
func KindOf(p Permission) ScopeKind {
	for _, d := range Permissions {
		if d.Key == p {
			return d.Scope
		}
	}
	return ScopeNone
}

// Can trả lời "u có quyền p trên target không" (mặc định từ chối, ADR-18):
//   - quyền lạ: luôn sai;
//   - quyền ScopeShop (quản trị): cần grant của p phạm vi all (target bỏ qua; Task 315 lượt 2);
//   - quyền ScopeNone: cần ít nhất một grant của p, ở bất kỳ phạm vi nào (target bỏ qua; Task 315: phạm vi chỉ thu
//     hẹp quyền của tài nguyên có phạm vi, nên vai trò Kho gán theo một kho vẫn đọc được sản phẩm);
//   - quyền ScopeByTarget có target: cần grant của p phạm vi all hoặc đúng target;
//   - quyền ScopeByTarget không target: cần ít nhất một grant của p (handler kiểm đích sau).
//
// Không có grant nào thì không có quyền.
func Can(u AuthUser, p Permission, target *Scope) bool {
	if !IsKnown(p) {
		return false
	}
	kind := KindOf(p)
	for _, g := range u.Grants {
		if g.Permission != p {
			continue
		}
		switch {
		case isAll(g.Scope):
			return true
		case kind == ScopeShop:
			continue
		case kind == ScopeNone || target == nil || g.Scope == *target:
			return true
		}
	}
	return false
}

// GrantedAt trả lời "u có grant hiệu lực của p ở phạm vi all hoặc đúng target không", không xét loại phạm vi của p
// (mặc định từ chối). Dùng khi một quyền none (có hiệu lực toàn shop với Can) vẫn phải giới hạn theo đích ở một thao
// tác cụ thể: order.create giữ hàng ở kho (Task 411, docs/quyen.md). Quyền theo đích dùng Can. Quyền lạ: luôn sai;
// grant không hiệu lực (Effective sai) không tính. GrantedAt đúng thì Can(u, p, &target) cũng đúng.
func GrantedAt(u AuthUser, p Permission, target Scope) bool {
	if !IsKnown(p) {
		return false
	}
	return slices.ContainsFunc(u.Grants, func(g Grant) bool {
		return g.Permission == p && Effective(g) && (isAll(g.Scope) || g.Scope == target)
	})
}

// Effective cho biết grant g có hiệu lực không: grant của quyền ScopeShop chỉ có hiệu lực ở phạm vi all; quyền khác
// luôn có (phạm vi của chúng do Can kiểm theo đích).
func Effective(g Grant) bool {
	return KindOf(g.Permission) != ScopeShop || isAll(g.Scope)
}

// ScopeWarning là một gán vai trò ở phạm vi hẹp hơn toàn shop mà vai trò có quyền ScopeShop: các quyền đó (đã sắp)
// không có hiệu lực từ gán này. Gán vẫn lưu (không lỗi); lớp HTTP trả kèm câu báo cho màn hình.
type ScopeWarning struct {
	RoleKey     string
	Scope       Scope
	Permissions []Permission
}

// ShopOnlyWarnings trả cảnh báo cho từng gán trong grants (theo thứ tự) có phạm vi khác all mà vai trò (tra trong
// roles theo khóa) có quyền ScopeShop. Vai trò không có trong roles: bỏ qua. Không cảnh báo nào: lát rỗng khác nil.
func ShopOnlyWarnings(roles []RoleRow, grants []GrantInput) []ScopeWarning {
	index := roleIndex(roles)
	out := []ScopeWarning{}
	for _, g := range grants {
		r, ok := index[g.RoleKey]
		if !ok || isAll(g.Scope) {
			continue
		}
		var perms []Permission
		for _, p := range r.Permissions {
			if KindOf(p) == ScopeShop {
				perms = append(perms, p)
			}
		}
		if len(perms) > 0 {
			slices.Sort(perms)
			out = append(out, ScopeWarning{RoleKey: g.RoleKey, Scope: g.Scope, Permissions: slices.Compact(perms)})
		}
	}
	return out
}

func isAll(s Scope) bool { return s.Type == ScopeAll && s.ID == "" }

// ScopesFor trả phạm vi của u cho quyền p: all = true khi có grant phạm vi all; ngược lại ids là mã đích (đã sắp,
// không trùng) để lọc danh sách. Không grant nào: false, rỗng.
func ScopesFor(u AuthUser, p Permission) (all bool, ids []string) {
	for _, g := range u.Grants {
		if g.Permission != p {
			continue
		}
		if g.Scope.Type == ScopeAll {
			return true, nil
		}
		ids = append(ids, g.Scope.ID)
	}
	slices.Sort(ids)
	return false, slices.Compact(ids)
}
