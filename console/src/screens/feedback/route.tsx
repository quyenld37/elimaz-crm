// Màn Góp ý của khách (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 461 (M5-49); mockup
// docs/mockups/m5/feedback.html, menu nhóm "Khách hàng" (HV01 của docs/mockups/m5). Khối Góp ý trong hồ sơ khách ở
// CustomerFeedback.tsx (màn Khách dùng).
import type { ScreenRoute } from "../../app/loadScreens";
import { FeedbackList } from "./FeedbackList";

export const screens: readonly ScreenRoute[] = [
  // Đọc cần customer.feedback.read; đổi trạng thái, ghi chú cần customer.feedback.manage (nút tự ẩn).
  {
    path: "/khach/gop-y",
    label: "Góp ý",
    group: "Khách hàng",
    permission: "customer.feedback.read",
    order: 20,
    element: <FeedbackList />,
  },
];
