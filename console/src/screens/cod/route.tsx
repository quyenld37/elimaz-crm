// Màn Đối soát COD (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M3 Task 137; mockup
// docs/mockups/m3/doi-soat-cod.html; nhóm menu "Tiền" (HV01). Đường dẫn theo Interfaces của kế hoạch.
import type { ScreenRoute } from "../../app/loadScreens";
import { DiscrepanciesPage } from "./DiscrepanciesPage";
import { OverduePage } from "./OverduePage";
import { StatementDetailPage } from "./StatementDetailPage";
import { StatementsPage } from "./StatementsPage";

export const screens: readonly ScreenRoute[] = [
  {
    path: "/cod/statements",
    label: "Đối soát COD",
    group: "Tiền",
    permission: "cash.cod.read",
    also: ["/cod/statements/:id"],
    order: 10,
    element: <StatementsPage />,
  },
  {
    path: "/cod/discrepancies",
    label: "Lệch đối soát",
    group: "Tiền",
    permission: "cash.cod.read",
    order: 20,
    element: <DiscrepanciesPage />,
  },
  {
    path: "/cod/overdue",
    label: "Quá hạn chưa có tiền",
    group: "Tiền",
    permission: "cash.cod.read",
    order: 30,
    element: <OverduePage />,
  },
  // Trang ẩn: một bảng kê, order lớn để /cod/statements khớp trước.
  {
    path: "/cod/statements/:id",
    label: "Bảng kê COD",
    group: "Tiền",
    permission: "cash.cod.read",
    hidden: true,
    order: 900,
    element: <StatementDetailPage />,
  },
];
