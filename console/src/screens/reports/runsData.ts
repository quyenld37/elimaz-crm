// Dữ liệu của trang Tệp xuất (Task 241, M6-18; mockup tep-xuat.html): ba họ lần chạy theo quyền, cách gọi danh sách và
// tệp của từng họ, chữ trạng thái, tên báo cáo, đường tới màn báo cáo để Xem.
import { useInfiniteQuery } from "@tanstack/react-query";
import {
  getCustomerExportFile,
  getInventoryReportRunFile,
  getSalesReportRunFile,
  listCustomerExports,
  listInventoryReportRuns,
  listSalesReportRuns,
  type ReportFileLink,
  type ReportKey,
  type ReportRun,
  type ReportRunPage,
} from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { count, dayLabel, periodText } from "./format";
import { TOPIC_RUN_FINISHED } from "./useReport";

export type Family = ReportRun["family"];

export const RUNS_PAGE = 50;
/** Lưới an toàn khi mất báo hiệu (04 mục 6): tải lại mỗi 20 giây. */
export const RUNS_REFRESH_MS = 20_000;

export interface FamilyDef {
  family: Family;
  title: string;
  /** Quyền (phạm vi bất kỳ) cần để thấy nhóm. */
  permission: string;
  /** Tiêu đề cột thứ hai. */
  scopeHeader: string;
  list: (cursor: string) => Promise<ReportRunPage>;
  file: (id: string) => Promise<ReportFileLink>;
}

const query = (cursor: string) => ({ limit: RUNS_PAGE, ...(cursor === "" ? {} : { cursor }) });

const salesDef: FamilyDef = {
  family: "sales",
  title: "Báo cáo bán hàng",
  permission: "report.sales.read",
  scopeHeader: "Khoảng ngày",
  list: (c) => unwrap(listSalesReportRuns({ query: query(c) })),
  file: (id) => unwrap(getSalesReportRunFile({ path: { id } })),
};

const inventoryDef: FamilyDef = {
  family: "inventory",
  title: "Báo cáo tồn kho",
  permission: "report.inventory.read",
  scopeHeader: "Ngày / kỳ",
  list: (c) => unwrap(listInventoryReportRuns({ query: query(c) })),
  file: (id) => unwrap(getInventoryReportRunFile({ path: { id } })),
};

const customersDef: FamilyDef = {
  family: "customers",
  title: "Danh sách khách",
  permission: "customer.export",
  scopeHeader: "Bộ lọc",
  list: (c) => unwrap(listCustomerExports({ query: query(c) })),
  file: (id) => unwrap(getCustomerExportFile({ path: { id } })),
};

export const FAMILIES: readonly [FamilyDef, FamilyDef, FamilyDef] = [salesDef, inventoryDef, customersDef];

export const runsKey = (family: Family, kind: "list" | "nav" = "list") => queryKey(TOPIC_RUN_FINISHED, kind, family);

/** Danh sách lần chạy của một họ, mới nhất trước, tải thêm bằng con trỏ, tự tải lại mỗi 20 giây. */
export function useRunList(def: FamilyDef, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: runsKey(def.family),
    initialPageParam: "",
    getNextPageParam: (last: ReportRunPage) => last.nextCursor,
    queryFn: ({ pageParam }) => def.list(pageParam),
    enabled,
    refetchInterval: RUNS_REFRESH_MS,
  });
}

export const REPORT_LABEL: Readonly<Record<ReportKey, string>> = {
  dashboard: "Tổng quan",
  orders: "Báo cáo đơn",
  order_drilldown: "Danh sách đơn chi tiết",
  carriers: "Hãng vận chuyển",
  best_sellers: "Bán chạy",
  stock: "Tồn",
  stock_movements: "Xuất nhập tồn",
  products: "Danh sách sản phẩm",
  customers: "Danh sách khách",
};

export const reportName = (r: ReportRun) => `${REPORT_LABEL[r.report]} · ${r.format === "xlsx" ? "Excel" : "bảng"}`;

const FAILED_TEXT: Readonly<Record<NonNullable<ReportRun["reason"]>, string>> = {
  too_heavy: "Lỗi: quá nặng",
  busy: "Lỗi: hệ thống bận, thử lại sau",
  forbidden: "Lỗi: không đủ quyền",
  error: "Lỗi",
};

/** Trạng thái bằng chữ (không chỉ màu). */
export function statusText(r: ReportRun): string {
  switch (r.status) {
    case "queued":
      return "Đang chờ";
    case "running":
      return "Đang chạy";
    case "ready":
      return "Xong";
    case "expired":
      return "Hết hạn";
    case "failed":
      return FAILED_TEXT[r.reason ?? "error"];
  }
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");

/** Khoảng ngày / ngày / bộ lọc của lần chạy, theo họ và báo cáo. `tags`: tên nhãn theo mã (xuất danh sách khách). */
export function scopeText(r: ReportRun, tags: ReadonlyMap<string, string>): string {
  const p = r.params;
  if (r.family === "customers") {
    const parts: string[] = [];
    if (Array.isArray(p.tagIds) && p.tagIds.length > 0) {
      parts.push(`Nhãn: ${p.tagIds.map((id) => tags.get(String(id)) ?? "nhãn đã xóa").join(", ")}`);
    }
    if (p.lifecycle === "customer") parts.push("Đã mua");
    if (p.lifecycle === "lead") parts.push("Tiềm năng");
    const created = rangeText("Tạo", str(p.createdFrom), str(p.createdTo));
    if (created !== "") parts.push(created);
    const last = rangeText("Mua cuối", str(p.lastOrderFrom), str(p.lastOrderTo));
    if (last !== "") parts.push(last);
    if (typeof p.minOrders === "number") parts.push(`Từ ${count(p.minOrders)} đơn`);
    return parts.length === 0 ? "Tất cả khách" : parts.join(" · ");
  }
  if (r.report === "products") {
    const parts = [STATUS_TEXT[str(p.status)] ?? "", str(p.q) === "" ? "" : `SKU ${str(p.q)}…`].filter((x) => x !== "");
    return parts.length === 0 ? "Tất cả mẫu" : parts.join(" · ");
  }
  if (r.report === "stock") return str(p.date) === "" ? "Hôm nay" : dayLabel(str(p.date));
  const [from, to] = [str(p.from), str(p.to)];
  return from === "" || to === "" ? "—" : periodText(from, to);
}

const STATUS_TEXT: Readonly<Record<string, string>> = { draft: "Nháp", active: "Đang bán", discontinued: "Ngừng bán" };

function rangeText(label: string, from: string, to: string): string {
  if (from === "" && to === "") return "";
  if (from !== "" && to !== "") return `${label} ${dayLabel(from)} – ${dayLabel(to)}`;
  return from !== "" ? `${label} từ ${dayLabel(from)}` : `${label} đến ${dayLabel(to)}`;
}

/** Đường tới màn báo cáo của lần chạy dạng bảng (nút Xem): màn tự hiện kết quả của đúng kỳ, như Q8. */
export function viewPath(r: ReportRun): string {
  const p = r.params;
  const period = new URLSearchParams();
  if (str(p.from) !== "" && str(p.to) !== "") {
    period.set("ky", "custom");
    period.set("tu", str(p.from));
    period.set("den", str(p.to));
  }
  const tail = (extra?: Record<string, string>) => {
    const sp = new URLSearchParams({ ...extra });
    period.forEach((v, k) => {
      sp.set(k, v);
    });
    const s = sp.toString();
    return s === "" ? "" : `?${s}`;
  };
  switch (r.report) {
    case "dashboard":
      return `/bao-cao/tong-quan${tail()}`;
    case "orders":
    case "order_drilldown":
      return `/bao-cao/don${tail()}`;
    case "carriers":
      return `/bao-cao/van-chuyen${tail()}`;
    case "best_sellers":
      return `/bao-cao/ban-chay${tail()}`;
    case "stock_movements":
      return `/bao-cao/ton-kho${tail({ tab: "xuat-nhap-ton" })}`;
    default:
      return `/bao-cao/ton-kho${str(p.date) === "" ? "" : `?ngay=${str(p.date)}`}`;
  }
}
