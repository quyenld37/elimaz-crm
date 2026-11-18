// Màn Sản phẩm tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình; quy ước chung ở
// src/app/screens.test.tsx).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/san-pham", "Danh sách sản phẩm", "Sản phẩm", "catalog.read", false, ["/san-pham/:id"], 10],
    ["/san-pham/:id", "Sửa mẫu", "Sản phẩm", "catalog.read", true, [], 10],
  ]);
});
