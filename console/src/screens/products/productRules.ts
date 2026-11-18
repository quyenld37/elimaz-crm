// Luật hiển thị của màn Danh sách sản phẩm (kế hoạch M1 Task 50; mockup docs/mockups/m1/san-pham-danh-sach.html).
// Tìm không dấu, lọc danh mục gồm con, tính "có thể bán" đều ở API (catalog.ListProducts); ở đây chỉ dựng chữ.
import type { Category, FileVariant, ProductCover, ProductListItem, ProductStatus, Warehouse } from "../../api";

const NUM = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 0 });

/** Tiền (số nguyên đồng) kiểu "350.000 ₫". */
export function formatMoney(v: number): string {
  return `${NUM.format(v)} ₫`;
}

/** Một giá, hay khoảng "thấp – cao" khi các biến thể khác giá; mẫu chưa có biến thể → "—". */
export function priceText(p: ProductListItem): string {
  if (p.minPrice === undefined || p.maxPrice === undefined) return "—";
  if (p.minPrice === p.maxPrice) return formatMoney(p.minPrice);
  return `${formatMoney(p.minPrice)} – ${formatMoney(p.maxPrice)}`;
}

export interface AvailableView {
  text: string;
  tone: "short" | "zero" | "normal";
  note?: string;
}

/**
 * Cột "Có thể bán" (chỉ có khi gửi warehouseId). Âm (kiểm kho đếm thiếu, Q8) → đỏ kèm "Thiếu N so với phần giữ";
 * 0 của mẫu đang bán → "Hết hàng".
 */
export function availableView(p: ProductListItem): AvailableView | null {
  const a = p.available;
  if (a === undefined) return null;
  if (a < 0) return { text: `−${NUM.format(-a)}`, tone: "short", note: `Thiếu ${NUM.format(-a)} so với phần giữ` };
  if (a === 0 && p.status === "active") return { text: "0", tone: "zero", note: "Hết hàng" };
  return { text: NUM.format(a), tone: "normal" };
}

/** Chữ trên chip trạng thái của dòng. */
export const STATUS_CHIP: Readonly<Record<ProductStatus, { label: string; className: string }>> = {
  active: { label: "Đang bán", className: "chip chip-ok" },
  draft: { label: "Đang nhập · ẩn với khách", className: "chip" },
  discontinued: { label: "Ngừng bán", className: "chip chip-line" },
};

/** Chữ của lựa chọn trong ô lọc trạng thái. */
export const STATUS_FILTER: Readonly<Record<ProductStatus, string>> = {
  active: "Đang bán",
  draft: "Đang nhập",
  discontinued: "Ngừng bán",
};
export const STATUSES: readonly ProductStatus[] = ["active", "draft", "discontinued"];

export interface Option {
  id: string;
  label: string;
}

/** Danh mục đang dùng theo cây (con ngay sau cha, xếp theo position rồi tên), nhãn "Cha > Con". */
export function categoryOptions(cats: readonly Category[]): Option[] {
  const active = cats.filter((c) => c.status === "active");
  const byParent = new Map<string, Category[]>();
  for (const c of active) {
    const k = c.parentId ?? "";
    byParent.set(k, [...(byParent.get(k) ?? []), c]);
  }
  const out: Option[] = [];
  const walk = (parent: string, prefix: string) => {
    const kids = [...(byParent.get(parent) ?? [])].sort(
      (a, b) => a.position - b.position || a.name.localeCompare(b.name, "vi"),
    );
    for (const c of kids) {
      const label = prefix === "" ? c.name : `${prefix} > ${c.name}`;
      out.push({ id: c.id, label });
      walk(c.id, label);
    }
  };
  walk("", "");
  return out;
}

/**
 * Kho để tính "Có thể bán": kho đang dùng trong danh sách listWarehouses trả (máy chủ đã lọc theo inventory.read, kể
 * cả quyền toàn shop), theo thứ tự API, nhãn là tên kho (E-07 review độc lập M1; thay cách tạm của Task 50).
 */
export function warehouseOptions(warehouses: readonly Warehouse[]): Option[] {
  return warehouses.filter((w) => w.status === "active").map((w) => ({ id: w.id, label: w.name }));
}

export interface Filters {
  q: string;
  status: ProductStatus | "";
  categoryId: string;
  tagId: string;
  warehouseId: string;
}

export const DEFAULT_FILTERS: Filters = { q: "", status: "", categoryId: "", tagId: "", warehouseId: "" };

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const uuidOr = (v: string | null) => (v !== null && UUID.test(v) ? v : "");
const isStatus = (v: string | null): v is ProductStatus => STATUSES.some((s) => s === v);
/** Giới hạn của tham số q trong hợp đồng listProducts. */
export const MAX_Q = 100;

/** Bộ lọc từ URL; giá trị lạ bị bỏ (API sẽ từ chối), kho phải là kho có quyền, mặc định kho đầu. */
export function readFilters(sp: URLSearchParams, warehouses: readonly string[]): Filters {
  const status = sp.get("status");
  const kho = sp.get("kho");
  return {
    q: (sp.get("q") ?? "").slice(0, MAX_Q),
    status: isStatus(status) ? status : "",
    categoryId: uuidOr(sp.get("category")),
    tagId: uuidOr(sp.get("tag")),
    warehouseId: kho !== null && warehouses.includes(kho) ? kho : (warehouses[0] ?? ""),
  };
}

export function writeFilters(f: Filters): URLSearchParams {
  const sp = new URLSearchParams();
  if (f.q !== "") sp.set("q", f.q);
  if (f.status !== "") sp.set("status", f.status);
  if (f.categoryId !== "") sp.set("category", f.categoryId);
  if (f.tagId !== "") sp.set("tag", f.tagId);
  if (f.warehouseId !== "") sp.set("kho", f.warehouseId);
  return sp;
}

/** Có lọc hay tìm (kho chỉ là cách tính cột, không phải bộ lọc). */
export function hasFilters(f: Filters): boolean {
  return f.q.trim() !== "" || f.status !== "" || f.categoryId !== "" || f.tagId !== "";
}

export interface FilterNames {
  category: ReadonlyMap<string, string>;
  tag: ReadonlyMap<string, string>;
}

/** Câu "Đang lọc: …" của trạng thái E (không tính ô tìm, đã nằm trong tiêu đề). */
export function filterSummary(f: Filters, names: FilterNames): string {
  const parts: string[] = [];
  if (f.status !== "") parts.push(`Trạng thái ${STATUS_FILTER[f.status]}`);
  if (f.categoryId !== "") parts.push(`danh mục ${names.category.get(f.categoryId) ?? "đã chọn"}`);
  if (f.tagId !== "") parts.push(`nhãn ${names.tag.get(f.tagId) ?? "đã chọn"}`);
  return parts.join(", ");
}

export type CoverPicture =
  | { kind: "none" }
  | { kind: "lost" }
  | { kind: "ready"; sources: { type: string; srcSet: string }[]; src: string; srcSet: string };

const srcSetOf = (vs: readonly FileVariant[]) =>
  [...vs]
    .sort((a, b) => a.width - b.width)
    .map((v) => `${v.url} ${String(v.width)}w`)
    .join(", ");

/** Ảnh bìa: chưa có, mất (ô trống có ghi chú), hay <picture> avif → webp → jpeg. */
export function coverPicture(cover: ProductCover | undefined): CoverPicture {
  if (cover === undefined) return { kind: "none" };
  const vs = cover.status === "ready" ? (cover.variants ?? []) : [];
  if (vs.length === 0) return { kind: "lost" };
  const of = (f: FileVariant["format"]) => vs.filter((v) => v.format === f);
  const sources = (["avif", "webp"] as const)
    .filter((f) => of(f).length > 0)
    .map((f) => ({ type: `image/${f}`, srcSet: srcSetOf(of(f)) }));
  const base = of("jpeg").length > 0 ? of("jpeg") : vs;
  const smallest = [...base].sort((a, b) => a.width - b.width).at(0);
  if (smallest === undefined) return { kind: "lost" };
  return { kind: "ready", sources, src: smallest.url, srcSet: srcSetOf(base) };
}
