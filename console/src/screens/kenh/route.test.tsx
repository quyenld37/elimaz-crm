// Màn Kênh kết nối và Cài đặt hộp chat tự đăng ký đúng đường, quyền, chỗ trên menu (khung của Task 174; Task 178).
import { expect, it } from "vitest";
import { canSee, isCurrent } from "../../app/nav";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

const grant = (permission: string) => ({ permission, scope: { type: "all" as const, id: "" } });
const TABS = ["/settings/pages/:pageId", "/settings/tags", "/settings/templates"];

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    [
      "/settings/channels",
      "Kênh kết nối",
      "Hộp chat",
      "channel.manage",
      false,
      ["/settings/channels/facebook/callback"],
      40,
    ],
    ["/settings/inbox", "Cài đặt hộp chat", "Hộp chat", "conversation.settings", false, TABS, 50],
    ["/settings/channels/facebook/callback", "Kết nối Page Facebook", "Hộp chat", "channel.manage", true, [], 930],
    ["/settings/pages/:pageId", "Cài đặt Page", "Hộp chat", "conversation.settings", true, [], 940],
    ["/settings/tags", "Nhãn hội thoại", "Hộp chat", "conversation.settings", true, [], 950],
    ["/settings/templates", "Mẫu trả lời", "Hộp chat", "conversation.settings", true, [], 960],
  ]);
});

it("mọi màn cần conversation.read (API đọc của màn) cùng quyền chính", () => {
  for (const s of screens) {
    expect(canSee([grant(s.permission)], s)).toBe(false);
    expect(canSee([grant(s.permission), grant("conversation.read")], s)).toBe(true);
  }
});

it("mục menu Cài đặt hộp chat sáng ở các thẻ; Kênh kết nối sáng ở trang callback", () => {
  const [channels, inbox] = screens;
  if (channels === undefined || inbox === undefined) throw new Error("thiếu màn");
  expect(isCurrent(inbox, "/settings/tags")).toBe(true);
  expect(isCurrent(inbox, "/settings/pages/abc")).toBe(true);
  expect(isCurrent(channels, "/settings/channels/facebook/callback")).toBe(true);
});
