// API giả (fetch) và cách dựng cho test vitest của màn Đơn. Chỉ test nhập tệp này. Trả đúng kiểu của client sinh.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { vi } from "vitest";
import type {
  DeliveryStats,
  Grant,
  Me,
  Order,
  OrderDuplicate,
  OrderItem,
  OrderListItem,
  Return,
  StaffMember,
  TimelineItem,
  TrafficSource,
  VariantSearchHit,
} from "../../api";
import { screens } from "./route";

export const O1 = "0195f3b2-0000-7000-8000-00000000d001";
export const O2 = "0195f3b2-0000-7000-8000-00000000d002";
export const O3 = "0195f3b2-0000-7000-8000-00000000d003";
export const C1 = "0195f3b2-0000-7000-8000-00000000c001";
export const V1 = "0195f3b2-0000-7000-8000-00000000b001";
export const V2 = "0195f3b2-0000-7000-8000-00000000b002";
export const V3 = "0195f3b2-0000-7000-8000-00000000b003";
export const I1 = "0195f3b2-0000-7000-8000-00000000e001";
export const I2 = "0195f3b2-0000-7000-8000-00000000e002";
export const R1 = "0195f3b2-0000-7000-8000-00000000a101";
export const R2 = "0195f3b2-0000-7000-8000-00000000a102";
export const S_ZALO = "0195f3b2-0000-7000-8000-00000000f002";
export const ME = "0195f3b2-0000-7000-8000-0000000000e1";
export const U2 = "0195f3b2-0000-7000-8000-0000000000e2";

export const LONG_COMPANY = "Công ty TNHH Thương mại Dịch vụ Thời trang Hoàng Gia Minh Châu";

export const SOURCES: TrafficSource[] = [
  { id: S_ZALO, name: "Zalo", active: true, position: 1, version: 1 },
  {
    id: "0195f3b2-0000-7000-8000-00000000f001",
    key: "manual",
    name: "Nhập tay",
    active: true,
    position: 2,
    version: 1,
  },
];

export function listItem(p: Partial<OrderListItem> = {}): OrderListItem {
  return {
    id: O1,
    code: "DH0001236",
    label: "new",
    status: "new",
    customerId: C1,
    customerName: "Trần Thị Thu Hà",
    shipPhone: "+84987654321",
    grandTotal: 1_800_000,
    codAmount: 1_800_000,
    itemCount: 4,
    trafficSourceId: S_ZALO,
    careBy: { id: ME, name: "Phạm Thu Trang" },
    createdAt: "2026-11-18T07:31:00Z",
    duplicateSuspect: false,
    ...p,
  };
}

export function orderItem(p: Partial<OrderItem> = {}): OrderItem {
  return {
    id: I1,
    position: 1,
    variantId: V1,
    sku: "DAM0123-BE-M",
    name: "Đầm maxi lụa Hàn",
    qty: 3,
    unitPrice: 690_000,
    netAmount: 1_860_000,
    lineDiscount: 210_000,
    ...p,
  };
}

const ALL_SCOPE = {
  lines: true,
  discounts: true,
  coupon: true,
  address: true,
  shipping: true,
  carrierNote: true,
  careBy: true,
  trafficSource: true,
};
export const CARE_ONLY = {
  lines: false,
  discounts: false,
  coupon: false,
  address: false,
  shipping: false,
  carrierNote: false,
  careBy: true,
  trafficSource: false,
};

export function order(p: Partial<Order> = {}): Order {
  return {
    id: O1,
    code: "DH0001235",
    status: "new",
    label: "new",
    version: 3,
    customerId: C1,
    customer: {
      id: C1,
      rootId: C1,
      code: "KH000877",
      fullName: LONG_COMPANY,
      stats: {
        orderCount: 23,
        deliveredCount: 21,
        returnedCount: 1,
        totalSpent: 96_400_000,
        lastOrderAt: "2026-11-18T06:05:00Z",
        returnRateBp: 500,
      },
    },
    subtotal: 2_070_000,
    lineDiscountTotal: 210_000,
    orderDiscount: 0,
    couponDiscount: 0,
    discountTotal: 210_000,
    shippingFee: 0,
    grandTotal: 1_860_000,
    prepaidTotal: 0,
    codAmount: 1_860_000,
    createdAt: "2026-11-18T06:05:00Z",
    createdBy: { id: U2, name: "Lê Văn Đức" },
    careBy: { id: U2, name: "Lê Văn Đức" },
    channel: "manual",
    trafficSourceId: S_ZALO,
    shipAddress: {
      name: "Chị Châu",
      phone: "+84903111222",
      line: "Tầng 3, 45 Lê Lợi",
      provinceCode: "79",
      provinceName: "TP. Hồ Chí Minh",
      wardCode: "26734",
      wardName: "Phường Bến Thành",
    },
    addressComplete: true,
    carrierNote: "Hàng sỉ, kiểm số lượng khi nhận.",
    items: [orderItem()],
    editScope: ALL_SCOPE,
    returns: [],
    ...p,
  };
}

export const grantsOf = (...perms: string[]): Grant[] =>
  ["order.read", "customer.read", "catalog.read", ...perms].map((permission) => ({
    permission,
    scope: { type: "all", id: "" },
  }));

export interface Write {
  method: string;
  path: string;
  key: string | null;
  body: Record<string, unknown>;
}

export interface Fake {
  grants: Grant[];
  list: OrderListItem[];
  listNextCursor: string | undefined;
  listCalls: URLSearchParams[];
  orders: Map<string, Order>;
  getCalls: string[];
  timeline: TimelineItem[];
  timelineCalls: number;
  returns: Map<string, Return>;
  duplicates: OrderDuplicate[];
  writes: Write[];
  /** Trả lần lượt cho mỗi yêu cầu ghi: mã HTTP và thân. Hết thì 200 (201 cho tạo) với đơn hiện tại hay phiếu. */
  replies: { status: number; body: unknown }[];
  returnWarningDays: number;
  /** Kết quả tìm biến thể (ô thêm sản phẩm của hộp thoại đổi hàng); mặc định rỗng. */
  variantHits: VariantSearchHit[];
  /** Số lần gọi /api/v1/coupon-check (kiểm mã trước khi lưu). */
  couponChecks: number;
  /** Mức nhận/hoàn của khách (getCustomerDeliveryStats); null → 404 (huy hiệu không hiện). */
  deliveryStats: DeliveryStats | null;
}

const json = (b: unknown, status = 200) =>
  Promise.resolve(new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } }));

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;

const NOT_FOUND = { error: "not_found", request_id: "t" };

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf(),
    list: [],
    listNextCursor: undefined,
    listCalls: [],
    orders: new Map(),
    getCalls: [],
    timeline: [],
    timelineCalls: 0,
    returns: new Map(),
    duplicates: [],
    writes: [],
    replies: [],
    returnWarningDays: 7,
    variantHits: [],
    couponChecks: 0,
    deliveryStats: null,
  };
  // Yêu cầu ghi: ghi lại khóa, thân; trả câu trả lời xếp sẵn hoặc đối tượng hiện tại.
  const onWrite = async (req: Request, p: string): Promise<Response> => {
    const raw = await req.text();
    const parsed: unknown = raw === "" ? {} : JSON.parse(raw);
    fake.writes.push({
      method: req.method,
      path: p,
      key: req.headers.get("Idempotency-Key"),
      body: isRecord(parsed) ? parsed : {},
    });
    const r = fake.replies.shift();
    if (r !== undefined) return json(r.body, r.status);
    if (p.endsWith("/notes")) {
      return json({ id: "n1", body: "x", author: { id: ME, name: "Tôi" }, createdAt: "2026-11-19T01:00:00Z" }, 201);
    }
    const rm = /^\/api\/v1\/returns\/([^/]+)/.exec(p);
    if (rm?.[1] !== undefined) return json(fake.returns.get(rm[1]) ?? NOT_FOUND, 200);
    const om = /^\/api\/v1\/orders\/([^/]+)/.exec(p);
    if (om?.[1] !== undefined) return json(fake.orders.get(om[1]) ?? NOT_FOUND);
    return json({});
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const { pathname: p, searchParams: q } = new URL(req.url);
      if (req.method !== "GET") return onWrite(req, p);
      if (p === "/api/v1/me") {
        const me: Me = { userId: ME, email: "sale@elimaz.vn", grants: fake.grants };
        return json(me);
      }
      if (p === "/api/v1/orders") {
        fake.listCalls.push(q);
        const code = q.get("q");
        if (code !== null && /^DH\d+$/.test(code)) return json({ items: fake.list.filter((o) => o.code === code) });
        const cursor = q.get("cursor");
        return json({
          items: cursor === null ? fake.list : [],
          ...(fake.listNextCursor === undefined || cursor !== null ? {} : { nextCursor: fake.listNextCursor }),
        });
      }
      if (/^\/api\/v1\/customers\/[^/]+\/delivery-stats$/.test(p)) {
        return fake.deliveryStats === null ? json(NOT_FOUND, 404) : json(fake.deliveryStats);
      }
      const tm = /^\/api\/v1\/orders\/([^/]+)\/timeline$/.exec(p);
      if (tm !== null) {
        fake.timelineCalls += 1;
        return json({ items: fake.timeline });
      }
      const om = /^\/api\/v1\/orders\/([^/]+)$/.exec(p);
      if (om?.[1] !== undefined) {
        fake.getCalls.push(om[1]);
        const o = fake.orders.get(om[1]);
        return o === undefined ? json(NOT_FOUND, 404) : json(o);
      }
      const rm = /^\/api\/v1\/returns\/([^/]+)$/.exec(p);
      if (rm?.[1] !== undefined) {
        const r = fake.returns.get(rm[1]);
        return r === undefined ? json(NOT_FOUND, 404) : json(r);
      }
      if (p === "/api/v1/order-duplicates") return json({ items: fake.duplicates });
      if (p === "/api/v1/traffic-sources") return json({ items: SOURCES });
      if (p === "/api/v1/order-settings") {
        return json({
          draftTtlMinutes: 1440,
          duplicateWindowMinutes: 1440,
          discountApprovalThresholdBp: 1000,
          returnWarningDays: fake.returnWarningDays,
          version: 1,
        });
      }
      if (p === "/api/v1/identity/users") {
        const staff: StaffMember[] = [
          {
            id: U2,
            email: "duc@elimaz.vn",
            displayName: "Lê Văn Đức",
            status: "active",
            version: 1,
            createdAt: "",
            grants: [],
          },
          {
            id: ME,
            email: "sale@elimaz.vn",
            displayName: "Phạm Thu Trang",
            status: "active",
            version: 1,
            createdAt: "",
            grants: [],
          },
        ];
        return json(staff);
      }
      if (p === "/api/v1/catalog/variants/search") return json(fake.variantHits);
      if (p === "/api/v1/order-duplicate-check") return json({ items: [] });
      if (p === "/api/v1/coupon-check") {
        fake.couponChecks += 1;
        return json({ ok: true, discount: 0 });
      }
      if (p === "/api/v1/admin-units") return json({ items: [] });
      const cm = /^\/api\/v1\/customers\/([^/]+)$/.exec(p);
      if (cm !== null) return json(NOT_FOUND, 404);
      if (p === "/api/v1/customers") return json({ items: [] });
      if (p === "/api/v1/inventory/warehouses") return json([]);
      return json(NOT_FOUND, 404);
    }),
  );
  return fake;
}

/** Dựng các màn của route.tsx ở đường `path`; trả QueryClient để test bắn báo hiệu realtime. */
export function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          {screens.map((s) => (
            <Route key={s.path} path={s.path} element={s.element} />
          ))}
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { qc, ...view };
}

/** jsdom chưa có showModal của <dialog>. */
export function stubDialog() {
  HTMLDialogElement.prototype.showModal = function (this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function (this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
}
