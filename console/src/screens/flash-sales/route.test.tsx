// Màn Đợt flash sale tự đăng ký đúng đường, quyền, chỗ trên menu (Task 206; HV26).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    [
      "/khuyen-mai/flash-sale",
      "Flash sale",
      "Khuyến mãi",
      "promotion.flash.read",
      false,
      ["/khuyen-mai/flash-sale/:id"],
      40,
    ],
    ["/khuyen-mai/flash-sale/:id", "Đợt flash sale", "Khuyến mãi", "promotion.flash.read", true, [], 900],
  ]);
});
