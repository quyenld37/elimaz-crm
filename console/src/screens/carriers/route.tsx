// Màn Kết nối vận chuyển và Cài đặt vận chuyển (tự đăng ký, quy ước src/app/loadScreens.ts; kế hoạch M3 Task 132,
// M3-41; mockup docs/mockups/m3/ket-noi-van-chuyen.html). Menu theo docs/mockups/m3/khung.js: nhóm "Vận chuyển", sau bốn
// danh sách vận hành (Task 135) là "Kết nối vận chuyển", "Cài đặt vận chuyển".
import type { ScreenRoute } from "../../app/loadScreens";
import { CarrierAccountDetail } from "./CarrierAccountDetail";
import { CarrierAccountsPage } from "./CarrierAccountsPage";
import { FulfillmentSettingsPage } from "./FulfillmentSettingsPage";

export const screens: readonly ScreenRoute[] = [
  // Tên kho đọc bằng listWarehouses (inventory.read). Nút ghi theo fulfillment.carrier_accounts.manage (API kiểm).
  {
    path: "/settings/carriers",
    also: ["/settings/carriers/:id"],
    label: "Kết nối vận chuyển",
    group: "Vận chuyển",
    permission: "fulfillment.carrier_accounts.read",
    alsoRequires: ["inventory.read"],
    order: 50,
    element: <CarrierAccountsPage />,
  },
  {
    path: "/settings/carriers/:id",
    label: "Tài khoản hãng",
    group: "Vận chuyển",
    permission: "fulfillment.carrier_accounts.read",
    alsoRequires: ["inventory.read"],
    hidden: true,
    order: 51,
    element: <CarrierAccountDetail />,
  },
  // Đọc getFulfillmentSettings cần fulfillment.read; ghi cần fulfillment.settings.manage.
  {
    path: "/settings/fulfillment",
    label: "Cài đặt vận chuyển",
    group: "Vận chuyển",
    permission: "fulfillment.settings.manage",
    alsoRequires: ["fulfillment.read"],
    order: 60,
    element: <FulfillmentSettingsPage />,
  },
];
