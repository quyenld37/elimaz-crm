// Màn Yêu cầu về đơn hàng: danh sách, chi tiết (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 207
// (M5-24); mockup docs/mockups/m5/order-requests.html, menu nhóm "Đơn hàng" có sẵn (HV01 của docs/mockups/m5).
import type { ScreenRoute } from "../../app/loadScreens";
import { RequestDetail } from "./RequestDetail";
import { RequestList } from "./RequestList";

export const screens: readonly ScreenRoute[] = [
  // Mọi API đọc của màn cần order.requests.read; trả lời, đổi trạng thái theo order.requests.manage (nút tự ẩn).
  {
    path: "/don/yeu-cau",
    label: "Yêu cầu về đơn hàng",
    group: "Đơn hàng",
    permission: "order.requests.read",
    also: ["/don/yeu-cau/:id"],
    order: 35,
    element: <RequestList />,
  },
  {
    path: "/don/yeu-cau/:id",
    label: "Chi tiết yêu cầu về đơn",
    group: "Đơn hàng",
    permission: "order.requests.read",
    hidden: true,
    order: 800,
    element: <RequestDetail />,
  },
];
