// Mục menu "Hãng trên web" (nhóm Website; tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 208 (M5-25):
// "Hãng trên web" là mục menu trỏ tới phần Hãng trên website của màn Kết nối vận chuyển, không phải màn thứ hai —
// mở chính màn Kết nối vận chuyển (cùng quyền) và cuộn tới thẻ. Mockup docs/mockups/m5/web-carriers.html, HV09–HV12.
import type { ScreenRoute } from "../../app/loadScreens";
import { CarrierAccountsPage } from "../carriers/CarrierAccountsPage";

export const screens: readonly ScreenRoute[] = [
  // Đọc: listCarrierAccounts, getWebCarrierSettings (fulfillment.carrier_accounts.read), tên kho (inventory.read). Ghi
  // (bật, thứ tự, chọn sẵn, phí dự phòng) cần fulfillment.carrier_accounts.manage (API kiểm).
  {
    path: "/website/hang-tren-web",
    label: "Hãng trên web",
    group: "Website",
    permission: "fulfillment.carrier_accounts.read",
    alsoRequires: ["inventory.read"],
    order: 30,
    element: <CarrierAccountsPage focusWeb />,
  },
];
