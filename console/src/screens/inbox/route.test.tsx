// Màn Hộp chat tự đăng ký đúng đường, quyền, chỗ trên menu (Task 174; HV01: nhóm "Hộp chat" đứng đầu menu).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/inbox", "Hộp chat", "Hộp chat", "conversation.read", false, ["/inbox/:conversationId"], 10],
    ["/inbox/:conversationId", "Hội thoại", "Hộp chat", "conversation.read", true, [], 900],
  ]);
});
