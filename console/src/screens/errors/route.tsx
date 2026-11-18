// Màn Sự kiện lỗi (tự đăng ký, quy ước src/app/loadScreens.ts).
import type { ScreenRoute } from "../../app/loadScreens";
import { ErrorsPage } from "./ErrorsPage";

export const screens: readonly ScreenRoute[] = [
  // Task 330: Sự kiện lỗi (mockup docs/mockups/m1/su-kien-loi.html). Thứ tự trong nhóm theo khung.js: Sức khỏe hệ
  // thống, Sự kiện lỗi, Tệp bị mất.
  {
    path: "/he-thong/su-kien-loi",
    label: "Sự kiện lỗi",
    group: "Hệ thống",
    permission: "platform.health.read",
    order: 20,
    element: <ErrorsPage />,
  },
];
