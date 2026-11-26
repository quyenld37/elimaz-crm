// Khung màn Kênh kết nối và Cài đặt hộp chat (Task 174 dựng đường và mục menu; nội dung ở Task 178).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/settings/channels", "Kênh kết nối", "Hộp chat", "channel.manage", false, [], 40],
    ["/settings/inbox", "Cài đặt hộp chat", "Hộp chat", "conversation.settings", false, [], 50],
    ["/settings/channels/facebook/callback", "Kết nối Page Facebook", "Hộp chat", "channel.manage", true, [], 930],
    ["/settings/pages/:pageId", "Cài đặt Page", "Hộp chat", "conversation.settings", true, [], 940],
    ["/settings/tags", "Nhãn hội thoại", "Hộp chat", "conversation.settings", true, [], 950],
    ["/settings/templates", "Mẫu trả lời", "Hộp chat", "conversation.settings", true, [], 960],
  ]);
});
