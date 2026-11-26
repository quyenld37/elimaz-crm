// Màn Kênh kết nối và Cài đặt hộp chat (kế hoạch M4 Task 38; mockup docs/mockups/m4/ket-noi-page.html, cai-dat.html).
// Task 174 dựng đường, mục menu (nhóm "Hộp chat", khung.js) và nạp lười tệp khung; Task 178 làm nội dung. Quyền theo
// docs/quyen.md: kết nối, chế độ gửi cần channel.manage; cài đặt, nhãn, mẫu cần conversation.settings.
import { lazy, Suspense } from "react";
import type { ScreenRoute } from "../../app/loadScreens";

const SettingsScreen = lazy(() => import("./index"));

const page = (title: string) => (
  <Suspense fallback={null}>
    <SettingsScreen title={title} />
  </Suspense>
);

const CHANNELS = "channel.manage";
const SETTINGS = "conversation.settings";

export const screens: readonly ScreenRoute[] = [
  {
    path: "/settings/channels",
    label: "Kênh kết nối",
    group: "Hộp chat",
    permission: CHANNELS,
    order: 40,
    element: page("Kênh kết nối"),
  },
  {
    path: "/settings/inbox",
    label: "Cài đặt hộp chat",
    group: "Hộp chat",
    permission: SETTINGS,
    order: 50,
    element: page("Cài đặt hộp chat"),
  },
  // Trang ẩn: quay về từ Facebook sau khi đăng nhập (Task 149), cài đặt của một Page, nhãn, mẫu trả lời.
  {
    path: "/settings/channels/facebook/callback",
    label: "Kết nối Page Facebook",
    group: "Hộp chat",
    permission: CHANNELS,
    hidden: true,
    order: 930,
    element: page("Kết nối Page Facebook"),
  },
  {
    path: "/settings/pages/:pageId",
    label: "Cài đặt Page",
    group: "Hộp chat",
    permission: SETTINGS,
    hidden: true,
    order: 940,
    element: page("Cài đặt Page"),
  },
  {
    path: "/settings/tags",
    label: "Nhãn hội thoại",
    group: "Hộp chat",
    permission: SETTINGS,
    hidden: true,
    order: 950,
    element: page("Nhãn hội thoại"),
  },
  {
    path: "/settings/templates",
    label: "Mẫu trả lời",
    group: "Hộp chat",
    permission: SETTINGS,
    hidden: true,
    order: 960,
    element: page("Mẫu trả lời"),
  },
];
