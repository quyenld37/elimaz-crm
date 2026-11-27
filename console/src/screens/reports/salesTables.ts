// Luật thuần của ba màn Báo cáo đơn, Hãng vận chuyển, Bán chạy (Task 240, M6-17): kiểm dạng kết quả của lần chạy nền
// (ReportRun.result là đối tượng tự do, sai dạng → null, màn nói "kết quả không đọc được" thay vì vẽ số sai), chiều
// nhóm, bộ lọc nhiều lựa chọn trên địa chỉ (giá trị lạ bị bỏ, không gửi lên máy chủ). Màn không tính số nào: tỷ lệ,
// trung bình, tổng do máy chủ trả (làm tròn xuống theo hợp đồng).
import type {
  ReportDimension,
  SalesBestSellerRow,
  SalesBestSellersReport,
  SalesCarrierRow,
  SalesCarriersReport,
  SalesOrdersReport,
  SalesOrdersRow,
} from "../../api";
import { carrierName, SELF_CARRIER } from "../warehouse/warehouseRules";

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const isInt = (v: unknown): v is number => typeof v === "number" && Number.isSafeInteger(v);
const isCount = (v: unknown): v is number => isInt(v) && v >= 0;
const orNull = (check: (v: unknown) => boolean) => (v: unknown) => v === null || check(v);
const isCountOrNull = orNull(isCount);
const isIntOrNull = orNull(isInt);
const isText = (v: unknown): v is string => typeof v === "string";
const allOk = (r: Record<string, unknown>, fields: readonly string[], check: (v: unknown) => boolean) =>
  fields.every((f) => check(r[f]));

function isOrdersRow(v: unknown): v is SalesOrdersRow {
  return (
    isRecord(v) &&
    isText(v.key) &&
    isText(v.label) &&
    allOk(v, ["orders", "delivered", "returned"], isCount) &&
    allOk(v, ["placed", "active", "cancelled", "successRateBp"], isCountOrNull) &&
    allOk(v, ["goodsAmount", "shippingFeeCharged"], isInt) &&
    isIntOrNull(v.avgGoodsAmount)
  );
}

const hasPeriod = (v: Record<string, unknown>) => isRecord(v.period) && isText(v.period.from) && isText(v.period.to);

export function isOrdersReport(v: unknown): v is SalesOrdersReport {
  if (!isRecord(v) || v.status !== "ready" || !hasPeriod(v)) return false;
  if (v.basis !== "placed" && v.basis !== "completed") return false;
  if (!isText(v.groupBy) || !GROUPS.some((g) => g.value === v.groupBy)) return false;
  return Array.isArray(v.rows) && v.rows.every(isOrdersRow) && isOrdersRow(v.totals);
}

function isCarrierRow(v: unknown): v is SalesCarrierRow {
  return (
    isRecord(v) &&
    isText(v.carrierKey) &&
    (v.accountName === undefined || isText(v.accountName)) &&
    allOk(v, ["created", "cancelled", "inProgress", "returning", "deliveryFailed", "delivered", "returned"], isCount) &&
    isCountOrNull(v.successRateBp) &&
    allOk(v, ["feeTotal", "codTotal"], isInt) &&
    isIntOrNull(v.feeAvg)
  );
}

export function isCarriersReport(v: unknown): v is SalesCarriersReport {
  return (
    isRecord(v) &&
    v.status === "ready" &&
    hasPeriod(v) &&
    Array.isArray(v.rows) &&
    v.rows.every(isCarrierRow) &&
    isCarrierRow(v.totals)
  );
}

function isBestRow(v: unknown): v is SalesBestSellerRow {
  return (
    isRecord(v) &&
    isText(v.productId) &&
    isText(v.name) &&
    allOk(v, ["quantity", "orders", "returnedQuantity"], isCount) &&
    isInt(v.goodsAmount)
  );
}

export function isBestSellers(v: unknown): v is SalesBestSellersReport {
  return (
    isRecord(v) &&
    v.status === "ready" &&
    hasPeriod(v) &&
    (v.level === "sku" || v.level === "product") &&
    (v.basis === "placed" || v.basis === "delivered") &&
    isCount(v.totalRows) &&
    Array.isArray(v.rows) &&
    v.rows.every(isBestRow)
  );
}

// ---------------------------------------------------------------- Chiều nhóm của báo cáo đơn

export const GROUPS: readonly { value: ReportDimension; label: string }[] = [
  { value: "care_by", label: "Nhân viên phụ trách" },
  { value: "status", label: "Trạng thái" },
  { value: "source", label: "Nguồn" },
  { value: "created_by", label: "Người tạo" },
  { value: "channel", label: "Kênh" },
  { value: "page", label: "Page" },
  { value: "carrier", label: "Hãng" },
  { value: "day", label: "Ngày" },
];
export const DEFAULT_GROUP: ReportDimension = "care_by";

/** Chiều nhóm trên địa chỉ; thiếu hay lạ → Nhân viên phụ trách. */
export function parseGroup(v: string | null): ReportDimension {
  return GROUPS.find((g) => g.value === v)?.value ?? DEFAULT_GROUP;
}

export const groupLabel = (d: ReportDimension): string => GROUPS.find((g) => g.value === d)?.label ?? "";

// ---------------------------------------------------------------- Bộ lọc nhiều lựa chọn

export type FilterField = "channel" | "sourceId" | "careBy" | "createdBy" | "pageId" | "carrierKey";
export type OrderFilters = Record<FilterField, string[]>;

/** Mỗi bộ lọc: tên trên địa chỉ, nhãn trên chip, dạng giá trị hợp lệ (cùng luật với openapi) và số lựa chọn tối đa. */
export const FILTER_FIELDS: readonly { field: FilterField; param: string; label: string; pattern: RegExp }[] = [
  { field: "channel", param: "kenh", label: "Kênh", pattern: /^[a-z][a-z0-9_]{0,31}$/ },
  { field: "sourceId", param: "nguon", label: "Nguồn", pattern: /^[A-Za-z0-9_-]{1,64}$/ },
  { field: "careBy", param: "nv", label: "Nhân viên phụ trách", pattern: /^[A-Za-z0-9_-]{1,64}$/ },
  { field: "createdBy", param: "tao", label: "Người tạo", pattern: /^[A-Za-z0-9_-]{1,64}$/ },
  { field: "pageId", param: "page", label: "Page", pattern: /^[A-Za-z0-9_-]{1,64}$/ },
  { field: "carrierKey", param: "hang", label: "Hãng", pattern: /^[a-z][a-z0-9_]{0,31}$/ },
];
export const MAX_PICKED = 20;

export const noFilters = (): OrderFilters => ({
  channel: [],
  sourceId: [],
  careBy: [],
  createdBy: [],
  pageId: [],
  carrierKey: [],
});

export function parseFilters(sp: URLSearchParams): OrderFilters {
  const out = noFilters();
  for (const f of FILTER_FIELDS) {
    const seen = new Set(sp.getAll(f.param).filter((v) => f.pattern.test(v)));
    out[f.field] = [...seen].slice(0, MAX_PICKED);
  }
  return out;
}

export function writeFilters(sp: URLSearchParams, filters: OrderFilters): void {
  for (const f of FILTER_FIELDS) {
    sp.delete(f.param);
    for (const v of filters[f.field]) sp.append(f.param, v);
  }
}

/** Phần tham số của lần gọi API: chỉ bộ lọc có chọn. */
export function filterQuery(filters: OrderFilters): Partial<OrderFilters> {
  const q: Partial<OrderFilters> = {};
  for (const f of FILTER_FIELDS) if (filters[f.field].length > 0) q[f.field] = filters[f.field];
  return q;
}

export const hasFilters = (filters: OrderFilters): boolean => FILTER_FIELDS.some((f) => filters[f.field].length > 0);

/** Tên hãng trên màn báo cáo: hãng tự giao là "Tự giao" (mockup van-chuyen.html). */
export const carrierLabel = (key: string): string => (key === SELF_CARRIER ? "Tự giao" : carrierName(key));
