// Mục Báo cáo (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M6 Task 239 (M6-16); mockup docs/m6/mockup:
// nhóm menu "Báo cáo" giữa "Kho" và "Hệ thống". Màn của Task 240 (Báo cáo đơn, Hãng, Bán chạy), 241 (Tồn, Tệp xuất)
// thêm dòng của mình vào đây, order 20, 30, 40, 50, 60.
import type { ScreenRoute } from "../../app/loadScreens";
import { Dashboard } from "./Dashboard";
import { ExportsNavCount } from "./ExportsNavCount";
import { InventoryReports } from "./InventoryReports";
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
    path: "/bao-cao/ton-kho",
    label: "Tồn và xuất nhập tồn",
    group: "Báo cáo",
    permission: "report.inventory.read",
    order: 50,
    element: <InventoryReports />,
  },
  {
    // Quyền chính report.sales.read: mọi vai có report.export (chủ, quản lý, kế toán) đều có (quyết định Task 241).
    path: "/bao-cao/tep-xuat",
    label: "Tệp xuất",
    group: "Báo cáo",
    permission: "report.sales.read",
    order: 60,
    badge: <ExportsNavCount />,
    element: <RunsPage />,
  },
];
