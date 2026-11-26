// Màn Live và Đã chốt, thiếu địa chỉ (kế hoạch M4 Task 177; tự đăng ký, quy ước src/app/loadScreens.ts). Nhóm menu
// "Hộp chat" (khung docs/mockups/m4/khung.js). Cần live.operate (người không có quyền Live không thấy mục, mockup trạng
// thái B) và conversation.read (mọi API đọc của màn); tạo/sửa đơn khi chốt máy chủ kiểm riêng, nút tự khóa.
import type { ScreenRoute } from "../../app/loadScreens";
import { LivePage } from "./LivePage";
import { PendingDraftsPage } from "./PendingDraftsPage";

export const screens: readonly ScreenRoute[] = [
  {
    path: "/live",
    label: "Live",
    group: "Hộp chat",
    permission: "live.operate",
    alsoRequires: ["conversation.read"],
    also: ["/live/:sessionId"],
    order: 20,
    element: <LivePage />,
  },
  {
    path: "/live/:sessionId",
    label: "Phiên live",
    group: "Hộp chat",
    permission: "live.operate",
    alsoRequires: ["conversation.read"],
    hidden: true,
    order: 820,
    element: <LivePage />,
  },
  {
    path: "/live/thieu-dia-chi",
    label: "Đã chốt, thiếu địa chỉ",
    group: "Hộp chat",
    permission: "live.operate",
    alsoRequires: ["conversation.read"],
    also: ["/live/:sessionId/thieu-dia-chi"],
    order: 30,
    element: <PendingDraftsPage />,
  },
  {
    path: "/live/:sessionId/thieu-dia-chi",
    label: "Đã chốt, thiếu địa chỉ của phiên",
    group: "Hộp chat",
    permission: "live.operate",
    alsoRequires: ["conversation.read"],
    hidden: true,
    order: 830,
    element: <PendingDraftsPage />,
  },
];
