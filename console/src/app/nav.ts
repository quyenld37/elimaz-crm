// Menu của khung (mockup M1 docs/mockups/m1/khung.js): bốn nhóm cố định theo thứ tự; mỗi màn tự khai Screen trong
// route.tsx của thư mục màn (quy ước src/app/loadScreens.ts). Menu chỉ hiện màn người dùng có quyền (bất kỳ phạm vi
// nào); API vẫn kiểm quyền thật.
import type { ReactNode } from "react";
import { matchPath } from "react-router";
import type { Grant } from "../api";

export const NAV_GROUPS = ["Sản phẩm", "Kho", "Hệ thống", "Quản trị"] as const;
export type NavGroup = (typeof NAV_GROUPS)[number];

export interface Screen {
  /** Đường dẫn (mẫu của React Router, ví dụ "/san-pham/:id"). */
  path: string;
  /** Tên trên menu và thanh trên. */
  label: string;
  group: NavGroup;
  /** Khóa quyền (docs/quyen.md) cần để thấy màn. */
  permission: string;
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

export function allowedScreens(screens: readonly Screen[], grants: readonly Grant[]): Screen[] {
  return screens.filter((s) => hasPermission(grants, s.permission));
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
