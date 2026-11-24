// Mục menu "Hãng trên web" (nhóm Website) tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389). Nó mở màn Kết nối
// vận chuyển (cần cùng quyền) và cuộn tới thẻ Hãng trên website, không phải màn thứ hai.
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens as carrierScreens } from "../carriers/route";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm Website, quyền như màn Kết nối vận chuyển, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/website/hang-tren-web", "Hãng trên web", "Website", "fulfillment.carrier_accounts.read", false, [], 30],
  ]);
  expect(screens[0]?.alsoRequires).toEqual(carrierScreens[0]?.alsoRequires);
});
