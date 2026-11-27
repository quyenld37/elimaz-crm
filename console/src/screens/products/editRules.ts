// Luật của biểu mẫu sửa mẫu (kế hoạch M1 Task 324; mockup docs/mockups/m1/san-pham-sua.html). Barcode tự sinh, chuẩn
// hóa SKU, kiểm trùng với mẫu khác đều ở API; ở đây chỉ dựng lưới màu × size, gợi ý SKU, đọc giá, báo lỗi trước cho
// ô trống/sai dạng, và chia lần lưu thành các bước theo version (docs/tasks/task-324.md, Soát thiết kế 6.1).
import type {
  AddVariantRequest,
  CreateProductRequest,
  MeasureCm,
  Product,
  ProductClassification,
  ProductStatus,
  UpdateProductRequest,
  UpdateVariantRequest,
  Variant,
  VariantStatus,
} from "../../api";
import { articleBody, checkArticle, editBlocks, sameArticle, type EditBlock } from "./articleRules";

const NUM = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 0 });

/** Giới hạn của hợp đồng (openapi.yaml: CatalogCode, Barcode, CreateProductRequest). */
const CODE_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const BARCODE_RE = /^[0-9A-Za-z-]{4,32}$/;
export const MAX_NAME = 200;
export const MAX_DESCRIPTION = 1000;
export const MAX_OPTION = 50;
export const MAX_VARIANTS = 200;
/**
 * Số đo của mẫu (Task 516, 517): cm, mỗi số 0,1–1.000, tối đa một chữ số thập phân (máy chủ lưu mm số nguyên:
 * catalog.MinMeasureMm, MaxMeasureMm).
 */
export const MIN_MEASURE_MM = 1;
export const MAX_MEASURE_MM = 10000;
export const MEASURE_KEYS = ["length", "width", "height"] as const;
export type MeasureKey = (typeof MEASURE_KEYS)[number];
export const TEXT_BAD_MEASURE = "Số cm từ 0,1 đến 1.000, tối đa một số lẻ";

export interface VariantRow {
  /** Mã biến thể khi đã lưu; dòng mới: "new:<màu>|<size>". */
  key: string;
  id?: string;
  version?: number;
  color: string;
  size: string;
  sku: string;
  /** SKU còn là gợi ý (chưa sửa tay): đổi mã mẫu thì đổi theo. Chỉ dòng mới. */
  skuAuto: boolean;
  barcode: string;
  /** Chữ trong ô giá ("350.000"). */
  price: string;
  costPrice: string;
  status: VariantStatus;
}

export interface Draft {
  /** Version của mẫu lúc mở bản nháp (0 khi tạo mới); mọi bước lưu gửi version này. */
  version: number;
  code: string;
  name: string;
  description: string;
  status: ProductStatus;
  /** "" là chưa chọn. */
  categoryId: string;
  tagIds: string[];
  attributeValueIds: string[];
  colors: string[];
  sizes: string[];
  rows: VariantRow[];
  /** Bài viết dạng khối (Task 326). */
  article: EditBlock[];
  /** Chữ trong ba ô số đo Dài, Rộng, Cao (cm; "" là chưa nhập; Task 516). */
  measure: Record<MeasureKey, string>;
  /**
   * Khóa các dòng nhân viên đã kéo, bấm ↑ ↓ (Task 513). Có giá trị: thứ tự do người đặt, dòng sinh thêm vào cuối;
   * không có: dòng mới xếp theo màu × size sau các dòng đã lưu.
   */
  moved?: readonly string[];
}

export type OptionKind = "color" | "size";

const norm = (s: string) => s.trim().toLocaleLowerCase("vi");
/** Số ký tự như máy chủ đếm (điểm mã Unicode). */
export const charCount = (s: string) => Array.from(s).length;

/** Bỏ dấu tiếng Việt, viết hoa, chỉ giữ chữ, số. */
function fold(s: string): string {
  return s
    .normalize("NFD")
    .replace(/\p{M}/gu, "")
    .replace(/đ/g, "d")
    .replace(/Đ/g, "D")
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, "");
}

/** SKU gợi ý cho dòng mới: "<MÃ MẪU>-<MÀU>-<SIZE>" bỏ dấu, viết hoa, tối đa 64 ký tự. */
export function suggestSku(code: string, color: string, size: string): string {
  const head = code
    .normalize("NFD")
    .replace(/\p{M}/gu, "")
    .replace(/[đĐ]/g, "D")
    .toUpperCase()
    .replace(/[^A-Z0-9._-]/g, "")
    .replace(/^[._-]+/, "");
  return [head, fold(color), fold(size)]
    .filter((p) => p !== "")
    .join("-")
    .slice(0, 64);
}

/** Chữ của ô giá cho số nguyên đồng ("350.000"). */
export function moneyInput(v: number | undefined): string {
  return v === undefined ? "" : NUM.format(v);
}

/** Đọc ô giá: "350.000", "350000", "350 000", có thể kèm "₫". Sai dạng, số lẻ, âm, quá lớn → null. */
export function parseMoney(s: string): number | null {
  const t = s.replace(/₫/g, "").trim().replace(/\s+/g, " ");
  let digits: string;
  if (/^\d+$/.test(t)) digits = t;
  else if (/^\d{1,3}([. ]\d{3})+$/.test(t)) digits = t.replace(/[. ]/g, "");
  else return null;
  const n = Number(digits);
  return Number.isSafeInteger(n) ? n : null;
}

const newKey = (color: string, size: string) => `new:${color}|${size}`;

function sortRows(rows: VariantRow[], colors: readonly string[], sizes: readonly string[]): VariantRow[] {
  const ci = (c: string) => colors.findIndex((x) => norm(x) === norm(c));
  const si = (s: string) => sizes.findIndex((x) => norm(x) === norm(s));
  return [...rows].sort((a, b) => ci(a.color) - ci(b.color) || si(a.size) - si(b.size));
}

function savedRow(v: Variant): VariantRow {
  return {
    key: v.id,
    id: v.id,
    version: v.version,
    color: v.color,
    size: v.size,
    sku: v.sku,
    skuAuto: false,
    barcode: v.barcode,
    price: moneyInput(v.price),
    costPrice: moneyInput(v.costPrice),
    status: v.status,
  };
}

export function emptyDraft(): Draft {
  return {
    version: 0,
    code: "",
    name: "",
    description: "",
    status: "draft",
    categoryId: "",
    tagIds: [],
    attributeValueIds: [],
    colors: [],
    sizes: [],
    rows: [],
    article: [],
    measure: measureText(undefined),
  };
}

/** Ô số đo từ measureCm của máy chủ (số vắng mặt → ô trống; số lẻ viết dấu phẩy: 72,5). */
function measureText(m: MeasureCm | undefined): Record<MeasureKey, string> {
  const t = (v: number | undefined) => (v === undefined ? "" : String(v).replace(".", ","));
  return { length: t(m?.length), width: t(m?.width), height: t(m?.height) };
}

/**
 * Số cm của một ô số đo ("72,5" hay "72.5" → 72.5): undefined khi trống; null khi sai (ngoài 0,1–1.000 hay hơn một
 * chữ số thập phân). Đếm theo mm số nguyên nên hai cách viết cùng số cho cùng kết quả.
 */
function measureNumber(s: string): number | null | undefined {
  const t = s.trim();
  if (t === "") return undefined;
  const m = /^([0-9]{1,4})(?:[.,]([0-9]))?$/.exec(t);
  if (m === null) return null;
  const mm = Number(m[1]) * 10 + Number(m[2] ?? "0");
  return mm >= MIN_MEASURE_MM && mm <= MAX_MEASURE_MM ? mm / 10 : null;
}

/** measureCm gửi đi (gọi sau validate): chỉ các ô có số; không ô nào → undefined. */
function measureBody(m: Record<MeasureKey, string>): MeasureCm | undefined {
  const out: MeasureCm = {};
  for (const k of MEASURE_KEYS) {
    const n = measureNumber(m[k]);
    if (typeof n === "number") out[k] = n;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

const sameMeasure = (a: Record<MeasureKey, string>, b: Record<MeasureKey, string>) =>
  MEASURE_KEYS.every((k) => measureNumber(a[k]) === measureNumber(b[k]));

/**
 * Bản nháp từ mẫu đã lưu và phân loại của nó. Version là số nhỏ hơn của hai lần đọc: đọc lệch nhau thì lần lưu báo
 * version_conflict chứ không đè thay đổi của người khác.
 */
export function draftFromProduct(p: Product, c: ProductClassification): Draft {
  const colors: string[] = [];
  const sizes: string[] = [];
  for (const v of p.variants) {
    if (!colors.some((x) => norm(x) === norm(v.color))) colors.push(v.color);
    if (!sizes.some((x) => norm(x) === norm(v.size))) sizes.push(v.size);
  }
  return {
    version: Math.min(p.version, c.version),
    code: p.code,
    name: p.name,
    description: p.description,
    status: p.status,
    categoryId: c.categoryId ?? "",
    tagIds: [...c.tagIds],
    attributeValueIds: [...c.attributeValueIds],
    colors,
    sizes,
    rows: p.variants.map(savedRow),
    article: editBlocks(p.article),
    measure: measureText(p.measureCm),
  };
}

/** Thêm màu hay size; thêm dòng mới cho mọi tổ hợp còn thiếu. Rỗng, quá dài hay trùng (không kể hoa thường) → bỏ qua. */
export function addOption(d: Draft, kind: OptionKind, value: string): Draft {
  const v = value.trim();
  const list = kind === "color" ? d.colors : d.sizes;
  if (v === "" || charCount(v) > MAX_OPTION || list.some((x) => norm(x) === norm(v))) return d;
  const colors = kind === "color" ? [...d.colors, v] : d.colors;
  const sizes = kind === "size" ? [...d.sizes, v] : d.sizes;
  const rows = [...d.rows];
  for (const c of colors)
    for (const s of sizes) {
      if (rows.some((r) => norm(r.color) === norm(c) && norm(r.size) === norm(s))) continue;
      rows.push({
        key: newKey(c, s),
        color: c,
        size: s,
        sku: suggestSku(d.code, c, s),
        skuAuto: true,
        barcode: "",
        price: "",
        costPrice: "",
        status: "active",
      });
    }
  return { ...d, colors, sizes, rows: d.moved === undefined ? arrange(rows, colors, sizes) : rows };
}

/** Chưa ai dời: dòng đã lưu giữ thứ tự (của máy chủ), dòng mới xếp theo màu × size phía sau. */
function arrange(rows: VariantRow[], colors: readonly string[], sizes: readonly string[]): VariantRow[] {
  return [
    ...rows.filter((r) => r.id !== undefined),
    ...sortRows(
      rows.filter((r) => r.id === undefined),
      colors,
      sizes,
    ),
  ];
}

/** Dời dòng ở chỗ from tới chỗ to (kéo thả, nút ↑ ↓); to ngoài biên thì về đầu/cuối bảng. */
export function moveRow(d: Draft, from: number, to: number): Draft {
  const row = d.rows[from];
  const at = Math.min(Math.max(to, 0), d.rows.length - 1);
  if (row === undefined || at === from) return d;
  const rows = d.rows.filter((_, i) => i !== from);
  rows.splice(at, 0, row);
  const moved = d.moved ?? [];
  return { ...d, rows, moved: moved.includes(row.key) ? moved : [...moved, row.key] };
}

/** Thân bước order: mã mọi biến thể theo thứ tự bảng (gọi sau các bước thêm: mọi dòng đã có mã). */
export function orderIds(d: Draft): string[] {
  return d.rows.flatMap((r) => (r.id === undefined ? [] : [r.id]));
}

const rowHas = (r: VariantRow, kind: OptionKind, value: string) =>
  norm(kind === "color" ? r.color : r.size) === norm(value);

/** Bỏ được màu/size khi không biến thể đã lưu nào dùng nó (biến thể đã lưu chỉ chuyển Ngừng bán). */
export function canRemoveOption(d: Draft, kind: OptionKind, value: string): boolean {
  return !d.rows.some((r) => r.id !== undefined && rowHas(r, kind, value));
}

export function removeOption(d: Draft, kind: OptionKind, value: string): Draft {
  if (!canRemoveOption(d, kind, value)) return d;
  const drop = (l: string[]) => l.filter((x) => norm(x) !== norm(value));
  return {
    ...d,
    colors: kind === "color" ? drop(d.colors) : d.colors,
    sizes: kind === "size" ? drop(d.sizes) : d.sizes,
    rows: d.rows.filter((r) => !rowHas(r, kind, value)),
  };
}

/** Đổi mã mẫu; SKU gợi ý của dòng mới chưa sửa tay đổi theo. */
export function setCode(d: Draft, code: string): Draft {
  return {
    ...d,
    code,
    rows: d.rows.map((r) => (r.id === undefined && r.skuAuto ? { ...r, sku: suggestSku(code, r.color, r.size) } : r)),
  };
}

export type RowPatch = Partial<Pick<VariantRow, "sku" | "barcode" | "price" | "costPrice" | "status">>;

export function setRow(d: Draft, key: string, patch: RowPatch): Draft {
  return {
    ...d,
    rows: d.rows.map((r) =>
      r.key === key ? { ...r, ...patch, skuAuto: patch.sku === undefined ? r.skuAuto : false } : r,
    ),
  };
}

export interface RowErrors {
  sku?: string;
  barcode?: string;
  price?: string;
  costPrice?: string;
}

export interface Validation {
  ok: boolean;
  fields: { code?: string; name?: string; description?: string; variants?: string; article?: string } & Partial<
    Record<MeasureKey, string>
  >;
  rows: Record<string, RowErrors>;
  /** Khóa khối bài viết → câu lỗi. */
  article: Record<string, string>;
}

export const TEXT_SKU_DUP = "Trùng SKU với dòng khác";
export const TEXT_BAD_PRICE = "Giá không hợp lệ";

/** Kiểm trước khi gửi. "create": có kiểm mã mẫu (mẫu đã lưu không đổi mã). */
export function validate(d: Draft, mode: "create" | "edit"): Validation {
  const fields: Validation["fields"] = {};
  if (mode === "create") {
    if (d.code.trim() === "") fields.code = "Nhập mã mẫu";
    else if (!CODE_RE.test(d.code.trim()))
      fields.code = "Mã mẫu chỉ gồm chữ không dấu, số, dấu . _ - (tối đa 64 ký tự)";
  }
  const nameLen = charCount(d.name.trim());
  if (nameLen === 0) fields.name = "Nhập tên mẫu";
  else if (nameLen > MAX_NAME) fields.name = "Tên mẫu tối đa 200 ký tự";
  if (charCount(d.description) > MAX_DESCRIPTION) fields.description = "Mô tả ngắn tối đa 1.000 ký tự";
  if (d.rows.length > MAX_VARIANTS) fields.variants = "Một mẫu tối đa 200 biến thể";
  for (const k of MEASURE_KEYS) if (measureNumber(d.measure[k]) === null) fields[k] = TEXT_BAD_MEASURE;
  const art = checkArticle(d.article);
  if (art.article !== undefined) fields.article = art.article;

  const skuCount = new Map<string, number>();
  for (const r of d.rows) {
    const k = r.sku.trim().toUpperCase();
    if (k !== "") skuCount.set(k, (skuCount.get(k) ?? 0) + 1);
  }
  const rows: Record<string, RowErrors> = {};
  for (const r of d.rows) {
    const e: RowErrors = {};
    const sku = r.sku.trim();
    if (sku === "") e.sku = "Nhập SKU";
    else if (!CODE_RE.test(sku)) e.sku = "SKU chỉ gồm chữ không dấu, số, dấu . _ -";
    else if ((skuCount.get(sku.toUpperCase()) ?? 0) > 1) e.sku = TEXT_SKU_DUP;
    const bc = r.barcode.trim();
    if (bc === "" && r.id !== undefined) e.barcode = "Barcode đã lưu không để trống được";
    else if (bc !== "" && !BARCODE_RE.test(bc)) e.barcode = "Barcode 4–32 ký tự chữ không dấu, số";
    if (r.price.trim() === "") e.price = "Nhập giá bán";
    else if (parseMoney(r.price) === null) e.price = TEXT_BAD_PRICE;
    if (r.costPrice.trim() !== "" && parseMoney(r.costPrice) === null) e.costPrice = TEXT_BAD_PRICE;
    if (Object.keys(e).length > 0) rows[r.key] = e;
  }
  return {
    ok: Object.keys(fields).length === 0 && Object.keys(rows).length === 0 && Object.keys(art.blocks).length === 0,
    fields,
    rows,
    article: art.blocks,
  };
}

// Gọi sau validate: ô giá đã đọc được.
const money = (s: string) => parseMoney(s) ?? 0;
const optMoney = (s: string) => (s.trim() === "" ? undefined : money(s));

function variantInput(r: VariantRow): Omit<AddVariantRequest, "productVersion"> {
  const bc = r.barcode.trim();
  const cost = optMoney(r.costPrice);
  return {
    sku: r.sku.trim(),
    ...(bc === "" ? {} : { barcode: bc }),
    color: r.color,
    size: r.size,
    price: money(r.price),
    ...(cost === undefined ? {} : { costPrice: cost }),
  };
}

/** Thân createProduct: một yêu cầu kèm mọi biến thể (khóa chống trùng → chỉ một mẫu dù gửi lại). */
export function createBody(d: Draft): CreateProductRequest {
  const out: CreateProductRequest = {
    code: d.code.trim(),
    name: d.name.trim(),
    status: d.status,
    variants: d.rows.map(variantInput),
  };
  if (d.description !== "") out.description = d.description;
  if (d.article.length > 0) out.article = articleBody(d.article);
  const m = measureBody(d.measure);
  if (m !== undefined) out.measureCm = m;
  return out;
}

export interface ClassificationBody {
  categoryId: string | null;
  attributeValueIds: string[];
  tagIds: string[];
}

export type SaveStep =
  | { kind: "product"; body: Omit<UpdateProductRequest, "version"> }
  | { kind: "classification"; body: ClassificationBody }
  | { kind: "add"; key: string; body: Omit<AddVariantRequest, "productVersion"> }
  | { kind: "order" }
  | { kind: "variant"; key: string; id: string; body: UpdateVariantRequest };

const sameSet = (a: readonly string[], b: readonly string[]) =>
  a.length === b.length && [...a].sort().join("\n") === [...b].sort().join("\n");

export function classificationBody(d: Draft): ClassificationBody {
  return {
    categoryId: d.categoryId === "" ? null : d.categoryId,
    attributeValueIds: [...d.attributeValueIds],
    tagIds: [...d.tagIds],
  };
}

/** Phân loại khác bản đã lưu (thứ tự nhãn, giá trị không tính). */
export function classificationChanged(base: Draft, edit: Draft): boolean {
  return (
    base.categoryId !== edit.categoryId ||
    !sameSet(base.tagIds, edit.tagIds) ||
    !sameSet(base.attributeValueIds, edit.attributeValueIds)
  );
}

/**
 * Các bước lưu bản nháp edit so với bản đã lưu base, theo thứ tự: sửa mẫu → phân loại → thêm biến thể → sắp thứ tự
 * biến thể → sửa biến thể. Bốn bước đầu khóa theo version của mẫu (mỗi bước +1); sửa biến thể chỉ kiểm version của
 * biến thể nên đi sau cùng. Sắp thứ tự chỉ khi thứ tự bảng khác thứ tự máy chủ sẽ có sau các bước thêm (dòng đã lưu
 * theo base, dòng thêm nối cuối theo thứ tự gửi).
 * Gọi sau validate.
 */
export function savePlan(base: Draft, edit: Draft): SaveStep[] {
  const steps: SaveStep[] = [];
  const p: Omit<UpdateProductRequest, "version"> = {};
  if (edit.name.trim() !== base.name) p.name = edit.name.trim();
  if (edit.description !== base.description) p.description = edit.description;
  if (edit.status !== base.status) p.status = edit.status;
  if (!sameArticle(base.article, edit.article)) p.article = articleBody(edit.article);
  if (!sameMeasure(base.measure, edit.measure)) p.measureCm = measureBody(edit.measure) ?? null;
  if (Object.keys(p).length > 0) steps.push({ kind: "product", body: p });
  if (classificationChanged(base, edit)) steps.push({ kind: "classification", body: classificationBody(edit) });
  for (const r of edit.rows) if (r.id === undefined) steps.push({ kind: "add", key: r.key, body: variantInput(r) });
  const server = [
    ...base.rows.filter((b) => edit.rows.some((r) => r.key === b.key)).map((b) => b.key),
    ...edit.rows.filter((r) => r.id === undefined).map((r) => r.key),
  ];
  if (server.join("\n") !== edit.rows.map((r) => r.key).join("\n")) steps.push({ kind: "order" });
  for (const r of edit.rows) {
    const b = base.rows.find((x) => x.key === r.key);
    if (r.id === undefined || r.version === undefined || b === undefined) continue;
    const body: UpdateVariantRequest = { version: r.version };
    if (r.sku.trim() !== b.sku) body.sku = r.sku.trim();
    if (r.barcode.trim() !== b.barcode) body.barcode = r.barcode.trim();
    if (money(r.price) !== money(b.price)) body.price = money(r.price);
    const cost = optMoney(r.costPrice);
    if (cost !== optMoney(b.costPrice)) body.costPrice = cost ?? null;
    if (r.status !== b.status) body.status = r.status;
    if (Object.keys(body).length > 1) steps.push({ kind: "variant", key: r.key, id: r.id, body });
  }
  return steps;
}

export interface Pair {
  base: Draft;
  edit: Draft;
}

/** Bước sửa mẫu xong: bản đã lưu nhận tên, mô tả, trạng thái, bài viết, số đo mới; cả hai nhận version mới. */
export function applyProduct({ base, edit }: Pair, p: Product): Pair {
  return {
    base: {
      ...base,
      name: p.name,
      description: p.description,
      status: p.status,
      article: editBlocks(p.article),
      measure: measureText(p.measureCm),
      version: p.version,
    },
    edit: { ...edit, version: p.version },
  };
}

/** Bước phân loại xong. */
export function applyClassification({ base, edit }: Pair, c: ProductClassification): Pair {
  return {
    base: {
      ...base,
      categoryId: c.categoryId ?? "",
      tagIds: [...c.tagIds],
      attributeValueIds: [...c.attributeValueIds],
      version: c.version,
    },
    edit: { ...edit, version: c.version },
  };
}

/**
 * Bước thêm hay sửa biến thể key xong: dòng thành bản đã lưu (SKU chuẩn hóa, barcode tự sinh của máy chủ) ở cả hai
 * bản; version mẫu +1 (máy chủ tăng đúng 1 trong cùng transaction).
 */
export function applyVariant({ base, edit }: Pair, key: string, v: Variant): Pair {
  const row = savedRow(v);
  const put = (d: Draft): Draft => {
    const has = d.rows.some((r) => r.key === key);
    const rows = has ? d.rows.map((r) => (r.key === key ? row : r)) : [...d.rows, row];
    return {
      ...d,
      version: d.version + 1,
      colors: d.colors.some((x) => norm(x) === norm(v.color)) ? d.colors : [...d.colors, v.color],
      sizes: d.sizes.some((x) => norm(x) === norm(v.size)) ? d.sizes : [...d.sizes, v.size],
      rows,
    };
  };
  // Dòng giữ chỗ trong bản nháp; bản đã lưu nhận dòng thêm ở cuối như máy chủ (vị trí = cuối + 1).
  return { base: put(base), edit: put(edit) };
}

/** Bước sắp thứ tự xong: bản đã lưu theo thứ tự máy chủ trả; cả hai nhận version mới; hết đánh dấu dòng đã dời. */
export function applyOrder({ base, edit }: Pair, p: Product): Pair {
  const rows = p.variants.flatMap((v) => {
    const r = base.rows.find((x) => x.id === v.id);
    return r === undefined ? [savedRow(v)] : [r];
  });
  const next: Draft = { ...edit, version: p.version };
  delete next.moved;
  return { base: { ...base, rows, version: p.version }, edit: next };
}
