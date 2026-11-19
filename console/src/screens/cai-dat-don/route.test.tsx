// Màn Cài đặt đơn tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình).
import { expect, it } from "vitest";
import { canSee } from "../../app/nav";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

const grant = (permission: string) => ({ permission, scope: { type: "all" as const, id: "" } });

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/cai-dat/don", "Cài đặt đơn", "Đơn hàng", "order.settings.manage", false, [], 40],
  ]);
});

it("chỉ người có order.settings.manage và order.read thấy màn (API đọc cần order.read)", () => {
  const s = screens[0];
  if (s === undefined) throw new Error("thiếu màn");
  expect(canSee([grant("order.read")], s)).toBe(false);
  expect(canSee([grant("order.settings.manage")], s)).toBe(false);
  expect(canSee([grant("order.read"), grant("order.settings.manage")], s)).toBe(true);
});
