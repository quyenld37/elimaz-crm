// Màn Quà sinh nhật (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 209 (M5-26, mở rộng bốn tab ở Task
// 450); mockup docs/mockups/m5/birthday-gifts.html, crumb "Khuyến mãi / Quà sinh nhật".
import type { ScreenRoute } from "../../app/loadScreens";
import { BirthdayGifts } from "./BirthdayGifts";

export const screens: readonly ScreenRoute[] = [
  // Mọi API đọc của màn cần promotion.read; nút ghi hiện theo promotion.birthday.manage.
  {
    path: "/khuyen-mai/qua-sinh-nhat",
    label: "Quà sinh nhật",
    group: "Khuyến mãi",
    permission: "promotion.read",
    order: 30,
    element: <BirthdayGifts />,
  },
];
