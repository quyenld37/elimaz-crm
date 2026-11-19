// API giả (fetch) và cách dựng cho test vitest của màn Lên đơn. Chỉ test nhập tệp này. Trả đúng kiểu của client sinh.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { vi } from "vitest";
import type {
  AdminUnit,
  Balance,
  CouponCheck,
  Customer,
  CustomerListItem,
  Grant,
  Me,
  Order,
  OrderListItem,
  TrafficSource,
  VariantSearchHit,
  Warehouse,
} from "../../api";
import { OrderForm, type OrderFormProps } from "./OrderForm";

export const W1 = "0195f3b2-0000-7000-8000-00000000a001";
export const V1 = "0195f3b2-0000-7000-8000-00000000b001";
export const V2 = "0195f3b2-0000-7000-8000-00000000b002";
export const V3 = "0195f3b2-0000-7000-8000-00000000b003";
export const C1 = "0195f3b2-0000-7000-8000-00000000c001";
export const C2 = "0195f3b2-0000-7000-8000-00000000c002";
export const C3 = "0195f3b2-0000-7000-8000-00000000c003";
export const S_MANUAL = "0195f3b2-0000-7000-8000-00000000f001";
export const S_ZALO = "0195f3b2-0000-7000-8000-00000000f002";
export const O1 = "0195f3b2-0000-7000-8000-00000000d001";
export const O_OLD = "0195f3b2-0000-7000-8000-00000000d002";

/** Tên 120 ký tự (ca dài của mockup). */
export const LONG_NAME =
  "Đầm maxi lụa Hàn cao cấp cổ yếm phối ren hoa thủ công tay phồng nhẹ dáng xòe xếp ly nhiều tầng màu xanh ngọc bích Thu 26";

const stats = (orderCount: number, returnedCount: number) => ({
  orderCount,
  deliveredCount: orderCount - returnedCount,
  returnedCount,
  totalSpent: 8450000,
  lastOrderAt: "2026-11-18T03:00:00Z",
  returnRateBp: 1800,
});

export const CUSTOMERS: CustomerListItem[] = [
  {
    id: C1,
    code: "KH000231",
    fullName: "Trần Thị Thu Hà",
    phones: ["+84912345678"],
    matchedBy: "phone_tail",
    stats: stats(12, 2),
  },
  {
    id: C2,
    code: "KH004410",
    fullName: "Lê Hà My",
    phones: ["+84988005678"],
    matchedBy: "phone_tail",
    stats: stats(1, 0),
  },
];
/** Hồ sơ khác giữ SĐT 0987 654 321 (ca "có thể là cùng một khách"). */
export const OTHER: CustomerListItem = {
  id: C3,
  code: "KH001877",
  fullName: "Nguyễn Văn Tùng",
  phones: ["+84987654321"],
  matchedBy: "phone",
  stats: stats(3, 0),
};

export function customerOf(c: CustomerListItem): Customer {
  return {
    id: c.id,
    rootId: c.id,
    code: c.code,
    fullName: c.fullName,
    lifecycle: "customer",
    stats: c.stats,
    memberIds: [c.id],
    identities: c.phones.map((p, i) => ({
      id: `${c.id.slice(0, -3)}9${String(i).padStart(2, "0")}`,
      customerId: c.id,
      kind: "phone",
      value: p,
      source: "staff",
      createdAt: "2026-01-01T00:00:00Z",
    })),
    addresses: [
      {
        id: `${c.id.slice(0, -3)}a01`,
        customerId: c.id,
        isDefault: true,
        version: 1,
        address: {
          name: c.fullName,
          phone: c.phones[0] ?? "",
          line: "Số 5 Láng Hạ",
          provinceCode: "01",
          provinceName: "Hà Nội",
          wardCode: "00004",
          wardName: "Cầu Giấy",
        },
      },
    ],
    tags: [{ tag: { id: "t1", name: "Khách quen", color: "line", version: 1 }, source: "rule" }],
    version: 1,
    createdAt: "2026-01-01T00:00:00Z",
  };
}

const hit = (variantId: string, sku: string, productName: string, color: string, size: string, price: number) => ({
  variantId,
  productId: `${variantId.slice(0, -4)}c${variantId.slice(-3)}`,
  sku,
  barcode: `20000000${variantId.slice(-5)}`,
  productCode: sku.split("-")[0] ?? sku,
  productName,
  color,
  size,
  price,
  sellable: true,
});
export const HITS: VariantSearchHit[] = [
  hit(V1, "DAM0123-BE-M", LONG_NAME, "Be", "M", 690000),
  hit(V2, "AO0456-TRANG-S", "Áo sơ mi lụa cổ đức", "Trắng", "S", 350000),
  hit(V3, "TUI0090-NAU", "Túi hộp da mini", "Nâu", "", 290000),
];

// Số dư của kho mặc định cho một biến thể (chỉ các trường màn đọc có giá trị thật).
function balance(h: VariantSearchHit, available: number): Balance {
  const { variantId, sku, barcode, productCode, productName, color, size } = h;
  const zero = { held: 0, inTransit: 0, damaged: 0 };
  return {
    ...zero,
    variantId,
    sku,
    barcode,
    productCode,
    productName,
    color,
    size,
    onHand: available,
    available,
    productStatus: "active",
    variantStatus: "active",
  };
}

export const PROVINCES: AdminUnit[] = [
  { code: "01", name: "Hà Nội", level: "province" },
  { code: "79", name: "TP. Hồ Chí Minh", level: "province" },
];
export const WARDS: AdminUnit[] = [
  { code: "00004", name: "Cầu Giấy", level: "ward", parentCode: "01" },
  { code: "00005", name: "Nghĩa Đô", level: "ward", parentCode: "01" },
];

export const SOURCES: TrafficSource[] = [
  { id: S_ZALO, name: "Zalo", active: true, position: 1, version: 1 },
  { id: S_MANUAL, key: "manual", name: "Nhập tay", active: true, position: 2, version: 1 },
  { id: "0195f3b2-0000-7000-8000-00000000f003", name: "Live cũ", active: false, position: 3, version: 1 },
];

export function order(p: Partial<Order> = {}): Order {
  return {
    id: O1,
    code: "DH0001236",
    status: "confirmed",
    customerId: C1,
    subtotal: 690000,
    shippingFee: 30000,
    grandTotal: 720000,
    codAmount: 720000,
    createdAt: "2026-11-19T02:00:00Z",
    items: [],
    ...p,
  };
}

export interface Write {
  key: string | null;
  body: Record<string, unknown>;
}

export interface Fake {
  grants: Grant[];
  writes: Write[];
  /** Trả lần lượt cho mỗi POST /orders: mã HTTP và thân (thân lỗi có "error"). Hết thì 201 order(). */
  replies: { status: number; body: unknown }[];
  coupon: CouponCheck;
  couponCalls: URLSearchParams[];
  duplicates: OrderListItem[];
  duplicateCalls: URLSearchParams[];
  thresholdBp: number;
  /** nextCursor của trang số dư (listBalances); undefined: trang cuối. */
  balanceNextCursor: string | undefined;
  /** Tham số q của mọi lần gọi listCustomers. */
  customerCalls: string[];
}

export const grantsOf = (...perms: string[]): Grant[] =>
  ["order.create", "order.read", "customer.read", "catalog.read", "inventory.read", ...perms].map((permission) => ({
    permission,
    scope: { type: "all", id: "" },
  }));

const json = (b: unknown, status = 200, headers: Record<string, string> = {}) =>
  Promise.resolve(
    new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json", ...headers } }),
  );

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf(),
    writes: [],
    replies: [],
    coupon: { ok: true, discount: 0 },
    couponCalls: [],
    duplicates: [],
    duplicateCalls: [],
    thresholdBp: 1000,
    customerCalls: [],
    balanceNextCursor: undefined,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      const p = url.pathname;
      const q = url.searchParams;
      if (req.method === "POST" && p === "/api/v1/orders") {
        const text = await req.text();
        const body: unknown = JSON.parse(text);
        fake.writes.push({ key: req.headers.get("Idempotency-Key"), body: isRecord(body) ? body : {} });
        const r = fake.replies.shift() ?? { status: 201, body: order() };
        return json(r.body, r.status, r.status === 503 ? { "Retry-After": "0" } : {});
      }
      if (p === "/api/v1/me") {
        const me: Me = { userId: "0195f3b2-0000-7000-8000-0000000000e1", email: "sale@elimaz.vn", grants: fake.grants };
        return json(me);
      }
      if (p === "/api/v1/customers") {
        fake.customerCalls.push(q.get("q") ?? "");
        const term = (q.get("q") ?? "").replace(/\D/g, "");
        const all = [...CUSTOMERS, OTHER];
        if (term.length >= 9)
          return json({ items: all.filter((c) => c.phones.some((x) => x.endsWith(term.slice(-9)))) });
        return json({ items: CUSTOMERS.filter((c) => c.phones.some((x) => x.endsWith(term))) });
      }
      const cm = /^\/api\/v1\/customers\/([^/]+)$/.exec(p);
      if (cm !== null) {
        const c = [...CUSTOMERS, OTHER].find((x) => x.id === cm[1]);
        return c === undefined ? json({ error: "not_found", request_id: "t" }, 404) : json(customerOf(c));
      }
      if (p === "/api/v1/admin-units") {
        const parent = q.get("parent");
        if (parent === null) return json({ items: PROVINCES });
        const term = (q.get("q") ?? "").toLowerCase();
        return json({ items: WARDS.filter((w) => w.parentCode === parent && w.name.toLowerCase().includes(term)) });
      }
      if (p === "/api/v1/catalog/variants/search") {
        const t = (q.get("q") ?? "").toUpperCase();
        return json(HITS.filter((h) => h.sku.startsWith(t)));
      }
      if (p === "/api/v1/inventory/warehouses") {
        const ws: Warehouse[] = [{ id: W1, code: "MAIN", name: "Kho chính", status: "active" }];
        return json(ws);
      }
      if (p === "/api/v1/inventory/balances") {
        const [h1, h2] = HITS;
        return json({
          items: h1 === undefined || h2 === undefined ? [] : [balance(h1, 5), balance(h2, 1)],
          ...(fake.balanceNextCursor === undefined ? {} : { nextCursor: fake.balanceNextCursor }),
        });
      }
      if (p === "/api/v1/coupon-check") {
        fake.couponCalls.push(q);
        return json(fake.coupon);
      }
      if (p === "/api/v1/order-duplicate-check") {
        fake.duplicateCalls.push(q);
        return json({ items: fake.duplicates });
      }
      if (p === "/api/v1/traffic-sources") return json({ items: SOURCES });
      if (p === "/api/v1/order-settings") {
        return json({
          draftTtlMinutes: 1440,
          duplicateWindowMinutes: 1440,
          discountApprovalThresholdBp: fake.thresholdBp,
          returnWarningDays: 7,
          version: 1,
        });
      }
      return json({ error: "not_found", request_id: "t" }, 404);
    }),
  );
  return fake;
}

export function renderForm(props: Partial<OrderFormProps> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <OrderForm onCreated={() => undefined} {...props} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
