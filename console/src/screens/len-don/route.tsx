// Màn Lên đơn nhanh (tự đăng ký, quy ước src/app/loadScreens.ts; Q-T57-8). Mockup docs/mockups/m2/len-don.html.
import type { ScreenRoute } from "../../app/loadScreens";
import { OrderPage } from "./OrderPage";

export const screens: readonly ScreenRoute[] = [
  // Task 84 (M2-27). Cần order.create; API của màn còn đòi customer.read (tìm khách, địa chỉ), catalog.read (tìm sản
  // phẩm), order.read (nguồn đơn, cài đặt đơn). "Có thể bán" cần thêm inventory.read, không có thì màn bỏ số đó.
  {
    path: "/don/moi",
    label: "Lên đơn",
    group: "Đơn hàng",
    permission: "order.create",
    alsoRequires: ["customer.read", "catalog.read", "order.read"],
    order: 10,
    element: <OrderPage />,
  },
];
