// API giả (fetch) cho test vitest của các màn mở sổ và hàng hoàn cũ (Task 273). Chỉ test nhập tệp này.
import { fireEvent } from "@testing-library/react";
import type {
  Grant,
  LegacyReturn,
  OpeningCheckResult,
  OpeningStocktake,
  StocktakeCountImport,
  StocktakeLine,
  VariantSearchHit,
  Warehouse,
} from "../../../api";
import { NOT_FOUND, meReply, reply, stubApi, type WriteQueue } from "../../../app/screenTestkit";

export const W1 = "0195f3b2-0000-7000-8000-00000000a001";
export const W2 = "0195f3b2-0000-7000-8000-00000000a002";
export const ST = "0195f3b2-0000-7000-8000-0000000c0007";
export const V_DAM = "0195f3b2-0000-7000-8000-00000000b015";
export const V_QU = "0195f3b2-0000-7000-8000-00000000b213";

export const WAREHOUSES: Warehouse[] = [
  { id: W1, code: "MAIN", name: "Kho chính", status: "active" },
  { id: W2, code: "PHU", name: "Kho phụ Bình Thạnh", status: "active" },
];

export const HIT_DAM: VariantSearchHit = {
  variantId: V_DAM,
  productId: "p1",
  sku: "DAM0123-DEN-S",
  barcode: "2000000000015",
  productCode: "DAM0123",
  productName: "Đầm suông công sở",
  color: "Đen",
  size: "S",
  price: 1,
  sellable: true,
};
export const HIT_QU: VariantSearchHit = {
  variantId: V_QU,
  productId: "p2",
  sku: "QU0789-DEN-M",
  barcode: "2000000000213",
  productCode: "QU0789",
  productName: "Quần âu ống đứng cạp cao",
  color: "Đen",
  size: "M",
  price: 1,
  sellable: true,
};

export const grantsAll = (...perms: string[]): Grant[] =>
  perms.map((permission) => ({ permission, scope: { type: "all", id: "" } }));

export const OPENING: OpeningStocktake = {
  stocktakeId: ST,
  warehouseId: W1,
  status: "open",
  linesExpected: 412,
  linesCounted: 405,
  unitsCounted: 8214,
};

export interface Fake extends WriteQueue {
  grants: Grant[];
  opening: OpeningStocktake;
  openingList: OpeningStocktake[];
  check: OpeningCheckResult;
  imports: Map<string, StocktakeCountImport>;
  legacy: LegacyReturn[];
  legacyById: Map<string, LegacyReturn>;
  /** Số đếm của từng mã sau mỗi lần cộng (máy chủ giả đếm theo khóa). */
  counts: Map<string, number>;
  seenKeys: Map<string, unknown>;
  /** Mọi lần gửi (kể cả lần mất mạng chưa tới máy chủ): khóa chống trùng. */
  attempts: { path: string; key: string | null }[];
  /** Số lần gửi tới sẽ bị rớt mạng (chưa tới máy chủ). */
  dropNext: number;
  failLegacyList: boolean;
  /** Dòng đếm của phiên (getStocktake). */
  lines: StocktakeLine[];
}

export function newFake(grants: Grant[]): Fake {
  return {
    writes: [],
    replies: [],
    hold: null,
    grants,
    opening: { ...OPENING },
    openingList: [],
    check: { ok: true, changedSince: false, mismatches: [] },
    imports: new Map(),
    legacy: [],
    legacyById: new Map(),
    counts: new Map(),
    seenKeys: new Map(),
    attempts: [],
    dropNext: 0,
    failLegacyList: false,
    lines: [],
  };
}

const HITS = [HIT_DAM, HIT_QU];

/** Dựng fetch giả; trả Fake để test đọc `writes`, `attempts`. */
export function stub(fake: Fake): Fake {
  stubApi(
    fake,
    (f, path, q) => {
      if (path === "/api/v1/me") return meReply(f.grants);
      if (path === "/api/v1/inventory/warehouses") return reply(WAREHOUSES);
      if (path === "/api/v1/catalog/variants/search") {
        const term = (q.get("q") ?? "").trim().toLowerCase();
        return reply(HITS.filter((h) => h.sku.toLowerCase() === term || h.barcode === term));
      }
      if (path === "/api/v1/inventory/opening-stocktakes") {
        const w = q.get("warehouseId");
        return reply({ items: f.openingList.filter((o) => w === null || o.warehouseId === w) });
      }
      if (path === `/api/v1/inventory/opening-stocktakes/${ST}`) return reply(f.opening);
      if (path === `/api/v1/inventory/opening-stocktakes/${ST}/check`) return reply(f.check);
      if (path === `/api/v1/inventory/stocktakes/${ST}`) {
        return reply({
          id: ST,
          code: "MS-0001",
          warehouseId: f.opening.warehouseId,
          status: f.opening.status,
          version: 4,
          createdAt: "2026-11-30T12:02:00Z",
          lines: f.lines,
        });
      }
      if (path.startsWith("/api/v1/inventory/count-imports/")) {
        const imp = f.imports.get(path.split("/").pop() ?? "");
        return imp === undefined ? reply(NOT_FOUND, 404) : reply(imp);
      }
      if (path === "/api/v1/inventory/legacy-returns") {
        if (f.failLegacyList) return reply({ error: "internal", request_id: "t" }, 500);
        return reply({ items: f.legacy });
      }
      if (path.startsWith("/api/v1/inventory/legacy-returns/")) {
        const r = f.legacyById.get(path.split("/").pop() ?? "");
        return r === undefined ? reply(NOT_FOUND, 404) : reply(r);
      }
      return reply(NOT_FOUND, 404);
    },
    (f, path, body) => {
      if (path === `/api/v1/inventory/stocktakes/${ST}/increments`) {
        const key = f.writes.at(-1)?.key ?? "";
        const done = f.seenKeys.get(key);
        if (done !== undefined) return reply(done);
        const code = String(body.code);
        const by = Number(body.by);
        const hit = HITS.find((h) => h.sku === code || h.barcode === code);
        if (hit === undefined) return reply({ error: "unknown_code", request_id: "t" }, 422);
        const n = (f.counts.get(hit.variantId) ?? 0) + by;
        if (n < 0) return reply({ error: "negative_count", request_id: "t" }, 422);
        f.counts.set(hit.variantId, n);
        const result = { variantId: hit.variantId, counted: n };
        f.seenKeys.set(key, result);
        return reply(result);
      }
      return reply({ error: "not_implemented", request_id: "t" }, 501);
    },
  );
  return fake;
}

/** Thay user-event (không có trong repo): bấm và gõ bằng fireEvent. "{Enter}" ở cuối gõ phím Enter. */
export const userEvent = {
  click: (el: Element): Promise<void> => {
    fireEvent.click(el);
    return Promise.resolve();
  },
  type: (el: Element, text: string): Promise<void> => {
    const enter = text.endsWith("{Enter}");
    const value = enter ? text.slice(0, -"{Enter}".length) : text;
    fireEvent.change(el, { target: { value } });
    if (enter) fireEvent.keyDown(el, { key: "Enter" });
    return Promise.resolve();
  },
};

/** jsdom chưa có <dialog>.showModal: dựng bản giả chỉ đặt thuộc tính open (đủ để role "dialog" hiện ra). */
export function stubDialog(): void {
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.open = true;
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.open = false;
  };
}
