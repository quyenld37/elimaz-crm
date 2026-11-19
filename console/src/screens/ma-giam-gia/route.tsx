// Màn Mã giảm giá: danh sách, chi tiết (tự đăng ký, quy ước src/app/loadScreens.ts; Q-T57-8). Mockup
// docs/mockups/m2/ma-giam-gia.html; nhóm menu "Khuyến mãi" theo khung docs/mockups/m2/khung.js.
import type { ScreenRoute } from "../../app/loadScreens";
import { CouponDetail } from "./CouponDetail";
import { CouponList } from "./CouponList";

export const screens: readonly ScreenRoute[] = [
  // Task 87 (M2-30). Mọi API đọc của màn cần promotion.read; nút ghi hiện theo promotion.manage.
  {
    path: "/ma-giam-gia",
    label: "Mã giảm giá",
    group: "Khuyến mãi",
    permission: "promotion.read",
    order: 10,
    also: ["/ma-giam-gia/:id"],
    element: <CouponList />,
  },
  // Trang ẩn, order lớn: currentScreen lấy màn khớp đầu tiên theo order.
  {
    path: "/ma-giam-gia/:id",
    label: "Chi tiết mã giảm giá",
    group: "Khuyến mãi",
    permission: "promotion.read",
    hidden: true,
    order: 900,
    element: <CouponDetail />,
  },
];
