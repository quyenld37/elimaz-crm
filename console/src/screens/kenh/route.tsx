// Màn Kênh kết nối và Cài đặt hộp chat của nhóm "Hộp chat" (kế hoạch M4 Task 178; mockup docs/mockups/m4/
// ket-noi-page.html, cai-dat.html). Tự đăng ký theo quy ước src/app/loadScreens.ts; đường và chỗ trên menu theo khung
// của Task 174 (kenh/route.test.tsx). API đọc của các màn cần conversation.read (alsoRequires).
import type { ScreenRoute } from "../../app/loadScreens";
import { ChannelsPage } from "./ChannelsPage";
import { FacebookCallback } from "./FacebookCallback";
import { InboxSettingsHome, PageSettingsPage, SettingsShell } from "./PageSettingsPage";
import { TagsPage } from "./TagsPage";
import { TemplatesPage } from "./TemplatesPage";

const SETTINGS_TABS = ["/settings/pages/:pageId", "/settings/tags", "/settings/templates"];

export const screens: readonly ScreenRoute[] = [
  {
    path: "/settings/channels",
    label: "Kênh kết nối",
    group: "Hộp chat",
    permission: "channel.manage",
    alsoRequires: ["conversation.read"],
    also: ["/settings/channels/facebook/callback"],
    order: 40,
    element: <ChannelsPage />,
  },
  {
    path: "/settings/inbox",
    label: "Cài đặt hộp chat",
    group: "Hộp chat",
    permission: "conversation.settings",
    alsoRequires: ["conversation.read"],
    also: SETTINGS_TABS,
    order: 50,
    element: (
      <SettingsShell>
        <InboxSettingsHome />
      </SettingsShell>
    ),
  },
  {
    path: "/settings/channels/facebook/callback",
    label: "Kết nối Page Facebook",
    group: "Hộp chat",
    permission: "channel.manage",
    alsoRequires: ["conversation.read"],
    hidden: true,
    order: 930,
    element: <FacebookCallback />,
  },
  {
    path: "/settings/pages/:pageId",
    label: "Cài đặt Page",
    group: "Hộp chat",
    permission: "conversation.settings",
    alsoRequires: ["conversation.read"],
    hidden: true,
    order: 940,
    element: <PageSettingsPage />,
  },
  {
    path: "/settings/tags",
    label: "Nhãn hội thoại",
    group: "Hộp chat",
    permission: "conversation.settings",
    alsoRequires: ["conversation.read"],
    hidden: true,
    order: 950,
    element: <TagsPage />,
  },
  {
    path: "/settings/templates",
    label: "Mẫu trả lời",
    group: "Hộp chat",
    permission: "conversation.settings",
    alsoRequires: ["conversation.read"],
    hidden: true,
    order: 960,
    element: <TemplatesPage />,
  },
];
