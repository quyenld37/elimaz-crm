// API giả (fetch) cho test vitest của màn Kết nối vận chuyển và Cài đặt vận chuyển (Task 132). Chỉ test nhập tệp này.
// Trả đúng kiểu của client sinh; yêu cầu ghi được ghi lại kèm khóa chống trùng.
import { vi } from "vitest";
import type {
  AdminUnit,
  CarrierAccount,
  FulfillmentSettings,
  Grant,
  Me,
  UnmappedLocation,
  Warehouse,
  WarehousePickup,
} from "../../api";
import { NOT_FOUND, bodyOf, renderScreens, reply, type Write } from "../../app/screenTestkit";
import { DEFAULT_SETTINGS } from "./carrierRules";
import { screens } from "./route";

export const WH = "0195f3b2-0000-7000-8000-0000000000a1";
export const WH2 = "0195f3b2-0000-7000-8000-0000000000a2";
const hex = (n: number) => n.toString(16).padStart(12, "0");
export const accId = (n: number) => `0195f3b2-0000-7000-8000-${hex(0xc000 + n)}`;

/** Tên tài khoản 60 ký tự không dấu cách (dữ liệu khó của mockup). */
export const LONG_NAME = "BestExpressTaiKhoanPhuChoDonSiCuaKhoChinhChiNhanhMienBacSo02";

export function account(n: number, p: Partial<CarrierAccount> = {}): CarrierAccount {
  return {
    id: accId(n),
    carrierKey: "vtp",
    mode: "api",
    environment: "production",
    name: `Tài khoản ${String(n)}`,
    status: "active",
    warehouseId: WH,
    isDefaultForWarehouse: false,
    showOnWeb: false,
    webSort: 0,
    hasCredentials: true,
    needsRewrap: false,
    version: 3,
    capabilities: {
      quote: true,
      create: true,
      cancel: true,
      track: true,
      webhook: true,
      labels: true,
      labelsBatch: true,
      updateAddress: true,
      redeliver: false,
      locations: true,
      statement: true,
    },
    ...p,
  };
}

/** Năm tài khoản của ca kế hoạch: Viettel Post 1, Best Express 3 (một cần cấp quyền lại, tên 60 ký tự), SPX 1. */
export function fiveAccounts(): CarrierAccount[] {
  return [
    account(1, {
      name: "Viettel Post — Kho chính",
      isDefaultForWarehouse: true,
      showOnWeb: true,
      webSort: 1,
      lastCheckAt: "2026-11-21T02:12:00Z",
      lastCheckResult: "ok",
      webhookUrl: "https://crm.elimaz.com.vn/hooks/vtp/q7Hk2mP0sVx9LrT4bNc8WdYe1Za6Uf3G",
    }),
    account(2, { carrierKey: "best", name: "Best Express — Kho chính", mode: "manual", hasCredentials: false }),
    account(3, {
      carrierKey: "best",
      name: LONG_NAME,
      status: "auth_failed",
      lastCheckAt: "2026-11-21T01:00:00Z",
      lastCheckResult: "vtp_unauthorized",
    }),
    account(4, { carrierKey: "best", name: "Best Express — Kho Hà Nội", status: "paused", warehouseId: WH2 }),
    account(5, { carrierKey: "spx", name: "SPX — Kho chính", status: "revoked" }),
  ];
}

export const grantsOf = (...perms: string[]): Grant[] =>
  ["inventory.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));
export const MANAGER = ["fulfillment.carrier_accounts.read", "fulfillment.carrier_accounts.manage", "customer.read"];

export interface Fake {
  grants: Grant[];
  accounts: CarrierAccount[];
  listCalls: URLSearchParams[];
  settings: FulfillmentSettings;
  pickup: WarehousePickup;
  unmapped: UnmappedLocation[];
  /** Câu trả lời xếp sẵn cho các lệnh ghi kế tiếp (lần lượt); hết thì thân mặc định theo đường (writeReply). */
  replies: { status: number; body: unknown }[];
  /** Lệnh ghi đã nhận, theo thứ tự. */
  writes: Write[];
}

const PROVINCES: AdminUnit[] = [
  { code: "01", name: "Hà Nội", level: "province" },
  { code: "79", name: "TP Hồ Chí Minh", level: "province" },
];

const WAREHOUSES: readonly Warehouse[] = [WH, WH2].map((id, i) =>
  i === 0
    ? { id, code: "MAIN", name: "Kho chính", status: "active" as const }
    : { id, code: "HN", name: "Kho Hà Nội", status: "active" as const },
);

/** Đọc của API giả: đường (khớp đầu) → thân trả; null → 404. */
function readRoutes(fake: Fake, path: string, q: URLSearchParams): unknown {
  if (path === "/api/v1/me") return { userId: "u1", email: "lan@elimaz.vn", grants: fake.grants } satisfies Me;
  if (path === "/api/v1/inventory/warehouses") return WAREHOUSES;
  if (path === "/api/v1/carrier-accounts") {
    fake.listCalls.push(q);
    return { items: fake.accounts };
  }
  if (path === "/api/v1/fulfillment-settings") return fake.settings;
  if (/^\/api\/v1\/inventory\/warehouses\/[^/]+\/pickup$/.test(path)) return fake.pickup;
  if (path === "/api/v1/carrier-locations/unmapped") return { items: fake.unmapped };
  if (path === "/api/v1/admin-units") return { items: q.get("parent") === null ? PROVINCES : [] };
  return null;
}

/** Thân mặc định của một lệnh ghi khi test không xếp sẵn câu trả lời. */
function writeReply(path: string): Response {
  if (path.endsWith("/check") || path.endsWith("/locations/sync")) return reply({ jobId: accId(900) }, 202);
  if (path === "/api/v1/carrier-accounts") return reply(account(99, { status: "draft" }), 201);
  return reply({});
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf(...MANAGER),
    accounts: fiveAccounts(),
    listCalls: [],
    settings: {
      ...DEFAULT_SETTINGS,
      pollMinutes: { awaiting_pickup: 120, shipping: 60, delivery_failed: 30, returning: 360, cancel_requested: 15 },
      version: 0,
    },
    pickup: { warehouseId: WH, version: 1 },
    unmapped: [],
    replies: [],
    writes: [],
  };
  const handle = async (req: Request): Promise<Response> => {
    const url = new URL(req.url);
    if (req.method === "GET") {
      const body = readRoutes(fake, url.pathname, url.searchParams);
      return body === null ? reply(NOT_FOUND, 404) : reply(body);
    }
    fake.writes.push({
      method: req.method,
      path: url.pathname,
      key: req.headers.get("Idempotency-Key"),
      body: await bodyOf(req),
    });
    const next = fake.replies.shift();
    return next === undefined ? writeReply(url.pathname) : reply(next.body, next.status);
  };
  vi.stubGlobal("fetch", vi.fn(handle));
  return fake;
}

export const renderAt = (path: string) => renderScreens(screens, path);

export { stubDialog } from "../don/donTestkit";
