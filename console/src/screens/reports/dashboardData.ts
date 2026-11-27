// Đọc kết quả của lần chạy nền (Task 239, M6-16): ReportRun.result là đối tượng tự do (cùng hình dạng với GET của báo
// cáo, status ready), nên phải kiểm trước khi hiện như xem tại chỗ. Kiểm các trường màn Tổng quan dùng; sai dạng →
// null (màn nói "kết quả không đọc được", không vẽ số sai).
import type { ReportAmounts, SalesDashboardReport } from "../../api";

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const isCount = (v: unknown): v is number => typeof v === "number" && Number.isSafeInteger(v) && v >= 0;

function isAmounts(v: unknown): v is ReportAmounts {
  return isRecord(v) && isCount(v.orders) && isCount(v.goodsAmount) && isCount(v.shippingFeeCharged);
}

export function isDashboard(v: unknown): v is SalesDashboardReport {
  if (!isRecord(v) || v.status !== "ready") return false;
  if (!isRecord(v.period) || typeof v.period.from !== "string" || typeof v.period.to !== "string") return false;
  if (typeof v.generatedAt !== "string") return false;
  for (const k of ["placed", "active", "cancelled", "delivered", "returned"]) if (!isAmounts(v[k])) return false;
  if (v.avgGoodsAmount !== null && !isCount(v.avgGoodsAmount)) return false;
  const cr = v.customerReturns;
  if (!isRecord(cr) || !isCount(cr.items) || !isCount(cr.refundAmount)) return false;
  const p = v.pending;
  if (!isRecord(p) || !isCount(p.confirm) || !isCount(p.ship) || !isCount(p.cancelRequested)) return false;
  if (!Array.isArray(v.byChannel) || !Array.isArray(v.byDay)) return false;
  const rowsOk = (rows: unknown[], key: string, fields: string[]) =>
    rows.every((r) => isRecord(r) && typeof r[key] === "string" && fields.every((f) => isAmounts(r[f])));
  return rowsOk(v.byChannel, "channel", ["placed", "active"]) && rowsOk(v.byDay, "day", ["active", "delivered"]);
}

/** Kết quả lần chạy nền của Tổng quan → báo cáo; sai dạng → null. */
export function parseDashboard(v: unknown): SalesDashboardReport | null {
  return isDashboard(v) ? v : null;
}
