// Màn Nhận đơn web tự đăng ký đúng đường, quyền, chỗ trên menu (nhóm Website, giữa Hãng trên web và Khóa dịch vụ;
// HV01, HV23).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/website/nhan-don-web", "Nhận đơn web", "Website", "order.read", false, [], 40],
  ]);
});
