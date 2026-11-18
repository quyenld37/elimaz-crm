// Dùng trong test đăng ký màn (src/screens/<tên>/route.test.tsx, Task 389): một màn khai trong route.tsx thành một
// dòng [path, label, group, permission, hidden, also, order] để so với bảng mong đợi của màn đó.
import type { ScreenRoute } from "./loadScreens";

export type RouteRow = [string, string, string, string, boolean, readonly string[], number];

export function routeRows(screens: readonly ScreenRoute[]): RouteRow[] {
  return screens.map((s) => [s.path, s.label, s.group, s.permission, s.hidden === true, s.also ?? [], s.order]);
}
