// Màn Banner trang chủ (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 459 (M5-47); mockup
// docs/mockups/m5/web-banners.html, nhóm menu "Website" có sẵn (Task 208), thứ tự HV01: Banner trang chủ đứng đầu.
import type { ScreenRoute } from "../../app/loadScreens";
import { BannerEditor } from "./BannerEditor";

export const screens: readonly ScreenRoute[] = [
  // Đọc (getBanners, previewBanners) cần content.read; sửa content.manage, tải ảnh files.upload, tìm mẫu catalog.read
  // (màn khóa phần thiếu quyền, API kiểm).
  {
    path: "/website/banner",
    label: "Banner trang chủ",
    group: "Website",
    permission: "content.read",
    order: 10,
    element: <BannerEditor />,
  },
];
