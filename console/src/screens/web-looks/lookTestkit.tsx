// API giả (fetch) và cách dựng cho test vitest của màn Bộ phối. Chỉ test nhập tệp này. Trả đúng kiểu của client sinh
// (LookList, LookPreview của Task 454; Product, ProductListItem của catalog).
import type { Look, LookList, LookPhoto, LookPreview, LookProductRef, Product, ProductListItem } from "../../api";
import { emptyWrites, meReply, page, renderScreens, reply, stubApi, type WriteQueue } from "../../app/screenTestkit";
import { grantsOf } from "../web-banners/bannerTestkit";
import { LookEditor } from "./LookEditor";
import { foldText, lookProblem } from "./lookRules";

export { stubDialog } from "../don/donTestkit";

export const L = (n: number) => `0195f3b2-0000-7000-8000-0000000b${String(n).padStart(4, "0")}`;
export const P1 = "0195f3b2-0000-7000-8000-0000000a0001";
export const P2 = "0195f3b2-0000-7000-8000-0000000a0002";
export const P3 = "0195f3b2-0000-7000-8000-0000000a0003";
export const P4 = "0195f3b2-0000-7000-8000-0000000a0004";
/** Ảnh: A1 (P1 Trắng), A2 (P1 Đen), B1 (P2 Đen), C1 (P3 Kem), D1 (P4 ảnh chung). */
export const A1 = "0195f3b2-0000-7000-8000-0000000f0a01";
export const A2 = "0195f3b2-0000-7000-8000-0000000f0a02";
export const B1 = "0195f3b2-0000-7000-8000-0000000f0b01";
export const C1 = "0195f3b2-0000-7000-8000-0000000f0c01";
export const D1 = "0195f3b2-0000-7000-8000-0000000f0d01";
export const CAT1 = "0195f3b2-0000-7000-8000-0000000c0001";

/** Quyền content.read cộng perms (cùng cách dựng của màn banner); mặc định đủ quyền sửa và tìm mẫu. */
export { grantsOf };
export const FULL = ["content.manage", "catalog.read"] as const;

interface Item {
  id: string;
  code: string;
  name: string;
  colors: string[];
  photos: { fileId: string; color?: string }[];
}
export const ITEMS: readonly Item[] = [
  {
    id: P1,
    code: "SM01",
    name: "Áo sơ mi lụa tay bồng",
    colors: ["Trắng", "Đen"],
    photos: [
      { fileId: A1, color: "Trắng" },
      { fileId: A2, color: "Đen" },
    ],
  },
  { id: P2, code: "CV02", name: "Chân váy bút chì", colors: ["Đen"], photos: [{ fileId: B1, color: "Đen" }] },
  { id: P3, code: "GI03", name: "Giày mũi nhọn da bò", colors: ["Kem"], photos: [{ fileId: C1, color: "Kem" }] },
  { id: P4, code: "DA04", name: "Đầm suông cổ vuông", colors: ["Be"], photos: [{ fileId: D1 }] },
];
const item = (id: string): Item => {
  const it = ITEMS.find((x) => x.id === id);
  if (it === undefined) throw new Error(`không có mẫu ${id}`);
  return it;
};

export const ref = (id: string): LookProductRef => ({
  id,
  code: item(id).code,
  name: item(id).name,
  categoryId: CAT1,
  onSale: true,
  inStock: true,
  colors: item(id).colors.map((name) => ({ name })),
});
const lookPhotos = (id: string): LookPhoto[] =>
  item(id).photos.map((p, i) => ({
    productId: id,
    fileId: p.fileId,
    position: i,
    ready: true,
    variants: [{ width: 320, format: "webp", url: `/img/${p.fileId}-320.webp` }],
    ...(p.color === undefined ? {} : { color: p.color }),
  }));

/** Bộ phối thứ n "Bộ n": món P1 (Trắng), ảnh A1, trên trang chủ. */
export const look = (n: number, p: Partial<Look> = {}): Look => ({
  id: L(n),
  onHome: true,
  name: `Bộ ${String(n)}`,
  photoFileId: A1,
  items: [{ productId: P1, color: "Trắng", x: 50, y: 45 }],
  ...p,
});

const productIds = (ls: readonly Look[]) => [...new Set(ls.flatMap((l) => l.items.map((i) => i.productId)))];
const slug = (n: string) =>
  foldText(n)
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/(^-|-$)/g, "");
const pathOf = (l: Look) => `/bo-phoi/${slug(l.name)}-${l.id.replaceAll("-", "").slice(0, 8)}`;

/** Trạng thái mặc định: bộ trên trang chủ đang hiện, bộ khác "disabled". */
function statusesOf(ls: readonly Look[]): LookList["statuses"] {
  return ls.map((l) => ({
    id: l.id,
    shown: l.onHome,
    ...(l.onHome ? {} : { reason: "disabled" as const }),
    autoTitle: l.items.map((i) => item(i.productId).name).join(", "),
    linkKey: l.id.replaceAll("-", "").slice(0, 8),
    path: pathOf(l),
  }));
}

/** Danh sách bộ phối (revision 4) kèm mẫu, ảnh của các món, một danh mục. */
export function list(looks: Look[], p: Partial<LookList> = {}): LookList {
  const ids = productIds(looks);
  return {
    revision: 4,
    looks,
    statuses: statusesOf(looks),
    products: ids.map(ref),
    photos: ids.flatMap(lookPhotos),
    categories: [{ id: CAT1, name: "Áo sơ mi" }],
    ...p,
  };
}

const listItem = (it: Item): ProductListItem => ({
  id: it.id,
  code: it.code,
  name: it.name,
  status: "active",
  categoryPath: [],
  variantCount: it.colors.length,
  updatedAt: "2026-11-20T00:00:00Z",
});

function product(it: Item): Product {
  return {
    id: it.id,
    code: it.code,
    name: it.name,
    description: "",
    article: [],
    articleHtml: "",
    status: "active",
    version: 1,
    variants: it.colors.map((color, i) => ({
      id: `${it.id.slice(0, -2)}9${String(i)}`,
      productId: it.id,
      sku: `${it.code}-${String(i)}`,
      barcode: "",
      color,
      size: "M",
      price: 590_000,
      status: "active",
      version: 1,
    })),
    images: it.photos.map((p, i) => ({
      id: `${p.fileId.slice(0, -2)}99`,
      fileId: p.fileId,
      position: i,
      status: "ready",
      variants: [{ width: 320, format: "webp", url: `/img/${p.fileId}-320.webp` }],
      ...(p.color === undefined ? {} : { color: p.color }),
    })),
  };
}

export interface Fake extends WriteQueue {
  grants: ReturnType<typeof grantsOf>;
  list: LookList;
  /** Câu trả lời lỗi xếp sẵn cho lần lưu (PUT) kế tiếp; xem trước không lấy mất. */
  saveErrors: { status: number; body: unknown }[];
  previews: Record<string, unknown>[];
  previewReply: (looks: Look[]) => LookPreview;
}

function read(fake: Fake, p: string, q: URLSearchParams): Response {
  if (p === "/api/v1/me") return meReply(fake.grants);
  if (p === "/api/v1/content/looks") return reply(fake.list);
  if (p === "/api/v1/catalog/products") {
    const text = q.get("q")?.toLowerCase() ?? "";
    return reply(page(ITEMS.filter((x) => x.name.toLowerCase().includes(text)).map(listItem), q));
  }
  const m = /^\/api\/v1\/catalog\/products\/([^/]+)$/.exec(p);
  const it = ITEMS.find((x) => x.id === m?.[1]);
  if (it !== undefined) return reply(product(it));
  return reply({ error: "not_found", request_id: "t" }, 404);
}

const isLook = (x: unknown): x is Look => typeof x === "object" && x !== null && "id" in x && "items" in x;
export const looksOf = (body: Record<string, unknown>): Look[] =>
  Array.isArray(body.looks) ? body.looks.filter(isLook) : [];

/** Mặc định: lưu trả danh sách mới (revision + 1); xem trước theo previewReply. */
function write(fake: Fake, p: string, body: Record<string, unknown>): Response {
  if (p === "/api/v1/content/looks") {
    const err = fake.saveErrors.shift();
    if (err !== undefined) return reply(err.body, err.status);
    const looks = looksOf(body);
    fake.list = list(looks, { revision: fake.list.revision + 1 });
    return reply(fake.list);
  }
  if (p === "/api/v1/content/looks/preview") {
    fake.previews.push(body);
    // Như ogen: photoFileId là uuid bắt buộc, chuỗi rỗng → 400 cho cả yêu cầu.
    if (looksOf(body).some((l) => l.photoFileId === "")) return reply({ error: "bad_request", request_id: "t" }, 400);
    return reply(fake.previewReply(looksOf(body)));
  }
  return reply({ error: "not_found", request_id: "t" }, 404);
}

export function installFake(l: LookList = list([look(1)])): Fake {
  const fake: Fake = {
    grants: grantsOf(...FULL),
    list: l,
    saveErrors: [],
    previews: [],
    // Như máy chủ (Task 488): bộ chưa đủ (luật lookProblem của màn) bị bỏ, ghi ở skipped; bộ còn lại có trạng thái.
    previewReply: (looks) => {
      const ids = productIds(looks);
      const ok = looks.filter((l) => lookProblem(l) === null);
      return {
        statuses: statusesOf(ok),
        products: ids.map(ref),
        photos: ids.flatMap(lookPhotos),
        skipped: looks
          .filter((l) => lookProblem(l) !== null)
          .map((l) => ({ id: l.id, reason: "look_items_invalid" as const })),
      };
    },
    ...emptyWrites(),
  };
  return stubApi(fake, read, write);
}

/** Dựng màn ở /website/bo-phoi. */
export function renderLooks() {
  return renderScreens(
    [{ path: "/website/bo-phoi", label: "", group: "Website", permission: "", order: 0, element: <LookEditor /> }],
    "/website/bo-phoi",
  );
}
