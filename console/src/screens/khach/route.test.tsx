// Màn Khách tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/khach", "Khách hàng", "Khách hàng", "customer.read", false, ["/khach/:id", "/khach/:id/gop"], 10],
    // Trang ẩn: hồ sơ và gộp, order lớn để /khach khớp trước.
    ["/khach/:id", "Hồ sơ khách", "Khách hàng", "customer.read", true, [], 900],
    ["/khach/:id/gop", "Gộp khách", "Khách hàng", "customer.read", true, [], 910],
  ]);
});
