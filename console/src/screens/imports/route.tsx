// Màn Nhập Excel (tự đăng ký, quy ước src/app/loadScreens.ts).
import type { ScreenRoute } from "../../app/loadScreens";
import { ImportPage } from "./ImportPage";

export const screens: readonly ScreenRoute[] = [
  // Task 52: Nhập sản phẩm từ Excel (mockup docs/mockups/m1/nhap-excel.html). Path ngoài "/san-pham/…" để mục
  // "Danh sách sản phẩm" (also "/san-pham/:id") không sáng cùng lúc. Tải lên cần thêm files.upload (API kiểm).
  {
    path: "/nhap-excel",
    label: "Nhập Excel",
    group: "Sản phẩm",
    permission: "catalog.import",
    order: 30,
    element: <ImportPage />,
  },
];
