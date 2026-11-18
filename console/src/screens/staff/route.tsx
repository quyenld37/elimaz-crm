// Màn Nhân viên (tự đăng ký, quy ước src/app/loadScreens.ts).
import type { ScreenRoute } from "../../app/loadScreens";
import { StaffGrants } from "./StaffGrants";
import { StaffList } from "./StaffList";

export const screens: readonly ScreenRoute[] = [
  // Task 54: Nhân viên và quyền (mockup docs/mockups/m1/nhan-vien.html).
  {
    path: "/nhan-vien",
    also: ["/nhan-vien/:id/quyen"],
    label: "Nhân viên",
    group: "Quản trị",
    permission: "identity.users.manage",
    order: 10,
    element: <StaffList />,
  },
  {
    path: "/nhan-vien/:id/quyen",
    label: "Nhân viên",
    group: "Quản trị",
    permission: "identity.users.manage",
    hidden: true,
    order: 10,
    element: <StaffGrants />,
  },
];
