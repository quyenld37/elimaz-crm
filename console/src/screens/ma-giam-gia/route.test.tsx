// Màn Mã giảm giá tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/ma-giam-gia", "Mã giảm giá", "Khuyến mãi", "promotion.read", false, ["/ma-giam-gia/:id"], 10],
    ["/ma-giam-gia/:id", "Chi tiết mã giảm giá", "Khuyến mãi", "promotion.read", true, [], 900],
  ]);
});
