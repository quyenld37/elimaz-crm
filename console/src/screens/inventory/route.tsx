// Màn Kho (tự đăng ký, quy ước src/app/loadScreens.ts).
import type { ScreenRoute } from "../../app/loadScreens";
import { Balances } from "./Balances";
import { Suppliers } from "./Suppliers";

export const screens: readonly ScreenRoute[] = [
  // Task 51: Tồn kho (mockup docs/mockups/m1/kho-ton.html).
  {
    path: "/kho/ton",
    label: "Tồn kho",
    group: "Kho",
    permission: "inventory.read",
    order: 10,
    element: <Balances />,
  },
  // Task 327: Nhà cung cấp (mockup docs/mockups/m1/kho-nha-cung-cap.html).
  {
    path: "/kho/nha-cung-cap",
    label: "Nhà cung cấp",
    group: "Kho",
    permission: "inventory.read",
    order: 40,
    element: <Suppliers />,
  },
];
