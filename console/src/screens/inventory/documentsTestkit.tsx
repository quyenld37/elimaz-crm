// API giả (fetch) và cách dựng dùng chung cho test vitest của màn Phiếu kho (DocumentList, DocumentEdit). Chỉ test nhập
// tệp này; mã chạy thật không dùng. Trả đúng kiểu của client sinh (src/api/types.gen.ts).
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { vi } from "vitest";
import type {
  Balance,
  Grant,
  Me,
  StockDocument,
  StockDocumentLine,
  Supplier,
  VariantSearchHit,
  Warehouse,
} from "../../api";
import { queryKey } from "../../app/queryClient";

export const W1 = "0195f3b2-0000-7000-8000-00000000a001";
export const W2 = "0195f3b2-0000-7000-8000-00000000a002";
export const V1 = "0195f3b2-0000-7000-8000-00000000b001";
export const V2 = "0195f3b2-0000-7000-8000-00000000b002";
export const DOC_EXPORT = "0195f3b2-0000-7000-8000-00000000d045";
export const DOC_IMPORT = "0195f3b2-0000-7000-8000-00000000d124";

export const WAREHOUSES: Warehouse[] = [{ id: W1, code: "MAIN", name: "Kho chính", status: "active" }];
/** Hai kho đang dùng (test phạm vi kho của quyền, E-01). */
export const TWO_WAREHOUSES: Warehouse[] = [...WAREHOUSES, { id: W2, code: "SUB", name: "Kho phụ", status: "active" }];

const H1: VariantSearchHit = {
  variantId: V1,
  productId: "0195f3b2-0000-7000-8000-00000000c001",
  sku: "QU0789-DEN-M",
  barcode: "2000000000213",
  productCode: "QU0789",
  productName: "Quần âu ống đứng cạp cao",
  color: "Đen",
  size: "M",
  price: 450000,
  sellable: true,
};
const H2: VariantSearchHit = {
  variantId: V2,
  productId: "0195f3b2-0000-7000-8000-00000000c002",
  sku: "VAY0101-BE-M",
  barcode: "2000000000503",
  productCode: "VAY0101",
  productName: "Chân váy bút chì xẻ sau",
  color: "Be",
  size: "M",
  price: 390000,
  sellable: true,
};
export const HITS: VariantSearchHit[] = [H1, H2];

export function balanceOf(h: VariantSearchHit, onHand: number, held: number): Balance {
  return {
    variantId: h.variantId,
    sku: h.sku,
    barcode: h.barcode,
    productCode: h.productCode,
    productName: h.productName,
    color: h.color,
    size: h.size,
    productStatus: "active",
    variantStatus: "active",
    onHand,
    held,
    inTransit: 0,
    damaged: 0,
    available: onHand - held,
  };
}

export function doc(id: string, s: Partial<StockDocument>): StockDocument {
  return {
    id,
    code: "PX-000045",
    warehouseId: W1,
    kind: "export",
    status: "draft",
    supplierName: "",
    note: "",
    version: 1,
    createdAt: "2026-11-18T07:30:00Z",
    lines: [],
    ...s,
  };
}

export const grantsOf = (...perms: string[]): Grant[] =>
  ["realtime.subscribe", "catalog.read", "inventory.read", ...perms].map((permission) => ({
    permission,
    scope: { type: "all", id: "" },
  }));

/** Grant của quyền ở một kho (phạm vi "warehouse"), như máy chủ trả trong Me.grants. */
export const grantAt = (permission: string, warehouseId: string): Grant => ({
  permission,
  scope: { type: "warehouse", id: warehouseId },
});

export interface Write {
  method: string;
  path: string;
  key: string | null;
  body: unknown;
}

export interface Fake {
  /** Kho listWarehouses trả (máy chủ đã lọc theo inventory.read). */
  warehouses: Warehouse[];
  docs: Map<string, StockDocument>;
  suppliers: Supplier[];
  balances: Balance[];
  writes: Write[];
  /** Tham số của mọi lần gọi listStockDocuments. */
  listCalls: URLSearchParams[];
  /** Lỗi cho lần gọi ghi sổ kế tiếp (một lần). */
  failPost: { status: number; body: Record<string, unknown> } | null;
  /** Ghi sổ chờ tới khi gọi hàm này (để thử bấm đúp). */
  holdPost: (() => void) | null;
}

const json = (b: unknown, status = 200) =>
  Promise.resolve(new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } }));

let seq = 0;

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;
const isLine = (v: unknown): v is StockDocumentLine =>
  isRecord(v) && typeof v.variantId === "string" && typeof v.quantity === "number";
function parse(text: string): unknown {
  const v: unknown = JSON.parse(text);
  return v;
}

/** Cài fetch giả; trả trạng thái để test đọc và chỉnh. */
export function installFake(): Fake {
  const fake: Fake = {
    warehouses: WAREHOUSES,
    docs: new Map(),
    suppliers: [{ id: "s-hoa-sen", name: "Xưởng may Hoa Sen", phone: "", note: "", status: "active", version: 1 }],
    balances: [balanceOf(H1, 6, 4), balanceOf(H2, 9, 0)],
    writes: [],
    listCalls: [],
    failPost: null,
    holdPost: null,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      const p = url.pathname;
      const m = req.method;
      if (m !== "GET") {
        const text = await req.text();
        fake.writes.push({
          method: m,
          path: p,
          key: req.headers.get("Idempotency-Key"),
          body: text === "" ? null : parse(text),
        });
      }
      const body = fake.writes.at(-1)?.body;
      const rec: Record<string, unknown> = isRecord(body) ? body : {};
      if (p === "/api/v1/inventory/warehouses") return json(fake.warehouses);
      if (p === "/api/v1/inventory/balances") return json({ items: fake.balances });
      if (p === "/api/v1/catalog/variants/search") {
        const q = (url.searchParams.get("q") ?? "").toUpperCase();
        return json(HITS.filter((h) => h.sku.startsWith(q) || h.barcode === q));
      }
      if (p === "/api/v1/inventory/suppliers" && m === "GET") {
        // Như máy chủ (tìm theo tên): mọi từ của q đều có trong tên.
        const words = (url.searchParams.get("q") ?? "").toLowerCase().split(/\s+/).filter(Boolean);
        return json(fake.suppliers.filter((s) => words.every((w) => s.name.toLowerCase().includes(w))));
      }
      if (p === "/api/v1/inventory/suppliers" && m === "POST") {
        const s: Supplier = {
          id: `s-${String(++seq)}`,
          name: String(rec.name),
          phone: "",
          note: "",
          status: "active",
          version: 1,
        };
        fake.suppliers.push(s);
        return json(s, 201);
      }
      if (p === "/api/v1/inventory/documents" && m === "GET") {
        fake.listCalls.push(url.searchParams);
        return json({ items: [...fake.docs.values()].map((d) => ({ ...d, lines: [] })) });
      }
      if (p === "/api/v1/inventory/documents" && m === "POST") {
        const sup = fake.suppliers.find((s) => s.id === rec.supplierId);
        const d = doc(`0195f3b2-0000-7000-8000-0000000e${String(++seq).padStart(4, "0")}`, {
          code: "PN-000125",
          kind: rec.kind === "import" ? "import" : "export",
          warehouseId: String(rec.warehouseId),
          note: typeof rec.note === "string" ? rec.note : "",
          supplierName: sup?.name ?? "",
          ...(sup !== undefined && { supplierId: sup.id }),
        });
        fake.docs.set(d.id, d);
        return json(d, 201);
      }
      const hit = /^\/api\/v1\/inventory\/documents\/([^/]+)(?:\/(lines|post|cancel))?$/.exec(p);
      if (hit !== null) {
        const d = fake.docs.get(hit[1] ?? "");
        if (d === undefined) return json({ error: "not_found", request_id: "t" }, 404);
        if (hit[2] === undefined) return json(d);
        if (rec.version !== d.version) return json({ error: "version_conflict", request_id: "t" }, 409);
        let next: StockDocument;
        if (hit[2] === "lines")
          next = { ...d, lines: Array.isArray(rec.lines) ? rec.lines.filter(isLine) : [], version: d.version + 1 };
        else if (hit[2] === "cancel") next = { ...d, status: "cancelled", version: d.version + 1 };
        else {
          // holdPost khác null: giữ câu trả lời tới khi test gọi fake.holdPost().
          await new Promise<void>((resolve) => {
            if (fake.holdPost === null) resolve();
            else fake.holdPost = resolve;
          });
          const f = fake.failPost;
          if (f !== null) {
            fake.failPost = null;
            return json({ request_id: "t", ...f.body }, f.status);
          }
          next = { ...d, status: "posted", postedAt: "2026-11-18T08:15:00Z", version: d.version + 1 };
        }
        fake.docs.set(d.id, next);
        return json(next);
      }
      return json({ error: "not_implemented", request_id: "t" }, 501);
    }),
  );
  return fake;
}

/** Dựng các route của màn phiếu ở path, với /api/v1/me có sẵn như khung. */
export function renderDocs(grants: Grant[], path: string, ui: { list: ReactNode; edit: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const me: Me = { userId: "u1", email: "lan@elimaz.vn", grants };
  qc.setQueryData(queryKey("identity.me"), me);
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/kho/phieu" element={ui.list} />
          <Route path="/kho/phieu/:id" element={ui.edit} />
          <Route path="/kho/ton" element={<p>màn tồn</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
