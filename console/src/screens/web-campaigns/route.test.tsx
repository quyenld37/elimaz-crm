// Màn Chiến dịch giảm giá tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    [
      "/khuyen-mai/chien-dich",
      "Chiến dịch giảm giá",
      "Khuyến mãi",
      "promotion.read",
      false,
      ["/khuyen-mai/chien-dich/moi", "/khuyen-mai/chien-dich/:id"],
      20,
    ],
    ["/khuyen-mai/chien-dich/moi", "Tạo chiến dịch giảm giá", "Khuyến mãi", "promotion.read", true, [], 900],
    ["/khuyen-mai/chien-dich/:id", "Sửa chiến dịch giảm giá", "Khuyến mãi", "promotion.read", true, [], 901],
  ]);
});
