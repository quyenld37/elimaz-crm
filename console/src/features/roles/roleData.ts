// Dữ liệu của màn Vai trò (kế hoạch M1 Task M1-56). Danh sách vai trò dùng chung khóa với màn Nhân viên (staffData),
// nên lưu vai trò xong thì màn Nhân viên cũng thấy vai trò mới.
import { useQuery } from "@tanstack/react-query";
import { getMe, listPermissions, listRoles } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";

/** Khóa truy vấn danh sách vai trò (cùng khóa của màn Nhân viên); ghi xong thì làm mới khóa này. */
export const ROLES_KEY = queryKey("identity.roles");

export function useRoleData() {
  const roles = useQuery({ queryKey: ROLES_KEY, queryFn: () => unwrap(listRoles()) });
  const permissions = useQuery({
    queryKey: queryKey("identity.permissions"),
    queryFn: () => unwrap(listPermissions()),
    staleTime: Infinity,
  });
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  return { roles, permissions, me };
}
