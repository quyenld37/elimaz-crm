// Luật hiển thị của màn Nhân viên và quyền (kế hoạch M1 Task 54). Luật thật ở API (identity.ReplaceGrants, Task 26;
// quyền quản trị chỉ hiệu lực toàn shop, Task 315); các hàm ở đây chỉ dựng nhãn và làm mờ lựa chọn chắc chắn bị từ chối.
import type { Grant, PermissionInfo, Role, ScopeWarning, StaffGrant, StaffMember } from "../../api";

export type ScopeType = StaffGrant["scopeType"];
export interface ScopeRef {
  type: ScopeType;
  id: string;
}

export const ALL_SCOPE: ScopeRef = { type: "all", id: "" };

/** Khóa so sánh của một phạm vi (dùng làm value của <option>). */
export const scopeKey = (s: ScopeRef): string => `${s.type}:${s.id}`;

export function scopeFromKey(key: string): ScopeRef {
  const i = key.indexOf(":");
  const type = key.slice(0, i);
  const id = key.slice(i + 1);
  if (type === "warehouse" || type === "page") return { type, id };
  return ALL_SCOPE;
}

export const grantScope = (g: StaffGrant): ScopeRef => ({ type: g.scopeType, id: g.scopeId });

/**
 * Nhãn phạm vi. Chưa có API danh sách kho (module inventory, Task 28) nên kho, Page hiện bằng mã; khi có thì thay ở đây.
 */
export function scopeLabel(s: ScopeRef): string {
  if (s.type === "warehouse") return `Kho ${s.id}`;
  if (s.type === "page") return `Page ${s.id}`;
  return "Toàn shop";
}

/** Tên vai trò như trên màn: vai trò tự tạo có thêm "(tự tạo)"; khóa lạ (vai trò đã xóa) hiện nguyên khóa. */
export function roleName(key: string, roles: readonly Role[]): string {
  const r = roles.find((x) => x.key === key);
  if (r === undefined) return key;
  return isCustom(r) ? `${r.name} (tự tạo)` : r.name;
}

const DEFAULT_ROLE_KEYS = new Set(["owner", "manager", "sale", "warehouse", "accountant"]);
/** Vai trò tự tạo (không phải một trong năm vai trò mẫu). */
export const isCustom = (r: Pick<Role, "key">) => !DEFAULT_ROLE_KEYS.has(r.key);

export function grantLabel(g: StaffGrant, roles: readonly Role[]): string {
  return `${roleName(g.roleKey, roles)} · ${scopeLabel(grantScope(g))}`;
}

export const sameGrant = (a: StaffGrant, b: StaffGrant): boolean =>
  a.roleKey === b.roleKey && a.scopeType === b.scopeType && a.scopeId === b.scopeId;

/**
 * Phạm vi để chọn: Toàn shop, rồi mọi kho và Page đã có trong grant của bất kỳ nhân viên nào (xếp theo mã, không trùng).
 */
export function scopeOptions(staff: readonly StaffMember[], extra: readonly ScopeRef[] = []): ScopeRef[] {
  const seen = new Map<string, ScopeRef>();
  for (const s of [...staff.flatMap((m) => m.grants.map(grantScope)), ...extra]) {
    if (s.type !== "all") seen.set(scopeKey(s), s);
  }
  const rest = [...seen.values()].sort((a, b) => scopeKey(a).localeCompare(scopeKey(b)));
  return [ALL_SCOPE, ...rest];
}

/** Loại phạm vi của từng quyền (listPermissions); quyền không có trong bảng là chưa biết loại. */
export type PermissionKinds = ReadonlyMap<string, PermissionInfo["scopeType"]>;

/** Bảng loại phạm vi từ listPermissions; chưa đọc được (người sửa không có identity.roles.manage) → bảng rỗng. */
export function permissionKinds(list: readonly PermissionInfo[] | undefined): PermissionKinds {
  return new Map((list ?? []).map((p) => [p.key, p.scopeType]));
}

/**
 * Người sửa gán được vai trò ở phạm vi này: mỗi quyền p của vai trò thỏa identity.Can(người sửa, p, phạm vi) trên grant
 * đang hiệu lực (Me.grants): grant toàn shop luôn thỏa; quyền "shop" chỉ grant toàn shop; quyền "none" grant ở phạm vi
 * bất kỳ; quyền theo đích (và quyền chưa biết loại) grant đúng phạm vi đó. Chưa biết loại thì chặt hơn máy chủ, chỉ
 * làm mờ thừa, không mở thừa.
 */
export function canGrant(role: Role, scope: ScopeRef, actor: readonly Grant[], kinds: PermissionKinds): boolean {
  return role.permissions.every((p) => {
    const kind = kinds.get(p);
    return actor.some(
      (g) =>
        g.permission === p &&
        (g.scope.type === "all" ||
          kind === "none" ||
          (kind !== "shop" && g.scope.type === scope.type && g.scope.id === scope.id)),
    );
  });
}

export function canGrantSomewhere(
  role: Role,
  scopes: readonly ScopeRef[],
  actor: readonly Grant[],
  kinds: PermissionKinds,
): boolean {
  return scopes.some((s) => canGrant(role, s, actor, kinds));
}

const ROLE_HINTS: Readonly<Record<string, string>> = {
  owner: "Mọi quyền, gồm quản lý nhân viên, vai trò và quyền hệ thống",
  manager: "Mọi quyền trừ quản lý nhân viên, vai trò và quyền hệ thống",
  sale: "Xem sản phẩm, tồn kho; tạo đơn, giao hàng",
  warehouse: "Tồn kho, phiếu kho, kiểm kho, nhà cung cấp, giao hàng",
  accountant: "Xem sản phẩm, tồn kho, đơn, vận chuyển",
};

/** Dòng mô tả dưới tên vai trò ở mục "Thêm vai trò". */
export function roleHint(r: Role): string {
  if (isCustom(r)) return `Tự tạo · ${String(r.permissions.length)} quyền`;
  return ROLE_HINTS[r.key] ?? `${String(r.permissions.length)} quyền`;
}

const WARNING_TEXT: Readonly<Record<string, string>> = {
  admin_needs_shop_scope: "quyền quản trị chỉ hiệu lực khi gán toàn shop, các quyền đó đang không dùng được",
};
const WARNING_FALLBACK = "một số quyền của vai trò này đang không dùng được ở phạm vi này";

/** Câu cảnh báo theo mã (câu cố định của màn, không chèn chữ từ máy chủ), kèm vai trò · phạm vi. */
export function warningText(w: ScopeWarning, roles: readonly Role[]): string {
  const text = WARNING_TEXT[w.code] ?? WARNING_FALLBACK;
  const scope = scopeLabel({ type: w.scopeType, id: w.scopeId });
  const who = w.roleKey === undefined ? scope : `${roleName(w.roleKey, roles)} · ${scope}`;
  return `${who}: ${text}`;
}

/** Cảnh báo của đúng grant này (cùng vai trò và phạm vi). */
export function warningsFor(g: StaffGrant, warnings: readonly ScopeWarning[] | undefined): ScopeWarning[] {
  return (warnings ?? []).filter(
    (w) => w.roleKey === g.roleKey && w.scopeType === g.scopeType && w.scopeId === g.scopeId,
  );
}
