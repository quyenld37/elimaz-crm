// Màn Vai trò tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình; quy ước chung ở
// src/app/screens.test.tsx).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/vai-tro", "Vai trò", "Quản trị", "identity.roles.manage", false, ["/vai-tro/:id"], 20],
    ["/vai-tro/:id", "Vai trò", "Quản trị", "identity.roles.manage", true, [], 20],
  ]);
});
