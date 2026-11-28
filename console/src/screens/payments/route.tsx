// Màn Nghĩa vụ hoàn (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M3 Task 138; mockup
// docs/mockups/m3/tien-tren-don.html trạng thái B; nhóm menu "Tiền" (HV01) sau ba màn đối soát COD.
import type { ScreenRoute } from "../../app/loadScreens";
import { RefundObligationsPage } from "./RefundObligationsPage";

export const screens: readonly ScreenRoute[] = [
  {
    path: "/refund-obligations",
    label: "Nghĩa vụ hoàn",
    group: "Tiền",
    permission: "cash.cod.read",
    order: 40,
    element: <RefundObligationsPage />,
  },
];
