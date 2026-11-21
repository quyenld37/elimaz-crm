// Màn Kho M3 tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order (menu Kho: Tồn kho 10, Đóng gói, Nhận hàng hoàn, Phiếu kho 20)", () => {
  expect(routeRows(screens)).toEqual([
    ["/warehouse/pack", "Đóng gói", "Kho", "fulfillment.pack", false, ["/warehouse/picking-list"], 12],
    ["/warehouse/picking-list", "Phiếu nhặt hàng", "Kho", "fulfillment.print", true, [], 13],
    [
      "/warehouse/returns/receive",
      "Nhận hàng hoàn",
      "Kho",
      "order.return.receive",
      false,
      ["/warehouse/returns/receive/:returnId"],
      14,
    ],
    ["/warehouse/returns/receive/:returnId", "Nhận hàng hoàn", "Kho", "order.return.receive", true, [], 15],
  ]);
  expect(screens.map((s) => s.alsoRequires ?? [])).toEqual([
    ["order.read", "inventory.read"],
    [],
    ["order.read"],
    ["order.read"],
  ]);
});
