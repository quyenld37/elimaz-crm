// Khung màn Live (Task 174 dựng đường và mục menu; nội dung ở Task 177).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/live", "Live", "Hộp chat", "live.operate", false, [], 20],
    ["/live/thieu-dia-chi", "Đã chốt, thiếu địa chỉ", "Hộp chat", "live.operate", false, [], 30],
    ["/live/:sessionId", "Phiên live", "Hộp chat", "live.operate", true, [], 910],
    ["/live/:sessionId/thieu-dia-chi", "Đã chốt, thiếu địa chỉ", "Hộp chat", "live.operate", true, [], 920],
  ]);
});
