// Màn Kho (tự đăng ký, quy ước src/app/loadScreens.ts).
import type { ScreenRoute } from "../../app/loadScreens";
import { Balances } from "./Balances";
import { DocumentEdit } from "./DocumentEdit";
import { DocumentList } from "./DocumentList";
import { StocktakeSession, StocktakeStart } from "./Stocktake";
import { LegacyReturnPage } from "./legacy/LegacyReturnPage";
import { OpeningSession, OpeningStart } from "./opening/OpeningStocktakePage";
import { Suppliers } from "./Suppliers";

// Kiểm kho còn gọi listWarehouses, getStocktake, listBalances (inventory.read) và searchVariants (catalog.read).
const STOCKTAKE_ALSO = ["inventory.read", "catalog.read"];

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
    alsoRequires: STOCKTAKE_ALSO,
    order: 30,
    also: ["/kho/kiem-kho/:id"],
    element: <StocktakeStart />,
  },
  {
    path: "/kho/kiem-kho/:id",
    label: "Kiểm kho",
    group: "Kho",
    permission: "inventory.stocktake",
    alsoRequires: STOCKTAKE_ALSO,
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
  // Task 273 (M7-28): Kiểm kho mở sổ (mockup docs/mockups/m7/kiem-kho-mo-so.html). Không có mục menu (HV01): vào từ nút
  // "Mở sổ kho này" của Kiểm kho. Đếm bằng quét mã đòi inventory.stocktake, đọc kho và tên mã đòi inventory.read,
  // catalog.read.
  {
    path: "/kho/mo-so",
    label: "Kiểm kho mở sổ",
    group: "Kho",
    permission: "inventory.stocktake.opening",
    alsoRequires: STOCKTAKE_ALSO,
    order: 32,
    hidden: true,
    element: <OpeningStart />,
  },
  {
    path: "/kho/mo-so/:id",
    label: "Kiểm kho mở sổ",
    group: "Kho",
    permission: "inventory.stocktake.opening",
    alsoRequires: [...STOCKTAKE_ALSO, "inventory.stocktake"],
    order: 33,
    hidden: true,
    element: <OpeningSession />,
  },
  // Task 273 (M7-28): Hàng hoàn đơn cũ (mockup docs/mockups/m7/hang-hoan-don-cu.html), HV02: ngay sau "Nhận hàng hoàn".
  {
    path: "/kho/hang-hoan-cu",
    label: "Hàng hoàn đơn cũ",
    group: "Kho",
    permission: "inventory.legacy_return.create",
    alsoRequires: ["inventory.read", "catalog.read"],
    order: 16,
    element: <LegacyReturnPage />,
  },
];
