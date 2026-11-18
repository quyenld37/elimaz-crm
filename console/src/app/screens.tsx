// Danh sách màn của CRM. Mỗi task màn hình (M1-50 trở đi, sau khi mockup được duyệt) thêm một dòng ở đây; khung,
// menu và định tuyến tự có.
import { ProductList } from "../features/products/ProductList";
import { RoleEdit } from "../features/roles/RoleEdit";
import { RoleList } from "../features/roles/RoleList";
import { StaffGrants } from "../features/staff/StaffGrants";
import { StaffList } from "../features/staff/StaffList";
import type { Screen } from "./nav";

export const SCREENS: readonly Screen[] = [
  // Task 50: Sản phẩm — danh sách (mockup docs/mockups/m1/san-pham-danh-sach.html). Trang sửa mẫu (Task 324) thêm
  // dòng "/san-pham/:id" riêng.
  {
    path: "/san-pham",
    also: ["/san-pham/:id"],
    label: "Danh sách sản phẩm",
    group: "Sản phẩm",
    permission: "catalog.read",
    element: <ProductList />,
  },
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
  // Task M1-56: Vai trò tự định nghĩa (mockup docs/mockups/m1/vai-tro.html).
  {
    path: "/vai-tro",
    also: ["/vai-tro/:id"],
    label: "Vai trò",
    group: "Quản trị",
    permission: "identity.roles.manage",
    element: <RoleList />,
  },
  {
    path: "/vai-tro/:id",
    label: "Vai trò",
    group: "Quản trị",
    permission: "identity.roles.manage",
    hidden: true,
    element: <RoleEdit />,
  },
];
