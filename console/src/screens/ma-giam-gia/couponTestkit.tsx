// API giả (fetch) và cách dựng cho test vitest của màn Mã giảm giá. Chỉ test nhập tệp này. Trả đúng kiểu của client sinh.
import { vi } from "vitest";
import type { Coupon, CouponBatch, CouponRedemption, Grant, Me } from "../../api";
import { NOT_FOUND, bodyOf, renderScreens, reply, type Write } from "../../app/screenTestkit";
import { screens } from "./route";

export { stubDialog } from "../don/donTestkit";

export const ID1 = "0195f3b2-0000-7000-8000-00000000d001";
export const ID2 = "0195f3b2-0000-7000-8000-00000000d002";
export const BATCH = "0195f3b2-0000-7000-8000-00000000e001";
export const ORDER = "0195f3b2-0000-7000-8000-00000000f001";

const BASE: Coupon = {
  id: ID1,
  code: "THU10",
  name: "Giảm 10% mùa thu cho khách quay lại",
  discountKind: "percent",
  value: 1000,
  maxDiscount: 150_000,
  minSubtotal: 500_000,
  channels: [],
  maxUses: 200,
  usedCount: 87,
  remainingUses: 113,
  releaseOnCancel: true,
  validFrom: "2026-10-31T17:00:00Z",
  validTo: "2099-11-30T16:59:00Z",
  status: "active",
  bound: false,
  createdAt: "2026-10-30T00:00:00Z",
  version: 3,
};

/** Mã THU10 đã dùng 87/200 lượt, sửa bằng p. */
export const coupon = (p: Partial<Coupon> = {}): Coupon => Object.assign({}, BASE, p);

export function batch(p: Partial<CouponBatch> = {}): CouponBatch {
  return {
    id: BATCH,
    name: "Sinh nhật tháng 12",
    prefix: "SN",
    quantity: 500,
    template: {
      name: "Sinh nhật tháng 12",
      discountKind: "percent",
      value: 1500,
      validFrom: "2026-11-30T17:00:00Z",
      validTo: "2026-12-31T16:59:00Z",
    },
    createdAt: "2026-11-19T01:00:00Z",
    ...p,
  };
}

export const redemption = (p: Partial<CouponRedemption> = {}): CouponRedemption => ({
  id: "r1",
  orderId: ORDER,
  orderCode: "DH0001236",
  discount: 150_000,
  status: "active",
  createdAt: "2026-11-18T01:00:00Z",
  ...p,
});

export const grantsOf = (...perms: string[]): Grant[] =>
  ["promotion.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

export interface Fake {
  grants: Grant[];
  coupons: Coupon[];
  listCalls: URLSearchParams[];
  batches: Map<string, CouponBatch>;
  /** Mã của đợt (listCoupons?batchId=…), trả theo trang `limit`. */
  batchCodes: string[];
  redemptions: CouponRedemption[];
  writes: Write[];
  replies: { status: number; body: unknown }[];
  /** Có thì yêu cầu ghi chờ lời hứa này rồi mới trả (xem trạng thái "đang gửi"). */
  hold: Promise<void> | null;
}

function batchPage(fake: Fake, q: URLSearchParams): Response {
  const limit = Number(q.get("limit") ?? "50");
  const from = Number(q.get("cursor") ?? "0");
  const codes = fake.batchCodes.slice(from, from + limit);
  const items = codes.map((code, i) => coupon({ id: `b-${String(from + i)}`, code, batchId: BATCH, usedCount: 0 }));
  const next = from + limit < fake.batchCodes.length ? { nextCursor: String(from + limit) } : {};
  return reply({ items, ...next });
}

function read(fake: Fake, p: string, q: URLSearchParams): Response {
  if (p === "/api/v1/me") return reply({ userId: "me", email: "a@elimaz.vn", grants: fake.grants } satisfies Me);
  if (p === "/api/v1/coupons") {
    fake.listCalls.push(q);
    if (q.get("batchId") !== null && fake.batchCodes.length > 0) return batchPage(fake, q);
    return reply({ items: fake.coupons });
  }
  const red = /^\/api\/v1\/coupons\/([^/]+)\/redemptions$/.exec(p);
  if (red !== null) return reply({ items: fake.redemptions });
  const one = /^\/api\/v1\/coupons\/([^/]+)$/.exec(p);
  if (one !== null) {
    const c = fake.coupons.find((x) => x.id === one[1]);
    return c === undefined ? reply(NOT_FOUND, 404) : reply(c);
  }
  const b = /^\/api\/v1\/coupon-batches\/([^/]+)$/.exec(p);
  if (b !== null) {
    const v = fake.batches.get(b[1] ?? "");
    return v === undefined ? reply(NOT_FOUND, 404) : reply(v);
  }
  return reply(NOT_FOUND, 404);
}

function writeReply(fake: Fake, p: string, body: Record<string, unknown>): Response {
  if (p === "/api/v1/coupons") {
    const c = coupon({ id: ID2, usedCount: 0, code: typeof body.code === "string" ? body.code : "AUTO1234" });
    fake.coupons.push(c);
    return reply(c, 201);
  }
  if (p === "/api/v1/coupon-batches") {
    const b = batch({ name: typeof body.name === "string" ? body.name : "" });
    fake.batches.set(b.id, b);
    return reply(b, 201);
  }
  const c = fake.coupons.find((x) => p.endsWith(x.id));
  return c === undefined ? reply(NOT_FOUND, 404) : reply({ ...c, version: c.version + 1 });
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf("promotion.manage"),
    coupons: [],
    listCalls: [],
    batches: new Map(),
    batchCodes: [],
    redemptions: [],
    writes: [],
    replies: [],
    hold: null,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      if (req.method === "GET") return read(fake, url.pathname, url.searchParams);
      const body = await bodyOf(req);
      fake.writes.push({ method: req.method, path: url.pathname, key: req.headers.get("Idempotency-Key"), body });
      if (fake.hold !== null) await fake.hold;
      const queued = fake.replies.shift();
      return queued === undefined ? writeReply(fake, url.pathname, body) : reply(queued.body, queued.status);
    }),
  );
  return fake;
}

export const renderAt = (path: string) =>
  renderScreens(screens, path, [{ path: "/don/:id", element: <p>Trang đơn</p> }]);
