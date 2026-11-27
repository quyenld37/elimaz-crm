// Màn Đối soát COD tự đăng ký đúng đường, quyền, chỗ trên menu (nhóm "Tiền", HV01 của mockup M3).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/cod/statements", "Đối soát COD", "Tiền", "cash.cod.read", false, ["/cod/statements/:id"], 10],
    ["/cod/discrepancies", "Lệch đối soát", "Tiền", "cash.cod.read", false, [], 20],
    ["/cod/overdue", "Quá hạn chưa có tiền", "Tiền", "cash.cod.read", false, [], 30],
    ["/cod/statements/:id", "Bảng kê COD", "Tiền", "cash.cod.read", true, [], 900],
  ]);
});
