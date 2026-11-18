// Màn Sức khỏe hệ thống (tự đăng ký, quy ước src/app/loadScreens.ts).
import type { ScreenRoute } from "../../app/loadScreens";
import { HealthPage } from "./HealthPage";

export const screens: readonly ScreenRoute[] = [
  // Task 53 (M1-53): Sức khỏe hệ thống (mockup docs/mockups/m1/suc-khoe.html).
  {
    path: "/suc-khoe",
    label: "Sức khỏe hệ thống",
    group: "Hệ thống",
    permission: "platform.health.read",
    order: 10,
    element: <HealthPage />,
  },
];
