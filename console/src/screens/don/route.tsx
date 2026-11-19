// Màn Đơn: danh sách, chi tiết, đơn trùng (tự đăng ký, quy ước src/app/loadScreens.ts; Q-T57-8). Mockup
// docs/mockups/m2/don-danh-sach.html, don-chi-tiet.html, don-trung.html.
import type { ScreenRoute } from "../../app/loadScreens";
import { DuplicatePairs } from "./DuplicatePairs";
import { OrderDetail } from "./OrderDetail";
import { OrderList } from "./OrderList";

export const screens: readonly ScreenRoute[] = [
  // Task 85 (M2-28). Mọi API đọc của màn cần order.read; nút ghi hiện theo quyền riêng của từng việc.
  {
    path: "/don",
    label: "Danh sách đơn",
    group: "Đơn hàng",
    permission: "order.read",
    order: 20,
    element: <OrderList />,
  },
  {
    path: "/don/trung",
    label: "Đơn trùng",
    group: "Đơn hàng",
    permission: "order.read",
    order: 30,
    element: <DuplicatePairs />,
  },
  // Trang ẩn, order lớn: currentScreen lấy màn khớp đầu tiên theo order, nên /don/moi, /don/trung không bị nhận nhầm
  // là chi tiết đơn.
  {
    path: "/don/:id",
    label: "Chi tiết đơn",
    group: "Đơn hàng",
    permission: "order.read",
    hidden: true,
    order: 900,
    element: <OrderDetail />,
  },
];
