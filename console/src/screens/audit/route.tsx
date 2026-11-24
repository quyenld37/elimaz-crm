// Màn Nhật ký thao tác (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 462 (M5-50); mockup
// docs/mockups/m5/audit.html, menu nhóm "Quản trị" sau Nhân viên, Vai trò (HV01, HV24).
import type { ScreenRoute } from "../../app/loadScreens";
import { AuditLogScreen } from "./AuditLog";

export const screens: readonly ScreenRoute[] = [
  {
    path: "/nhat-ky-thao-tac",
    label: "Nhật ký thao tác",
    group: "Quản trị",
    permission: "audit.read",
    order: 30,
    element: <AuditLogScreen />,
  },
];
