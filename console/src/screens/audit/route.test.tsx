// Màn Nhật ký thao tác tự đăng ký đúng đường, quyền, chỗ trên menu (nhóm Quản trị, sau Nhân viên, Vai trò; HV01).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/nhat-ky-thao-tac", "Nhật ký thao tác", "Quản trị", "audit.read", false, [], 30],
  ]);
});
