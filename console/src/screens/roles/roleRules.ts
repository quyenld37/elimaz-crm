// Luật hiển thị của màn Vai trò (kế hoạch M1 Task M1-56; mockup docs/mockups/m1/vai-tro.html). Luật thật ở API
// (identity.CreateRole, UpdateRole, ArchiveRole): các hàm ở đây chỉ dựng nhãn và làm mờ quyền chắc chắn bị từ chối.
import type { Grant, PermissionInfo, Role } from "../../api";
import { isCustom } from "../staff/staffRules";

export const OWNER_KEY = "owner";

/** Cột "Loại" của danh sách vai trò. */
export function roleKind(r: Role): string {
  if (r.key === OWNER_KEY) return "Mẫu · luôn đủ mọi quyền";
  if (isCustom(r)) return "Tự tạo";
  return r.customized ? "Mẫu đã sửa" : "Mẫu";
}

/** Vai trò chỉ xem (không sửa, không lưu trữ): vai trò Chủ và vai trò đã lưu trữ. */
export const isReadOnly = (r: Role): boolean => r.key === OWNER_KEY || r.status === "archived";

/**
 * Người sửa thêm hay gỡ được quyền p: có p ở toàn shop (quyền loại "none" thì gán nào cũng được, vì nó có hiệu lực toàn
 * shop). Như identity.Can(actor, p, all) trên grant đang hiệu lực (Me.grants).
 */
export function canChange(p: PermissionInfo, actor: readonly Grant[]): boolean {
  return actor.some((g) => g.permission === p.key && (p.scopeType === "none" || g.scope.type === "all"));
}

export interface PermissionGroup {
  group: string;
  items: PermissionInfo[];
}

/** Nhóm quyền theo thứ tự API trả (đã xếp theo nhóm của màn). */
export function groupPermissions(list: readonly PermissionInfo[]): PermissionGroup[] {
  const out: PermissionGroup[] = [];
  for (const p of list) {
    const last = out.at(-1);
    if (last?.group === p.group) last.items.push(p);
    else out.push({ group: p.group, items: [p] });
  }
  return out;
}

/** Chip phạm vi cạnh nhãn quyền ("theo kho", "theo Page"); quyền khác không có chip. */
export function scopeChip(p: PermissionInfo): string | null {
  if (p.scopeType === "warehouse") return "theo kho";
  if (p.scopeType === "page") return "theo Page";
  return null;
}

/**
 * Quyền gửi lên khi lưu: các quyền đã chọn có trong danh mục (quyền lạ còn sót trong vai trò bị bỏ: máy chủ cho gỡ tự
 * do, gửi lại thì bị unknown_permission), theo thứ tự danh mục.
 */
export function toSubmit(selected: ReadonlySet<string>, catalog: readonly PermissionInfo[]): string[] {
  return catalog.filter((p) => selected.has(p.key)).map((p) => p.key);
}

/** Quyền người sửa không có nhưng sẽ bị thêm hay gỡ (để báo trước khi gửi; máy chủ vẫn quyết). */
export function blockedChanges(
  before: readonly string[],
  after: ReadonlySet<string>,
  catalog: readonly PermissionInfo[],
  actor: readonly Grant[],
): PermissionInfo[] {
  return catalog.filter((p) => before.includes(p.key) !== after.has(p.key) && !canChange(p, actor));
}
