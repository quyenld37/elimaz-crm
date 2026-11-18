// Danh sách màn của CRM. Mỗi task màn hình (M1-50 trở đi, sau khi mockup được duyệt) thêm một dòng ở đây; khung,
// menu và định tuyến tự có.
import { ProductEdit } from "../features/products/ProductEdit";
import { ProductList } from "../features/products/ProductList";
import { StaffGrants } from "../features/staff/StaffGrants";
import { StaffList } from "../features/staff/StaffList";
import type { Screen } from "./nav";

export const SCREENS: readonly Screen[] = [
  // Task 50: Sản phẩm — danh sách (mockup docs/mockups/m1/san-pham-danh-sach.html).
  {
    path: "/san-pham",
    also: ["/san-pham/:id"],
    label: "Danh sách sản phẩm",
    group: "Sản phẩm",
    permission: "catalog.read",
    element: <ProductList />,
  },
  // Task 324: thêm mẫu ("/san-pham/moi") và sửa mẫu (mockup docs/mockups/m1/san-pham-sua.html). Xem cần catalog.read;
  // không có catalog.manage thì biểu mẫu chỉ đọc.
  {
    path: "/san-pham/:id",
    label: "Sửa mẫu",
    group: "Sản phẩm",
    permission: "catalog.read",
    hidden: true,
    element: <ProductEdit />,
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
];
