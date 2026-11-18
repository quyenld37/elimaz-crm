// Danh sách màn của CRM. Mỗi task màn hình (M1-50 trở đi, sau khi mockup được duyệt) thêm một dòng ở đây; khung,
// menu và định tuyến tự có.
import { StaffGrants } from "../features/staff/StaffGrants";
import { StaffList } from "../features/staff/StaffList";
import type { Screen } from "./nav";

export const SCREENS: readonly Screen[] = [
  // Task 54: Nhân viên và quyền (mockup docs/mockups/m1/nhan-vien.html).
  {
    path: "/nhan-vien",
    also: ["/nhan-vien/:id/quyen"],
    label: "Nhân viên",
    group: "Quản trị",
    permission: "identity.users.manage",
    element: <StaffList />,
  },
  {
    path: "/nhan-vien/:id/quyen",
    label: "Nhân viên",
    group: "Quản trị",
    permission: "identity.users.manage",
    hidden: true,
    element: <StaffGrants />,
  },
];
