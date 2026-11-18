// Danh sách màn của CRM. Mỗi task màn hình (M1-50 trở đi, sau khi mockup được duyệt) thêm một dòng ở đây; khung,
// menu và định tuyến tự có.
import { Balances } from "../features/inventory/Balances";
import { ProductList } from "../features/products/ProductList";
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
  // Task 51: Tồn kho (mockup docs/mockups/m1/kho-ton.html).
  {
    path: "/kho/ton",
    label: "Tồn kho",
    group: "Kho",
    permission: "inventory.read",
    element: <Balances />,
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
