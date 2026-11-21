// API giả (fetch) cho test vitest của màn Kho M3. Chỉ test nhập tệp này. Trả đúng kiểu của client sinh.
import { vi } from "vitest";
import type {
  CarrierAccount,
  Grant,
  LabelBatch,
  Me,
  Order,
  OrderListItem,
  PickingList,
  Return,
  Shipment,
  Warehouse,
} from "../../api";
import { renderScreens, reply, bodyOf, NOT_FOUND, type Write } from "../../app/screenTestkit";
import { screens } from "./route";

export const WH = "0195f3b2-0000-7000-8000-0000000000a1";
export const WH2 = "0195f3b2-0000-7000-8000-0000000000a2";
export const ACC_VTP = "0195f3b2-0000-7000-8000-0000000000c1";
export const ACC_BEST = "0195f3b2-0000-7000-8000-0000000000c2";
export const ACC_SELF = "0195f3b2-0000-7000-8000-0000000000c3";

const hex = (n: number) => n.toString(16).padStart(12, "0");
export const orderId = (n: number) => `0195f3b2-0000-7000-8000-${hex(0xd000 + n)}`;
export const shipId = (n: number) => `0195f3b2-0000-7000-8000-${hex(0x5000 + n)}`;

/** Một đơn chờ thu gom; n là số thứ tự (mã DH00012<n>). */
export function packRow(
  n: number,
  p: Partial<OrderListItem> = {},
  ship: Partial<NonNullable<OrderListItem["shipment"]>> = {},
): OrderListItem {
  return {
    id: orderId(n),
    code: `DH00012${String(n).padStart(2, "0")}`,
    label: "awaiting_pickup",
    status: "confirmed",
    customerId: "0195f3b2-0000-7000-8000-00000000c001",
    customerName: "Khách",
    grandTotal: 690_000,
    codAmount: 690_000,
    itemCount: 1,
    createdAt: "2026-11-21T07:00:00Z",
    duplicateSuspect: false,
    packingStatus: "none",
    shipment: {
      id: shipId(n),
      code: `E2611210000${String(n)}`,
      carrierKey: "vtp",
      carrierAccountId: ACC_VTP,
      mode: "api",
      trackingCode: `12345678901${String(n)}`,
      status: "awaiting_pickup",
      ...ship,
    },
    ...p,
  };
}

/** Năm vận đơn của trạng thái A: Viettel Post 3, Best Express 1, tự giao 1. */
export function fiveRows(): OrderListItem[] {
  return [
    packRow(40, { itemCount: 3, codAmount: 1_960_000 }, { labelPrintedAt: "2026-11-21T08:12:00Z" }),
    packRow(41),
    packRow(42, { itemCount: 64, codAmount: 112_450_000, packingStatus: "picking" }),
    packRow(
      43,
      { codAmount: 0, packingStatus: "packed" },
      {
        carrierKey: "best",
        carrierAccountId: ACC_BEST,
        trackingCode: "BE0098812737VN",
      },
    ),
    packRow(
      25,
      { codAmount: 450_000 },
      {
        carrierKey: "self",
        carrierAccountId: ACC_SELF,
        mode: "manual",
        trackingCode: "E261121R2W8KN",
      },
    ),
  ];
}

export const grantsOf = (...perms: string[]): Grant[] =>
  ["order.read", "inventory.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

export interface Fake {
  grants: Grant[];
  rows: OrderListItem[];
  nextCursor: string | undefined;
  listCalls: URLSearchParams[];
  batches: Map<string, LabelBatch>;
  picking: PickingList;
  pickingCalls: URLSearchParams[];
  returns: Map<string, Return>;
  orders: Map<string, Order>;
  shipments: Map<string, Shipment>;
  writes: Write[];
  /** Trả lần lượt cho mỗi yêu cầu ghi; hết thì 200 {}. */
  replies: { status: number; body: unknown }[];
}

const WAREHOUSES: Warehouse[] = [
  { id: WH, code: "MAIN", name: "Kho chính", status: "active" },
  { id: WH2, code: "HN", name: "Kho Hà Nội", status: "active" },
];

const account = (id: string, carrierKey: string, name: string): CarrierAccount => ({
  id,
  carrierKey,
  mode: "api",
  environment: "production",
  name,
  status: "active",
  warehouseId: WH,
  isDefaultForWarehouse: false,
  showOnWeb: false,
  webSort: 0,
  hasCredentials: true,
  needsRewrap: false,
  version: 1,
});

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf("fulfillment.print", "fulfillment.pack", "fulfillment.carrier_accounts.read"),
    rows: [],
    nextCursor: undefined,
    listCalls: [],
    batches: new Map(),
    picking: { lines: [], orders: [] },
    pickingCalls: [],
    returns: new Map(),
    orders: new Map(),
    shipments: new Map(),
    writes: [],
    replies: [],
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const { pathname: p, searchParams: q } = new URL(req.url);
      if (req.method !== "GET") {
        fake.writes.push({
          method: req.method,
          path: p,
          key: req.headers.get("Idempotency-Key"),
          body: await bodyOf(req),
        });
        const r = fake.replies.shift();
        return r === undefined ? reply({}) : reply(r.body, r.status);
      }
      if (p === "/api/v1/me") {
        const me: Me = { userId: "u1", email: "kho@elimaz.vn", grants: fake.grants };
        return reply(me);
      }
      if (p === "/api/v1/inventory/warehouses") return reply(WAREHOUSES);
      if (p === "/api/v1/orders") {
        fake.listCalls.push(q);
        const cursor = q.get("cursor");
        return reply({
          items: cursor === null ? fake.rows : [],
          ...(fake.nextCursor === undefined || cursor !== null ? {} : { nextCursor: fake.nextCursor }),
        });
      }
      if (p === "/api/v1/carrier-accounts") {
        return reply({
          items: [
            account(ACC_VTP, "vtp", "Viettel Post — Kho chính"),
            account(ACC_BEST, "best", "Best Express — Kho chính"),
          ],
        });
      }
      const lb = /^\/api\/v1\/label-batches\/([^/]+)$/.exec(p);
      if (lb?.[1] !== undefined) {
        const b = fake.batches.get(lb[1]);
        return b === undefined ? reply(NOT_FOUND, 404) : reply(b);
      }
      if (p === "/api/v1/picking-list") {
        fake.pickingCalls.push(q);
        return reply(fake.picking);
      }
      if (p === "/api/v1/returns") {
        const kind = q.get("kind");
        return reply({
          items: [...fake.returns.values()].filter((r) => r.kind === kind && r.status === q.get("status")),
        });
      }
      const rm = /^\/api\/v1\/returns\/([^/]+)$/.exec(p);
      if (rm?.[1] !== undefined) {
        const r = fake.returns.get(rm[1]);
        return r === undefined ? reply(NOT_FOUND, 404) : reply(r);
      }
      const om = /^\/api\/v1\/orders\/([^/]+)$/.exec(p);
      if (om?.[1] !== undefined) {
        const o = fake.orders.get(om[1]);
        return o === undefined ? reply(NOT_FOUND, 404) : reply(o);
      }
      const sm = /^\/api\/v1\/shipments\/([^/]+)$/.exec(p);
      if (sm?.[1] !== undefined) {
        const s = fake.shipments.get(sm[1]);
        return s === undefined ? reply(NOT_FOUND, 404) : reply(s);
      }
      return reply(NOT_FOUND, 404);
    }),
  );
  return fake;
}

export const renderAt = (path: string) =>
  renderScreens(screens, path, [{ path: "/don/:id", element: <p>Trang đơn</p> }]);

export { stubDialog } from "../don/donTestkit";
