// Màn Kho của M3: Đóng gói, phiếu nhặt, Nhận hàng hoàn (tự đăng ký, quy ước src/app/loadScreens.ts; kế hoạch M3 Task
// 136; mockup docs/mockups/m3/kho.html, menu theo docs/mockups/m3/khung.js: Tồn kho, Đóng gói, Nhận hàng hoàn, Phiếu
// kho…). Nút ghi hiện theo quyền riêng (fulfillment.print, fulfillment.pack); API kiểm theo kho.
import type { ScreenRoute } from "../../app/loadScreens";
import { PickingListPrint } from "./PickingListPrint";
import { ReturnReceiptPage } from "./ReturnReceiptPage";
import { ShipmentsToPackPage } from "./ShipmentsToPackPage";

export const screens: readonly ScreenRoute[] = [
  // Danh sách đọc listOrders (order.read) và listWarehouses (inventory.read).
  {
    path: "/warehouse/pack",
    also: ["/warehouse/picking-list"],
    label: "Đóng gói",
    group: "Kho",
    permission: "fulfillment.pack",
    alsoRequires: ["order.read", "inventory.read"],
    order: 12,
    element: <ShipmentsToPackPage />,
  },
  {
    path: "/warehouse/picking-list",
    label: "Phiếu nhặt hàng",
    group: "Kho",
    permission: "fulfillment.print",
    hidden: true,
    order: 13,
    element: <PickingListPrint />,
  },
  // Phiếu và đơn đọc bằng order.read; nhận bằng order.return.receive.
  {
    path: "/warehouse/returns/receive",
    also: ["/warehouse/returns/receive/:returnId"],
    label: "Nhận hàng hoàn",
    group: "Kho",
    permission: "order.return.receive",
    alsoRequires: ["order.read"],
    order: 14,
    element: <ReturnReceiptPage />,
  },
  {
    path: "/warehouse/returns/receive/:returnId",
    label: "Nhận hàng hoàn",
    group: "Kho",
    permission: "order.return.receive",
    alsoRequires: ["order.read"],
    hidden: true,
    order: 15,
    element: <ReturnReceiptPage />,
  },
];
