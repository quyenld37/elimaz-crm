// API giả (fetch) và cách dựng khối Vận chuyển cho test vitest. Chỉ test nhập tệp này. Trả đúng kiểu của client sinh.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { vi } from "vitest";
import type {
  CarrierAccount,
  DeliveryStats,
  FulfillmentSettings,
  Grant,
  Me,
  Order,
  Shipment,
  ShippingQuote,
} from "../../api";
import { NOT_FOUND, bodyOf, reply, type Write } from "../../app/screenTestkit";
import { ME, order } from "../don/donTestkit";
import { ShippingPanel } from "./ShippingPanel";

export { O1, C1 } from "../don/donTestkit";

export const WH = "0195f3b2-0000-7000-8000-0000000000a1";
export const WH2 = "0195f3b2-0000-7000-8000-0000000000a2";
export const A_VTP = "0195f3b2-0000-7000-8000-00000000ac01";
export const A_BEST = "0195f3b2-0000-7000-8000-00000000ac02";
export const A_BEST2 = "0195f3b2-0000-7000-8000-00000000ac03";
export const A_GHN = "0195f3b2-0000-7000-8000-00000000ac04";
export const A_SELF = "0195f3b2-0000-7000-8000-00000000ac05";
export const Q_VTP = "0195f3b2-0000-7000-8000-00000000ad01";
export const Q_BEST = "0195f3b2-0000-7000-8000-00000000ad02";
export const REQ1 = "0195f3b2-0000-7000-8000-00000000ae01";
export const REQ2 = "0195f3b2-0000-7000-8000-00000000ae02";
export const SH1 = "0195f3b2-0000-7000-8000-00000000af01";

export const LONG_ACCOUNT = "BestExpressTaiKhoanPhuChoDonSiCuaKhoChinhChiNhanhMienBacSo02";

/** Đơn đã xác nhận, chưa gửi hãng, ở kho WH. */
export function confirmedOrder(p: Partial<Order> = {}): Order {
  return order({ status: "confirmed", label: "confirmed", warehouseId: WH, codAmount: 1_960_000, ...p });
}

const future = () => new Date(Date.now() + 25 * 60_000).toISOString();

export function quote(p: Partial<ShippingQuote> = {}): ShippingQuote {
  return {
    quoteId: Q_VTP,
    carrierAccountId: A_VTP,
    accountName: "Viettel Post — Kho chính",
    carrierKey: "vtp",
    status: "ok",
    service: "VCN",
    serviceName: "Chuyển phát nhanh",
    feeTotal: 32_000,
    expectedDays: 2,
    validUntil: future(),
    ...p,
  };
}

export function account(p: Partial<CarrierAccount> = {}): CarrierAccount {
  return {
    id: A_GHN,
    carrierKey: "ghn",
    mode: "manual",
    environment: "production",
    name: "GHN — nhập mã tay",
    status: "active",
    warehouseId: WH,
    isDefaultForWarehouse: false,
    showOnWeb: false,
    webSort: 0,
    hasCredentials: false,
    needsRewrap: false,
    version: 1,
    ...p,
  };
}

export function shipment(p: Partial<Shipment> = {}): Shipment {
  return {
    id: SH1,
    orderId: "0195f3b2-0000-7000-8000-00000000d001",
    code: "E261121K7Q2MX",
    carrierKey: "vtp",
    carrierAccountId: A_VTP,
    accountName: "Viettel Post — Kho chính",
    mode: "api",
    status: "creating",
    createState: "pending",
    label: "Đang gửi hãng",
    codAmount: 1_960_000,
    createdAt: "2026-11-21T03:42:00Z",
    version: 1,
    ...p,
  };
}

export const grantsAt = (warehouse: string, ...perms: string[]): Grant[] =>
  ["order.read", "customer.read", "fulfillment.read", ...perms].map((permission) => ({
    permission,
    scope: permission.startsWith("fulfillment.") ? { type: "warehouse", id: warehouse } : { type: "all", id: "" },
  }));

export const SENDER = ["fulfillment.quote", "fulfillment.ship", "fulfillment.carrier_accounts.read"];

export interface Fake {
  grants: Grant[];
  /** Kết quả listShippingQuotes theo requestId ("" = lần gần nhất). */
  quotes: Map<string, { requestId?: string; items: ShippingQuote[] }>;
  quoteCalls: (string | null)[];
  accounts: CarrierAccount[];
  shipments: Map<string, Shipment>;
  orderShipments: Shipment[];
  stats: DeliveryStats | null;
  writes: Write[];
  /** Trả lần lượt cho mỗi yêu cầu ghi: mã HTTP, thân, header. Hết thì câu trả lời mặc định theo đường. */
  replies: { status: number; body: unknown; headers?: Record<string, string> }[];
  /** Thân trả mặc định của createShipment (202). */
  created: Shipment;
}

const FORBIDDEN = { error: "forbidden", request_id: "t" };

const SETTINGS: FulfillmentSettings = {
  stuckHours: {},
  callbackDeadlineHours: 24,
  pollMinutes: {},
  defaultWeightGramsPerItem: 300,
  quoteValidMinutes: 30,
  returnRisk: { watchMinReturned: 1, highMinReturned: 2, highRateBp: 3000 },
  codOverdueDays: 14,
  agedDays: { held: 20, inTransit: 180 },
  version: 1,
};

const has = (f: Fake, p: string) => f.grants.some((g) => g.permission === p);

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsAt(WH, ...SENDER),
    quotes: new Map(),
    quoteCalls: [],
    accounts: [],
    shipments: new Map(),
    orderShipments: [],
    stats: { delivered: 9, returned: 1, returnRateBp: 1000, risk: "none" },
    writes: [],
    replies: [],
    created: shipment(),
  };
  // Yêu cầu ghi: ghi lại; trả câu trả lời xếp sẵn (kèm header như Retry-After) hoặc mặc định theo đường.
  const onWrite = async (req: Request, path: string): Promise<Response> => {
    fake.writes.push({ method: req.method, path, key: req.headers.get("Idempotency-Key"), body: await bodyOf(req) });
    const queued = fake.replies.shift();
    if (queued !== undefined)
      return Response.json(queued.body, { status: queued.status, headers: queued.headers ?? {} });
    if (path.endsWith("/shipping-quotes")) {
      return reply(
        { requestId: REQ2, accounts: [{ id: A_VTP, name: "Viettel Post — Kho chính", carrierKey: "vtp" }] },
        202,
      );
    }
    if (path.endsWith("/shipments")) return reply(fake.created, 202);
    const id = /^\/api\/v1\/shipments\/([^/]+)\//.exec(path)?.[1];
    return id === undefined ? reply({}) : reply(fake.shipments.get(id) ?? NOT_FOUND);
  };
  // Đọc: theo bảng đường → câu trả lời; không khớp → 404.
  const onRead = (path: string, q: URLSearchParams): Response => {
    const me: Me = { userId: ME, email: "sale@elimaz.vn", grants: fake.grants };
    if (path === "/api/v1/me") return reply(me);
    if (path === "/api/v1/fulfillment-settings") return reply(SETTINGS);
    if (path === "/api/v1/carrier-accounts") {
      return has(fake, "fulfillment.carrier_accounts.read")
        ? reply({ items: fake.accounts.filter((a) => a.warehouseId === q.get("warehouseId")) })
        : reply(FORBIDDEN, 403);
    }
    if (/^\/api\/v1\/orders\/[^/]+\/shipping-quotes$/.test(path)) {
      const rid = q.get("requestId");
      fake.quoteCalls.push(rid);
      return reply(fake.quotes.get(rid ?? "") ?? { items: [] });
    }
    if (/^\/api\/v1\/orders\/[^/]+\/shipments$/.test(path)) return reply({ items: fake.orderShipments });
    if (/^\/api\/v1\/customers\/[^/]+\/delivery-stats$/.test(path)) {
      return fake.stats === null ? reply(NOT_FOUND, 404) : reply(fake.stats);
    }
    const sid = /^\/api\/v1\/shipments\/([^/]+)$/.exec(path)?.[1];
    const s = sid === undefined ? undefined : fake.shipments.get(sid);
    return s === undefined ? reply(NOT_FOUND, 404) : reply(s);
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const u = new URL(req.url);
      return req.method === "GET" ? onRead(u.pathname, u.searchParams) : onWrite(req, u.pathname);
    }),
  );
  return fake;
}

/** Dựng khối Vận chuyển của đơn o; trả QueryClient để test bắn báo hiệu realtime. */
export function renderPanel(o: Order, onEdit?: () => void) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <ShippingPanel order={o} {...(onEdit === undefined ? {} : { onEdit })} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { qc, ...view };
}
