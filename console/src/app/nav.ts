// Menu của khung (mockup M1 docs/mockups/m1/khung.js; M2 thêm "Đơn hàng", "Khách hàng", "Khuyến mãi" đứng đầu, HV01 của docs/mockups/m2): các nhóm cố định theo thứ tự; mỗi màn tự khai Screen trong
// route.tsx của thư mục màn (quy ước src/app/loadScreens.ts). Menu chỉ hiện màn người dùng có quyền (bất kỳ phạm vi
// nào); API vẫn kiểm quyền thật.
import type { ReactNode } from "react";
import { matchPath } from "react-router";
import type { Grant } from "../api";

export const NAV_GROUPS = ["Đơn hàng", "Khách hàng", "Khuyến mãi", "Sản phẩm", "Kho", "Hệ thống", "Quản trị"] as const;
export type NavGroup = (typeof NAV_GROUPS)[number];

export interface Screen {
  /** Đường dẫn (mẫu của React Router, ví dụ "/san-pham/:id"). */
  path: string;
  /** Tên trên menu và thanh trên. */
  label: string;
  group: NavGroup;
  /** Khóa quyền (docs/quyen.md) cần để thấy màn. */
  permission: string;
  /** Quyền khác mà API của màn cũng đòi (thiếu một cái thì màn không chạy được): cần đủ mới thấy màn. */
  alsoRequires?: readonly string[];
  /** Đường dẫn khác làm sáng mục này trên menu (trang con, ví dụ sửa mẫu → Danh sách sản phẩm). */
  also?: string[];
  /** Có route nhưng không có mục trên menu (trang con). */
  hidden?: boolean;
  element: ReactNode;
}

/** Có quyền trên ít nhất một phạm vi (đủ để thấy màn; màn tự lọc theo phạm vi). */
export function hasPermission(grants: readonly Grant[], permission: string): boolean {
  return grants.some((g) => g.permission === permission);
}

/** Thấy được màn: có quyền chính và mọi quyền đi kèm (alsoRequires), mỗi quyền ở phạm vi bất kỳ. */
export function canSee(grants: readonly Grant[], s: Screen): boolean {
  return [s.permission, ...(s.alsoRequires ?? [])].every((p) => hasPermission(grants, p));
}

export function allowedScreens(screens: readonly Screen[], grants: readonly Grant[]): Screen[] {
  return screens.filter((s) => canSee(grants, s));
}

export interface NavSection {
  group: NavGroup;
  items: Screen[];
}

/** Các nhóm có ít nhất một mục được thấy, theo thứ tự NAV_GROUPS. */
export function navSections(screens: readonly Screen[], grants: readonly Grant[]): NavSection[] {
  const visible = allowedScreens(screens, grants).filter((s) => s.hidden !== true);
  return NAV_GROUPS.map((group) => ({ group, items: visible.filter((s) => s.group === group) })).filter(
    (s) => s.items.length > 0,
  );
}

/** Màn đang mở (khớp path, kể cả trang ẩn), để đặt tên ở thanh trên. */
export function currentScreen(screens: readonly Screen[], pathname: string): Screen | undefined {
  return screens.find((s) => matchPath(s.path, pathname) !== null);
}

/** Mục menu đang sáng: khớp path hoặc một trong also. */
export function isCurrent(s: Screen, pathname: string): boolean {
  return [s.path, ...(s.also ?? [])].some((p) => matchPath(p, pathname) !== null);
}
