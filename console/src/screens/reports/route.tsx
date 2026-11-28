// Mục Báo cáo (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M6 Task 239 (M6-16); mockup docs/m6/mockup:
// nhóm menu "Báo cáo" giữa "Kho" và "Hệ thống". Màn của Task 240 (Báo cáo đơn, Hãng, Bán chạy), 241 (Tồn, Tệp xuất)
// thêm dòng của mình vào đây, order 20, 30, 40, 50, 60.
import type { ScreenRoute } from "../../app/loadScreens";
import { BestSellers } from "./BestSellers";
import { CarriersReport } from "./CarriersReport";
import { Dashboard } from "./Dashboard";
import { ExportsNavCount } from "./ExportsNavCount";
import { InventoryReports } from "./InventoryReports";
import { OrdersReport } from "./OrdersReport";
import { RunsPage } from "./RunsPage";

export const screens: readonly ScreenRoute[] = [
  {
    path: "/bao-cao/tong-quan",
    label: "Tổng quan",
    group: "Báo cáo",
    permission: "report.sales.read",
    order: 10,
    element: <Dashboard />,
  },
  {
    path: "/bao-cao/don",
    label: "Báo cáo đơn",
    group: "Báo cáo",
    permission: "report.sales.read",
    order: 20,
    element: <OrdersReport />,
  },
  {
    path: "/bao-cao/van-chuyen",
    label: "Hãng vận chuyển",
    group: "Báo cáo",
    permission: "report.sales.read",
    order: 30,
    element: <CarriersReport />,
  },
  {
    path: "/bao-cao/ban-chay",
    label: "Bán chạy",
    group: "Báo cáo",
    permission: "report.sales.read",
    order: 40,
    element: <BestSellers />,
  },
  {
    path: "/bao-cao/ton-kho",
    label: "Tồn và xuất nhập tồn",
    group: "Báo cáo",
    permission: "report.inventory.read",
    order: 50,
    element: <InventoryReports />,
  },
  {
    // Quyền chính report.sales.read: mọi vai có report.export (chủ, quản lý, kế toán) đều có (quyết định Task 241).
    // Vai kho chỉ có report.inventory.read vẫn thấy menu để tải tệp tồn của mình (Task 519); trang chỉ hiện họ có quyền.
    path: "/bao-cao/tep-xuat",
    label: "Tệp xuất",
    group: "Báo cáo",
    permission: "report.sales.read",
    orPermissions: ["report.inventory.read"],
    order: 60,
    badge: <ExportsNavCount />,
    element: <RunsPage />,
  },
];
