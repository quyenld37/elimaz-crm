// Màn Live (kế hoạch M4 Task 37; mockup docs/mockups/m4/live.html, thieu-dia-chi.html). Task 174 dựng đường, mục menu
// (nhóm "Hộp chat", khung.js) và nạp lười tệp khung; Task 177 làm nội dung. /live/thieu-dia-chi là mục menu (mọi phiên),
// /live/:sessionId/thieu-dia-chi là danh sách của một phiên.
import { lazy, Suspense } from "react";
import type { ScreenRoute } from "../../app/loadScreens";

const LiveScreen = lazy(() => import("./index"));

const page = (title: string) => (
  <Suspense fallback={null}>
    <LiveScreen title={title} />
  </Suspense>
);

export const screens: readonly ScreenRoute[] = [
  { path: "/live", label: "Live", group: "Hộp chat", permission: "live.operate", order: 20, element: page("Live") },
  {
    path: "/live/thieu-dia-chi",
    label: "Đã chốt, thiếu địa chỉ",
    group: "Hộp chat",
    permission: "live.operate",
    order: 30,
    element: page("Đã chốt, thiếu địa chỉ"),
  },
  // Trang ẩn đứng sau /live/thieu-dia-chi: currentScreen lấy màn khớp đầu tiên theo order.
  {
    path: "/live/:sessionId",
    label: "Phiên live",
    group: "Hộp chat",
    permission: "live.operate",
    hidden: true,
    order: 910,
    element: page("Live"),
  },
  {
    path: "/live/:sessionId/thieu-dia-chi",
    label: "Đã chốt, thiếu địa chỉ",
    group: "Hộp chat",
    permission: "live.operate",
    hidden: true,
    order: 920,
    element: page("Đã chốt, thiếu địa chỉ"),
  },
];
