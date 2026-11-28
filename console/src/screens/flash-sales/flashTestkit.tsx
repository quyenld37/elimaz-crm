// API giả (fetch) và cách dựng cho test vitest của màn Đợt flash sale (Task 206). Chỉ test nhập tệp này. Trả đúng kiểu
// của client sinh (FlashSale, FlashItem, FlashLive, VariantSearchHit).
import { cleanup, fireEvent } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";
import type {
  FlashItem,
  FlashLive,
  FlashLiveItem,
  FlashSale,
  FlashSaleSummary,
  Grant,
  VariantSearchHit,
} from "../../api";
import {
  NOT_FOUND,
  emptyWrites,
  meReply,
  page,
  renderScreens,
  reply,
  stubApi,
  type WriteQueue,
} from "../../app/screenTestkit";
import { screens } from "./route";

import { stubDialog } from "../don/donTestkit";

export const S1 = "0195f3b2-0000-7000-8000-00000000f001";
export const S2 = "0195f3b2-0000-7000-8000-00000000f002";
export const S3 = "0195f3b2-0000-7000-8000-00000000f003";
export const S4 = "0195f3b2-0000-7000-8000-00000000f004";
export const SNEW = "0195f3b2-0000-7000-8000-00000000f0ff";
/** Tên 120 ký tự (dữ liệu khó của mockup). */
export const LONG_NAME =
  "Flash sale mừng sinh nhật ELIMAZ sáu tuổi: đầm công sở, áo sơ mi lụa, chân váy bút chì và áo khoác dạ, đồng giá từ 199k!";

export const grantsOf = (...perms: string[]): Grant[] =>
  ["promotion.flash.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

/** Đợt của cửa hàng; sửa bằng p. Mặc định: nháp "Đêm 12.12 khung 1", 12/12 00:00 → 02:00 giờ VN. */
export function sale(p: Partial<FlashSaleSummary> = {}): FlashSaleSummary {
  return {
    id: S1,
    name: "Đêm 12.12 khung 1",
    startsAt: "2026-12-11T17:00:00Z",
    endsAt: "2026-12-11T19:00:00Z",
    status: "draft",
    version: 3,
    itemCount: 2,
    quantity: 240,
    sold: 0,
    createdAt: "2026-11-20T00:00:00Z",
    updatedAt: "2026-11-20T00:00:00Z",
    ...p,
  };
}

/** Món của đợt; i là số thứ tự để mã không trùng. */
export function item(i: number, p: Partial<FlashItem> = {}): FlashItem {
  return {
    id: `0195f3b2-0000-7000-8000-0000000d${String(i).padStart(4, "0")}`,
    variantId: `0195f3b2-0000-7000-8000-0000000b${String(i).padStart(4, "0")}`,
    sellerId: "0195f3b2-0000-7000-8000-0000000a0001",
    sku: `SP${String(i).padStart(4, "0")}-DEN-M`,
    name: `Đầm suông ${String(i)} - Đen / M`,
    listPrice: 690_000,
    salePrice: 199_000,
    quantity: 100,
    perCustomerLimit: 1,
    holdMinutes: 10,
    version: 1,
    sellable: 820,
    ...p,
  };
}

export const liveItem = (it: FlashItem, p: Partial<FlashLiveItem> = {}): FlashLiveItem => ({
  itemId: it.id,
  quantity: it.quantity,
  available: it.quantity,
  held: 0,
  sold: 0,
  released: 0,
  buyers: 0,
  ...p,
});

export const HIT_BE: VariantSearchHit = {
  variantId: "0195f3b2-0000-7000-8000-0000000b9001",
  productId: "0195f3b2-0000-7000-8000-0000000a9001",
  sku: "DS2201-BE-M",
  barcode: "893000000001",
  productCode: "DS2201",
  productName: "Đầm suông cổ vuông tay phồng",
  color: "Be",
  size: "M",
  price: 690_000,
  sellable: true,
};
export const HIT_OFF: VariantSearchHit = {
  ...HIT_BE,
  variantId: "0195f3b2-0000-7000-8000-0000000b9002",
  sku: "DS1802-REU-S",
  productName: "Đầm suông linen",
  color: "Rêu",
  size: "S",
  sellable: false,
};

export interface Fake extends WriteQueue {
  grants: Grant[];
  sales: FlashSaleSummary[];
  /** Món theo mã đợt. */
  items: Record<string, FlashItem[]>;
  /** Số trực tiếp theo mã đợt; thiếu thì mỗi món còn nguyên số suất. */
  live: Record<string, FlashLiveItem[]>;
  hits: VariantSearchHit[];
  /** Tham số các lần gọi danh sách đợt. */
  listCalls: URLSearchParams[];
  /** Số lần tải chi tiết, số trực tiếp. */
  detailCalls: number;
  liveCalls: number;
  /** Lỗi đọc danh sách (500) khi true. */
  listFails: boolean;
  /** Lỗi đọc số trực tiếp (500) khi true. */
  liveFails: boolean;
}

const sum = (s: FlashSaleSummary, items: FlashItem[]): FlashSale => ({
  sale: { ...s, itemCount: items.length },
  items,
});

function read(fake: Fake, p: string, q: URLSearchParams): Response {
  if (p === "/api/v1/me") return meReply(fake.grants);
  if (p === "/api/v1/catalog/variants/search") {
    const text = q.get("q")?.toLowerCase() ?? "";
    return reply(fake.hits.filter((h) => `${h.sku} ${h.productName}`.toLowerCase().includes(text)));
  }
  if (p === "/api/v1/flash-sales") {
    fake.listCalls.push(q);
    if (fake.listFails) return reply({ error: "internal", request_id: "t" }, 500);
    const status = q.get("status");
    return reply(page(status === null ? fake.sales : fake.sales.filter((s) => s.status === status), q));
  }
  const m = /^\/api\/v1\/flash-sales\/([^/]+)(\/live)?$/.exec(p);
  const s = fake.sales.find((r) => r.id === m?.[1]);
  if (m === null || s === undefined) return reply(NOT_FOUND, 404);
  const items = fake.items[s.id] ?? [];
  if (m[2] === "/live") {
    fake.liveCalls++;
    if (fake.liveFails) return reply({ error: "internal", request_id: "t" }, 500);
    const live: FlashLive = { items: fake.live[s.id] ?? items.map((i) => liveItem(i)) };
    return reply(live);
  }
  fake.detailCalls++;
  return reply(sum(s, items));
}

/** Câu trả lời mặc định của yêu cầu ghi: đổi trạng thái, tăng version như máy chủ. */
function write(fake: Fake, p: string, body: Record<string, unknown>): Response {
  if (p === "/api/v1/flash-sales") {
    const created = sale({
      id: SNEW,
      version: 1,
      itemCount: 0,
      name: String(body.name),
      startsAt: String(body.startsAt),
      endsAt: String(body.endsAt),
    });
    fake.sales = [created, ...fake.sales];
    fake.items[SNEW] = [];
    return reply(sum(created, []), 201);
  }
  const m = /^\/api\/v1\/flash-sales\/([^/]+)(?:\/(schedule|close)|\/items(?:\/([^/]+)(\/add-slots)?)?)?$/.exec(p);
  const s = fake.sales.find((r) => r.id === m?.[1]);
  if (m === null || s === undefined) return reply(NOT_FOUND, 404);
  const items = fake.items[s.id] ?? [];
  const setSale = (next: FlashSaleSummary) => {
    fake.sales = fake.sales.map((r) => (r.id === s.id ? next : r));
    return reply(sum(next, items));
  };
  if (m[2] === "schedule") return setSale({ ...s, status: "scheduled", version: s.version + 1 });
  if (m[2] === "close") return setSale({ ...s, status: "closed", closedReason: "manual", version: s.version + 1 });
  const target = items.find((i) => i.id === m[3]);
  if (m[4] !== undefined && target !== undefined) {
    return reply({ ...target, quantity: target.quantity + Number(body.add), version: target.version + 1 });
  }
  if (m[3] !== undefined) return target === undefined ? reply(NOT_FOUND, 404) : reply({ ...target, ...body }); // sửa món
  if (p.endsWith("/items")) return reply(item(99, { quantity: Number(body.quantity) }), 201);
  return setSale({ ...s, ...(typeof body.name === "string" ? { name: body.name } : {}), version: s.version + 1 }); // sửa đợt
}

export function installFake(): Fake {
  const items = [item(1), item(2, { quantity: 140 })];
  const fake: Fake = {
    grants: grantsOf("promotion.flash.manage"),
    sales: [sale()],
    items: { [S1]: items },
    live: {},
    hits: [HIT_BE, HIT_OFF],
    listCalls: [],
    detailCalls: 0,
    liveCalls: 0,
    listFails: false,
    liveFails: false,
    ...emptyWrites(),
  };
  return stubApi(fake, read, write);
}

export const renderAt = (path: string) => renderScreens(screens, path);

/** Mỗi test: dựng API giả (đưa cho `set`), thay hộp thoại bằng bản jsdom hiểu, dọn sau test. */
export function hookFlashFake(set: (f: Fake) => void) {
  beforeEach(() => {
    stubDialog();
    set(installFake());
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });
}

/** Gõ v vào ô el. */
export const typeInto = (el: HTMLElement, v: string) => {
  fireEvent.change(el, { target: { value: v } });
};
