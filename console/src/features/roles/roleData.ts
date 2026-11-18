// Dữ liệu của màn Vai trò (kế hoạch M1 Task M1-56). Danh sách vai trò đọc qua listRolesToManage (chỉ cần
// identity.roles.manage; màn Nhân viên đọc listRoles, cần identity.users.manage). Khóa của màn này là con của khóa màn
// Nhân viên, nên lưu vai trò xong làm mới ROLES_KEY thì cả hai màn thấy vai trò mới.
import { useQuery } from "@tanstack/react-query";
import { getMe, listPermissions, listRolesToManage } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";

/** Khóa truy vấn danh sách vai trò của màn Nhân viên; ghi xong làm mới khóa này (khớp cả MANAGE_KEY theo tiền tố). */
export const ROLES_KEY = queryKey("identity.roles");
const MANAGE_KEY = queryKey("identity.roles", "manage");

export function useRoleData() {
  const roles = useQuery({ queryKey: MANAGE_KEY, queryFn: () => unwrap(listRolesToManage()) });
  const permissions = useQuery({
    queryKey: queryKey("identity.permissions"),
    queryFn: () => unwrap(listPermissions()),
    staleTime: Infinity,
  });
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  return { roles, permissions, me };
}
