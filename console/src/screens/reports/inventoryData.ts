// Dữ liệu của báo cáo tồn kho (Task 241, M6-18): cặp gọi API của họ inventory cho lần chạy nền, kiểm kết quả lần chạy
// nền (ReportRun.result là đối tượng tự do), danh sách kho, luật tải trang tiếp theo.
import { useQuery } from "@tanstack/react-query";
import {
  createInventoryReportRun,
  getInventoryReportRun,
  getInventoryReportRunFile,
  listWarehouses,
  type InventoryMovementRow,
  type InventoryMovements,
  type InventoryStock,
  type InventoryStockRow,
} from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import type { RunApi } from "./useReport";

/** Số dòng mỗi lần tải (tối đa của API). */
export const STOCK_PAGE = 500;

export const inventoryRunApi: RunApi = {
  create: (key, body) => unwrap(createInventoryReportRun({ body, headers: { "Idempotency-Key": key } })),
  get: (id) => unwrap(getInventoryReportRun({ path: { id } })),
};

export const inventoryRunFile = (id: string) => unwrap(getInventoryReportRunFile({ path: { id } }));

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const isInt = (v: unknown): v is number => typeof v === "number" && Number.isSafeInteger(v);
const isText = (v: unknown): v is string => typeof v === "string";

function isStockRow(v: unknown): v is InventoryStockRow {
  return (
    isRecord(v) &&
    [v.variantId, v.sku, v.productName, v.color, v.size, v.warehouseId, v.warehouseName].every(isText) &&
    [v.onHand, v.held, v.inTransit, v.damaged, v.available].every(isInt)
  );
}

function isMovementRow(v: unknown): v is InventoryMovementRow {
  return (
    isRecord(v) &&
    [v.variantId, v.sku, v.productName, v.color, v.size, v.warehouseId].every(isText) &&
    [v.opening, v.imported, v.returnedIn, v.sold, v.exportedOther, v.adjusted, v.closing, v.damagedClosing].every(isInt)
  );
}

export function parseStock(v: unknown): InventoryStock | null {
  if (!isRecord(v) || v.status !== "ready" || !isText(v.date)) return null;
  if (v.source !== "live" && v.source !== "snapshot") return null;
  if (!Array.isArray(v.rows) || !v.rows.every(isStockRow)) return null;
  return {
    status: "ready",
    date: v.date,
    source: v.source,
    rows: v.rows,
    ...(isText(v.firstSnapshotDay) ? { firstSnapshotDay: v.firstSnapshotDay } : {}),
    ...(isText(v.nextCursor) ? { nextCursor: v.nextCursor } : {}),
  };
}

export function parseMovements(v: unknown): InventoryMovements | null {
  if (!isRecord(v) || v.status !== "ready" || !isRecord(v.period)) return null;
  const { from, to } = v.period;
  if (!isText(from) || !isText(to)) return null;
  if (!Array.isArray(v.rows) || !v.rows.every(isMovementRow)) return null;
  return {
    status: "ready",
    period: { from, to },
    rows: v.rows,
    ...(isText(v.nextCursor) ? { nextCursor: v.nextCursor } : {}),
  };
}

export const warehousesKey = () => queryKey("inventory.warehouse", "report-list");

/** Kho người xem được chọn (máy chủ đã lọc theo quyền). */
export function useReportWarehouses() {
  return useQuery({ queryKey: warehousesKey(), queryFn: () => unwrap(listWarehouses()) });
}
