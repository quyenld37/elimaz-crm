// Màn Live (Task 174 dựng đường và mục menu; Task 177 làm nội dung, thêm also và alsoRequires conversation.read).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/live", "Live", "Hộp chat", "live.operate", false, ["/live/:sessionId"], 20],
    ["/live/:sessionId", "Phiên live", "Hộp chat", "live.operate", true, [], 820],
    [
      "/live/thieu-dia-chi",
      "Đã chốt, thiếu địa chỉ",
      "Hộp chat",
      "live.operate",
      false,
      ["/live/:sessionId/thieu-dia-chi"],
      30,
    ],
    ["/live/:sessionId/thieu-dia-chi", "Đã chốt, thiếu địa chỉ của phiên", "Hộp chat", "live.operate", true, [], 830],
  ]);
  expect(screens.map((s) => s.alsoRequires)).toEqual([
    ["conversation.read"],
    ["conversation.read"],
    ["conversation.read"],
    ["conversation.read"],
  ]);
});
