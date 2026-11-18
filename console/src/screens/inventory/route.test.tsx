// Màn Kho tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình; quy ước chung ở
// src/app/screens.test.tsx).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/kho/ton", "Tồn kho", "Kho", "inventory.read", false, [], 10],
    // Task 328: Phiếu kho (danh sách trên menu, trang soạn ẩn).
    ["/kho/phieu", "Phiếu kho", "Kho", "inventory.read", false, ["/kho/phieu/:id"], 20],
    ["/kho/phieu/:id", "Phiếu kho", "Kho", "inventory.read", true, [], 20],
    // Task 327: Nhà cung cấp.
    ["/kho/nha-cung-cap", "Nhà cung cấp", "Kho", "inventory.read", false, [], 40],
  ]);
});
