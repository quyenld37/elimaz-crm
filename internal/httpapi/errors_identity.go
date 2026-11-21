package httpapi

// Bảng đổi lỗi của vùng identity sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// Mã lỗi chỉ vùng identity trả.
const (
	codeCannotModifySelf  = "cannot_modify_self"
	codeGrantExceedsActor = "grant_exceeds_actor"
	codeLastOwner         = "last_owner"
	codeEmailTaken        = "email_taken"
	codeOwnerRoleLocked   = "owner_role_locked"
	codeRoleInUse         = "role_in_use"
	codeRoleArchived      = "role_archived"
	codeRoleNameTaken     = "role_name_taken"
	codeUnknownPermission = "unknown_permission"
)

// identityError đổi lỗi nghiệp vụ của quản lý nhân viên (PQ07) và vai trò (M1-56) sang mã HTTP. ErrLocked ở đây là người thao tác vừa bị
// khóa giữa lúc xác thực và lúc ghi: như bước xác thực, 403 forbidden. ErrStaffNotFound: không có hoặc của tenant
// khác, không cho biết là có.
func identityError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{identity.ErrSelfModify, http.StatusForbidden, codeCannotModifySelf},
		{identity.ErrGrantExceedsActor, http.StatusForbidden, codeGrantExceedsActor},
		{identity.ErrLocked, http.StatusForbidden, codeForbidden},
		{identity.ErrLastOwner, http.StatusConflict, codeLastOwner},
		{identity.ErrEmailTaken, http.StatusConflict, codeEmailTaken},
		{identity.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{identity.ErrStaffNotFound, http.StatusNotFound, codeNotFound},
		{identity.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
		{identity.ErrOwnerRoleLocked, http.StatusConflict, codeOwnerRoleLocked},
		{identity.ErrRoleInUse, http.StatusConflict, codeRoleInUse},
		{identity.ErrRoleArchived, http.StatusConflict, codeRoleArchived},
		{identity.ErrRoleNameTaken, http.StatusConflict, codeRoleNameTaken},
		{identity.ErrUnknownPermission, http.StatusUnprocessableEntity, codeUnknownPermission},
		{identity.ErrRoleNotFound, http.StatusNotFound, codeNotFound},
	})
}
