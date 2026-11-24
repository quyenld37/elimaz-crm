// Màn Nhận đơn web (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 462 (M5-50); mockup
// docs/mockups/m5/web-intake.html, menu nhóm "Website" giữa Hãng trên web và Khóa dịch vụ (HV01, HV23).
import type { ScreenRoute } from "../../app/loadScreens";
import { WebIntakeScreen } from "./WebIntake";

export const screens: readonly ScreenRoute[] = [
  // Xem cần order.read; tạm dừng, mở lại cần order.web_intake.manage (nút khóa kèm câu; API kiểm).
  {
    path: "/website/nhan-don-web",
    label: "Nhận đơn web",
    group: "Website",
    permission: "order.read",
    order: 40,
    element: <WebIntakeScreen />,
  },
];
