// Màn Kho (tự đăng ký, quy ước src/app/loadScreens.ts).
import type { ScreenRoute } from "../../app/loadScreens";
import { Balances } from "./Balances";
import { DocumentEdit } from "./DocumentEdit";
import { DocumentList } from "./DocumentList";
import { StocktakeSession, StocktakeStart } from "./Stocktake";
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
  // Task 328: Phiếu kho (mockup docs/mockups/m1/kho-phieu.html). Soạn cần inventory.documents.create, ghi sổ cần
  // inventory.documents.post; màn tự ẩn, khóa nút theo quyền, API kiểm theo kho.
  {
    path: "/kho/phieu",
    also: ["/kho/phieu/:id"],
    label: "Phiếu kho",
    group: "Kho",
    permission: "inventory.read",
    order: 20,
    element: <DocumentList />,
  },
  {
    path: "/kho/phieu/:id",
    label: "Phiếu kho",
    group: "Kho",
    permission: "inventory.read",
    hidden: true,
    order: 20,
    element: <DocumentEdit />,
  },
  // Task 329: Kiểm kho trên điện thoại (mockup docs/mockups/m1/kho-kiem-kho.html). Thứ tự theo khung.js: Tồn kho, Phiếu
  // kho, Kiểm kho, Nhà cung cấp. Mỗi phiên có đường dẫn riêng để gửi cho người cùng đếm.
  {
    path: "/kho/kiem-kho",
    label: "Kiểm kho",
    group: "Kho",
    permission: "inventory.stocktake",
    order: 30,
    also: ["/kho/kiem-kho/:id"],
    element: <StocktakeStart />,
  },
  {
    path: "/kho/kiem-kho/:id",
    label: "Kiểm kho",
    group: "Kho",
    permission: "inventory.stocktake",
    order: 31,
    hidden: true,
    element: <StocktakeSession />,
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
