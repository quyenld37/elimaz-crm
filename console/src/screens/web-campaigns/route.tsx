// Màn Chiến dịch giảm giá: danh sách, tạo, sửa (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 458
// (M5-46); mockup docs/mockups/m5/web-campaigns.html, menu nhóm "Khuyến mãi" có sẵn (HV01 của docs/mockups/m5).
import type { ScreenRoute } from "../../app/loadScreens";
import { CampaignEditor } from "./CampaignEditor";
import { CampaignList } from "./CampaignList";

export const screens: readonly ScreenRoute[] = [
  // Mọi API đọc của màn cần promotion.read; nút ghi hiện theo promotion.manage.
  {
    path: "/khuyen-mai/chien-dich",
    label: "Chiến dịch giảm giá",
    group: "Khuyến mãi",
    permission: "promotion.read",
    also: ["/khuyen-mai/chien-dich/moi", "/khuyen-mai/chien-dich/:id"],
    order: 20,
    element: <CampaignList />,
  },
  // Trang ẩn, order lớn: currentScreen lấy màn khớp đầu tiên theo order ("moi" đứng trước ":id").
  {
    path: "/khuyen-mai/chien-dich/moi",
    label: "Tạo chiến dịch giảm giá",
    group: "Khuyến mãi",
    permission: "promotion.read",
    hidden: true,
    order: 900,
    element: <CampaignEditor />,
  },
  {
    path: "/khuyen-mai/chien-dich/:id",
    label: "Sửa chiến dịch giảm giá",
    group: "Khuyến mãi",
    permission: "promotion.read",
    hidden: true,
    order: 901,
    element: <CampaignEditor />,
  },
];
