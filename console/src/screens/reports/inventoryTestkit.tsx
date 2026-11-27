// API giả (fetch) cho test vitest của báo cáo tồn, Tệp xuất và nút xuất (Task 241, M6-18). Chỉ test nhập tệp này.
// Số của mockup docs/m6/mockup/ton-kho.html: áo A-S đen M: tồn sổ 24, giữ 3, đang giao 2, lỗi 1 → có thể bán 18.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, createMemoryRouter, RouterProvider } from "react-router";
import type { Grant, InventoryMovementRow, InventoryStockRow, Me, ReportRun, Warehouse } from "../../api";
import { NOT_FOUND, renderScreens, reply, type Write } from "../../app/screenTestkit";
import { installFakeNow, makeRun, stubApi } from "./reportsTestkit";
import { screens } from "./route";

export { installFakeNow, makeRun };

export const KHO_HN = "0195f3b2-0000-7000-8000-00000000a001";
export const KHO_LB = "0195f3b2-0000-7000-8000-00000000a002";

export const WAREHOUSES: Warehouse[] = [
  { id: KHO_HN, code: "HN", name: "Kho Hà Nội", status: "active" },
  { id: KHO_LB, code: "LB", name: "Kho phụ Long Biên", status: "active" },
];

export const LONG_PRODUCT =
  "Áo sơ mi lụa cổ đức tay bồng phối nơ cổ, dáng suông dài qua hông, chất lụa Hàn mềm mát không nhăn, mặc đi làm, đi chơi ạ";

const all = (permission: string): Grant => ({ permission, scope: { type: "all", id: "" } });
export const STOCK_ALL: Grant[] = [all("report.inventory.read"), all("report.sales.read"), all("report.export")];
export const STOCK_ONE_WH: Grant[] = [
  { permission: "report.inventory.read", scope: { type: "warehouse", id: KHO_HN } },
];
export const EXPORT_CUSTOMERS: Grant[] = [all("customer.export")];

/** Sáu trường nhận diện biến thể chung của dòng tồn và dòng xuất nhập tồn. */
const variantOf = (base: number, n: number, sku: string, name: string, color: string, size: string) => ({
  variantId: `0195f3b2-0000-7000-8000-${(base + n).toString(16).padStart(12, "0")}`,
  sku: `${sku}-${String(n).padStart(5, "0")}`,
  productName: `${name} ${String(n)}`,
  color,
  size,
  warehouseId: KHO_HN,
});

export function stockRow(n: number, p: Partial<InventoryStockRow> = {}): InventoryStockRow {
  return {
    ...variantOf(0xe000, n, "SKU", "Áo mẫu", "Đen", "M"),
    warehouseName: "Kho Hà Nội",
    onHand: 24,
    held: 3,
    inTransit: 2,
    damaged: 1,
    available: 18,
    ...p,
  };
}

export function movementRow(n: number, p: Partial<InventoryMovementRow> = {}): InventoryMovementRow {
  return {
    ...variantOf(0xf000, n, "XNT", "Quần mẫu", "Be", "S"),
    opening: 30,
    imported: 20,
    returnedIn: 2,
    sold: 27,
    exportedOther: 0,
    adjusted: -1,
    closing: 24,
    damagedClosing: 1,
    ...p,
  };
}

export interface InventoryFake {
  writes: Write[];
  /** Số yêu cầu ghi tới đây còn bị mất mạng (dùng chung với stubApi). */
  failPosts: number;
  /** Mọi GET đã nhận: "đường?truy vấn". */
  gets: string[];
  grants: Grant[];
  warehouses: Warehouse[];
  /** Mọi dòng tồn hôm nay (phân trang bằng cursor là vị trí). */
  stock: InventoryStockRow[];
  /** Ngày bản chụp đầu tiên (YYYY-MM-DD); không có → chưa chụp. */
  firstSnapshotDay: string | undefined;
  /** Dòng tồn của ngày đã chụp. */
  snapshots: Record<string, InventoryStockRow[]>;
  movements: InventoryMovementRow[];
  /** Lỗi thay cho GET tồn/xuất nhập tồn (mọi lần gọi). */
  readError: { status: number; body: unknown } | null;
  /** Lần chạy theo họ: sales, inventory, customers. */
  runs: { sales: ReportRun[]; inventory: ReportRun[]; customers: ReportRun[] };
  /** Lỗi thay cho lần đọc danh sách của một họ. */
  listError: Partial<Record<"sales" | "inventory" | "customers", { status: number; body: unknown }>>;
  fileUrl: string;
  /** Câu trả lời xếp sẵn cho yêu cầu ghi (lấy lần lượt). */
  replies: { status: number; body: unknown }[];
  hold: Promise<void> | null;
  tags: { id: string; name: string; color: string; version: number }[];
}

export function fakeInventory(p: Partial<InventoryFake> = {}): InventoryFake {
  return {
    writes: [],
    failPosts: 0,
    gets: [],
    grants: STOCK_ALL,
    warehouses: WAREHOUSES,
    stock: [
      stockRow(1, { sku: "AO-A-DEN-M", productName: LONG_PRODUCT }),
      stockRow(2, { sku: "AO-A-DEN-S", onHand: 10, held: 0, inTransit: 1, damaged: 0, available: 9, size: "S" }),
    ],
    firstSnapshotDay: "2026-11-15",
    snapshots: {},
    movements: [],
    readError: null,
    runs: { sales: [], inventory: [], customers: [] },
    listError: {},
    fileUrl: "https://files.example.test/reports/run-1.xlsx",
    replies: [],
    hold: null,
    tags: [{ id: "0195f3b2-0000-7000-8000-00000000a0a1", name: "Khách VIP", color: "gray", version: 1 }],
    ...p,
  };
}

const PAGE_DEFAULT = 100;

/** Cắt rows theo limit và cursor (vị trí); trả items và nextCursor. */
function pageOf<T>(rows: T[], q: URLSearchParams) {
  const limit = Number(q.get("limit") ?? PAGE_DEFAULT);
  const from = Number(q.get("cursor") ?? "0");
  const items = rows.slice(from, from + limit);
  return from + limit < rows.length ? { items, nextCursor: String(from + limit) } : { items };
}

const RUNS_LIST: Record<string, keyof InventoryFake["runs"]> = {
  "/api/v1/reports/sales/runs": "sales",
  "/api/v1/reports/inventory/runs": "inventory",
  "/api/v1/reports/customers/exports": "customers",
};
const RUN_ONE = /^\/api\/v1\/reports\/(sales\/runs|inventory\/runs|customers\/exports)\/([^/]+)(\/file)?$/;
const FAMILY_OF: Record<string, keyof InventoryFake["runs"]> = {
  "sales/runs": "sales",
  "inventory/runs": "inventory",
  "customers/exports": "customers",
};

function readRoute(f: InventoryFake, path: string, q: URLSearchParams): Response {
  if (path === "/api/v1/me") return reply({ userId: "me", email: "a@elimaz.vn", grants: f.grants } satisfies Me);
  if (path === "/api/v1/inventory/warehouses") return reply(f.warehouses);
  if (path === "/api/v1/customer-tags") return reply({ items: f.tags });
  if (path.startsWith("/api/v1/reports/")) f.gets.push(`${path}?${q.toString()}`);
  const family = RUNS_LIST[path];
  if (family !== undefined) {
    const err = f.listError[family];
    if (err !== undefined) return reply(err.body, err.status);
    return reply(pageOf(f.runs[family], q));
  }
  const one = RUN_ONE.exec(path);
  if (one !== null) {
    const run = f.runs[FAMILY_OF[one[1] ?? ""] ?? "sales"].find((r) => r.id === one[2]);
    if (run === undefined) return reply(NOT_FOUND, 404);
    return one[3] === undefined ? reply(run) : reply({ url: f.fileUrl, expiresAt: "2026-11-28T07:00:00Z" });
  }
  if (path === "/api/v1/reports/inventory/stock" || path === "/api/v1/reports/inventory/movements") {
    if (f.readError !== null) return reply(f.readError.body, f.readError.status);
  }
  if (path === "/api/v1/reports/inventory/stock") {
    const date = q.get("date");
    const prefix = (q.get("q") ?? "").toLowerCase();
    const bySku = (r: InventoryStockRow) => r.sku.toLowerCase().startsWith(prefix);
    if (date === null) {
      const pg = pageOf(f.stock.filter(bySku), q);
      return reply({
        status: "ready",
        date: "2026-11-27",
        source: "live",
        ...(f.firstSnapshotDay === undefined ? {} : { firstSnapshotDay: f.firstSnapshotDay }),
        rows: pg.items,
        ...(pg.nextCursor === undefined ? {} : { nextCursor: pg.nextCursor }),
      });
    }
    const snap = f.snapshots[date];
    if (snap === undefined) return reply({ error: "no_snapshot", request_id: "t" }, 404);
    const rows = snap.filter(bySku);
    const pg = pageOf(rows, q);
    return reply({
      status: "ready",
      date,
      source: "snapshot",
      rows: pg.items,
      ...(pg.nextCursor === undefined ? {} : { nextCursor: pg.nextCursor }),
    });
  }
  if (path === "/api/v1/reports/inventory/movements") {
    const prefix = (q.get("q") ?? "").toLowerCase();
    const rows = f.movements.filter((r) => r.sku.toLowerCase().startsWith(prefix));
    const pg = pageOf(rows, q);
    return reply({
      status: "ready",
      period: { from: q.get("from") ?? "", to: q.get("to") ?? "" },
      rows: pg.items,
      ...(pg.nextCursor === undefined ? {} : { nextCursor: pg.nextCursor }),
    });
  }
  return reply(NOT_FOUND, 404);
}

function writeRoute(f: InventoryFake, path: string, body: Record<string, unknown>): Response {
  const inv = path === "/api/v1/reports/inventory/runs";
  const cus = path === "/api/v1/reports/customers/exports";
  if (!inv && !cus) return reply(NOT_FOUND, 404);
  const fam = inv ? "inventory" : "customers";
  const id = `run-${String(f.runs[fam].length + 1)}`;
  const params = typeof body.params === "object" && body.params !== null ? { ...body.params } : {};
  const run = makeRun(id, {
    family: fam,
    report: inv ? (body.report === "products" ? "products" : "stock") : "customers",
    format: "xlsx",
    params,
  });
  f.runs[fam].push(run);
  return reply(run, 202);
}

/** Thay fetch bằng API giả (đọc: readRoute; ghi: ghi lại kèm Idempotency-Key, trả lần chạy mới). */
export function stubInventory(f: InventoryFake): InventoryFake {
  stubApi(f, readRoute, writeRoute);
  return f;
}

/** Dựng các màn báo cáo ở đường path với API giả f (kèm trang giả cho liên kết sang màn khác). */
export function renderReports(f: InventoryFake, path: string) {
  stubInventory(f);
  return renderScreens(screens, path, [{ path: "/don/:id", element: <p>Trang đơn</p> }]);
}

/** Dựng một thành phần riêng dưới QueryClient và router bộ nhớ (hộp xuất, nút xuất). */
export function renderUi(f: InventoryFake, ui: ReactNode, path = "/") {
  stubInventory(f);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      { path: "/bao-cao/tep-xuat", element: <p>Trang Tệp xuất</p> },
      { path: "*", element: ui },
    ],
    { initialEntries: [path] },
  );
  const view = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { qc, router, ...view };
}

export { MemoryRouter };
