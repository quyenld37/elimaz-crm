// Kiểm và xếp các màn tự đăng ký (Task 393, M1-x2). Quy ước: mỗi màn là thư mục src/screens/<tên>/ có route.tsx xuất
//   export const screens: readonly ScreenRoute[] = [{ path, label, group, permission, order, element, also?, hidden?,
//   alsoRequires? }];
// src/app/screens.ts nạp mọi route.tsx bằng import.meta.glob; không có danh sách màn viết tay nào khác. Tệp sai quy
// ước (thiếu hay sai trường, trùng path giữa hai tệp) làm hỏng lần nạp với lỗi nêu tên tệp: khung không chạy với một
// màn thiếu quyền hay hai màn giành một path.
import { isValidElement } from "react";
import { NAV_GROUPS, type NavGroup, type Screen } from "./nav";

/** Một màn trong route.tsx: Screen cộng thứ tự trên menu trong nhóm (số nhỏ đứng trước). */
export interface ScreenRoute extends Screen {
  order: number;
}

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;
const isText = (v: unknown): v is string => typeof v === "string" && v.trim() !== "";
const isGroup = (v: unknown): v is NavGroup => NAV_GROUPS.some((g) => g === v);

function parseScreen(file: string, i: number, v: unknown): { screen: Screen; order: number } {
  const fail = (field: string, rule: string) =>
    new Error(`${file}: screens[${String(i)}].${field} ${rule} (quy ước màn: src/app/loadScreens.ts)`);
  if (!isRecord(v)) throw fail("", "phải là đối tượng ScreenRoute");
  const { path, label, group, permission, order, element, also, hidden, alsoRequires } = v;
  if (!isText(path) || !path.startsWith("/")) throw fail("path", "phải là chuỗi bắt đầu bằng /");
  if (!isText(label)) throw fail("label", "phải là chuỗi không rỗng");
  if (!isGroup(group)) throw fail("group", `phải là một trong ${NAV_GROUPS.join(", ")}`);
  if (!isText(permission)) throw fail("permission", "phải là khóa quyền không rỗng (docs/quyen.md)");
  if (typeof order !== "number" || !Number.isFinite(order)) throw fail("order", "phải là số");
  if (!isValidElement(element)) throw fail("element", "phải là phần tử React");
  if (also !== undefined && !(Array.isArray(also) && also.every(isText))) throw fail("also", "phải là mảng chuỗi");
  if (hidden !== undefined && typeof hidden !== "boolean") throw fail("hidden", "phải là true hoặc false");
  if (alsoRequires !== undefined && !(Array.isArray(alsoRequires) && alsoRequires.every(isText))) {
    throw fail("alsoRequires", "phải là mảng khóa quyền không rỗng (docs/quyen.md)");
  }
  const screen: Screen = {
    path,
    label,
    group,
    permission,
    element,
    ...(also === undefined ? {} : { also: also.filter(isText) }),
    ...(hidden === undefined ? {} : { hidden }),
    ...(alsoRequires === undefined ? {} : { alsoRequires: alsoRequires.filter(isText) }),
  };
  return { screen, order };
}

/**
 * Từ các mô-đun route.tsx (khóa là đường tệp, như import.meta.glob trả) ra danh sách màn, xếp theo nhóm menu
 * (NAV_GROUPS) rồi order rồi path. Ném lỗi nêu tên tệp khi một tệp sai quy ước hay hai màn trùng path.
 */
export function loadScreens(modules: Record<string, unknown>): Screen[] {
  const owner = new Map<string, string>();
  const all: { screen: Screen; order: number }[] = [];
  for (const file of Object.keys(modules).sort()) {
    const mod = modules[file];
    const list = isRecord(mod) ? mod.screens : undefined;
    if (!Array.isArray(list) || list.length === 0) {
      throw new Error(
        `${file}: phải xuất \`screens\` là mảng ScreenRoute không rỗng (quy ước màn: src/app/loadScreens.ts)`,
      );
    }
    list.forEach((v: unknown, i) => {
      const s = parseScreen(file, i, v);
      const other = owner.get(s.screen.path);
      if (other !== undefined) throw new Error(`trùng path "${s.screen.path}": ${other} và ${file}`);
      owner.set(s.screen.path, file);
      all.push(s);
    });
  }
  const rank = (g: NavGroup) => NAV_GROUPS.indexOf(g);
  return all
    .sort(
      (a, b) =>
        rank(a.screen.group) - rank(b.screen.group) || a.order - b.order || a.screen.path.localeCompare(b.screen.path),
    )
    .map((s) => s.screen);
}
