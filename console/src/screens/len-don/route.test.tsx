// Màn Lên đơn tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([["/don/moi", "Lên đơn", "Đơn hàng", "order.create", false, [], 10]]);
  expect(screens[0]?.alsoRequires).toEqual(["customer.read", "catalog.read", "order.read"]);
});
