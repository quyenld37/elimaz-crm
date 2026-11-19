// Màn Khách: danh sách, hồ sơ, gộp (tự đăng ký, quy ước src/app/loadScreens.ts; Q-T57-8). Mockup
// docs/mockups/m2/khach-danh-sach.html, khach-ho-so.html, khach-gop.html.
import type { ScreenRoute } from "../../app/loadScreens";
import { CustomerList } from "./CustomerList";
import { CustomerProfile } from "./CustomerProfile";
import { MergeView } from "./MergeView";

export const screens: readonly ScreenRoute[] = [
  // Task 86 (M2-29). Mọi API đọc của màn cần customer.read; nút ghi hiện theo quyền riêng của từng việc.
  {
    path: "/khach",
    label: "Khách hàng",
    group: "Khách hàng",
    permission: "customer.read",
    order: 10,
    also: ["/khach/:id", "/khach/:id/gop"],
    element: <CustomerList />,
  },
  // Trang ẩn, order lớn: currentScreen lấy màn khớp đầu tiên theo order.
  {
    path: "/khach/:id",
    label: "Hồ sơ khách",
    group: "Khách hàng",
    permission: "customer.read",
    hidden: true,
    order: 900,
    element: <CustomerProfile />,
  },
  {
    path: "/khach/:id/gop",
    label: "Gộp khách",
    group: "Khách hàng",
    permission: "customer.read",
    hidden: true,
    order: 910,
    element: <MergeView />,
  },
];
