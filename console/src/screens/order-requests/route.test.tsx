// Màn Yêu cầu về đơn hàng tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/don/yeu-cau", "Yêu cầu về đơn hàng", "Đơn hàng", "order.requests.read", false, ["/don/yeu-cau/:id"], 35],
    ["/don/yeu-cau/:id", "Chi tiết yêu cầu về đơn", "Đơn hàng", "order.requests.read", true, [], 800],
  ]);
});
