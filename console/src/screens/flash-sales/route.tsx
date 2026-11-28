// Màn Đợt flash sale: danh sách và trang một đợt (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 206
// (M5-23); mockup docs/mockups/m5/flash-sales.html (Task 514), HV26–HV31. Mục menu "Flash sale" ở nhóm "Khuyến mãi".
import type { ScreenRoute } from "../../app/loadScreens";
import { FlashSaleEditor } from "./FlashSaleEditor";
import { FlashSaleList } from "./FlashSaleList";

export const screens: readonly ScreenRoute[] = [
  // Mọi API đọc của màn cần promotion.flash.read; nút ghi hiện theo promotion.flash.manage.
  {
    path: "/khuyen-mai/flash-sale",
    label: "Flash sale",
    group: "Khuyến mãi",
    permission: "promotion.flash.read",
    also: ["/khuyen-mai/flash-sale/:id"],
    order: 40,
    element: <FlashSaleList />,
  },
  {
    path: "/khuyen-mai/flash-sale/:id",
    label: "Đợt flash sale",
    group: "Khuyến mãi",
    permission: "promotion.flash.read",
    hidden: true,
    order: 900,
    element: <FlashSaleEditor />,
  },
];
