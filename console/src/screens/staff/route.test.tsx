// Màn Nhân viên tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình; quy ước chung ở
// src/app/screens.test.tsx).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/nhan-vien", "Nhân viên", "Quản trị", "identity.users.manage", false, ["/nhan-vien/:id/quyen"], 10],
    ["/nhan-vien/:id/quyen", "Nhân viên", "Quản trị", "identity.users.manage", true, [], 10],
  ]);
});
