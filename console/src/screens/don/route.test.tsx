// Màn Đơn tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/don", "Danh sách đơn", "Đơn hàng", "order.read", false, [], 20],
    ["/don/trung", "Đơn trùng", "Đơn hàng", "order.read", false, [], 30],
    // Trang ẩn đứng sau mọi màn /don/<chữ> của nhóm: currentScreen lấy màn khớp đầu tiên theo order.
    ["/don/:id", "Chi tiết đơn", "Đơn hàng", "order.read", true, [], 900],
  ]);
});
