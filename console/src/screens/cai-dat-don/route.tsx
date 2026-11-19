// Màn Cài đặt đơn: bốn cài đặt và nguồn đơn (tự đăng ký, quy ước src/app/loadScreens.ts; Q-T57-8). Mockup
// docs/mockups/m2/cai-dat-don.html; mục cuối nhóm "Đơn hàng" theo khung docs/mockups/m2/khung.js.
import type { ScreenRoute } from "../../app/loadScreens";
import { OrderSettings } from "./OrderSettings";

export const screens: readonly ScreenRoute[] = [
  // Task 87 (M2-30). Ghi cần order.settings.manage; API đọc (getOrderSettings, listTrafficSources) cần order.read.
  {
    path: "/cai-dat/don",
    label: "Cài đặt đơn",
    group: "Đơn hàng",
    permission: "order.settings.manage",
    alsoRequires: ["order.read"],
    order: 40,
    element: <OrderSettings />,
  },
];
