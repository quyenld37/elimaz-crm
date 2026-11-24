// Màn Bộ phối (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 460 (M5-48); mockup
// docs/mockups/m5/web-looks.html, nhóm menu "Website", thứ tự HV01: Banner trang chủ (10), Bộ phối (20), Hãng trên web (30).
import type { ScreenRoute } from "../../app/loadScreens";
import { LookEditor } from "./LookEditor";

export const screens: readonly ScreenRoute[] = [
  // Đọc (getLooks, previewLooks) cần content.read; sửa content.manage, tìm mẫu và đọc màu, ảnh của mẫu catalog.read
  // (màn khóa phần thiếu quyền, API kiểm).
  {
    path: "/website/bo-phoi",
    label: "Bộ phối",
    group: "Website",
    permission: "content.read",
    order: 20,
    element: <LookEditor />,
  },
];
