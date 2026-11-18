// Màn Sản phẩm (tự đăng ký, quy ước src/app/loadScreens.ts).
import type { ScreenRoute } from "../../app/loadScreens";
import { ProductEdit } from "./ProductEdit";
import { ProductList } from "./ProductList";

export const screens: readonly ScreenRoute[] = [
  // Task 50: Sản phẩm — danh sách (mockup docs/mockups/m1/san-pham-danh-sach.html).
  {
    path: "/san-pham",
    also: ["/san-pham/:id"],
    label: "Danh sách sản phẩm",
    group: "Sản phẩm",
    permission: "catalog.read",
    order: 10,
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
    order: 10,
    element: <ProductEdit />,
  },
];
