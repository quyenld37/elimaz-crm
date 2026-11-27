// Dữ liệu dùng chung của màn Đối soát COD: khóa truy vấn theo chủ đề realtime cod_statement.changed (báo hiệu khi nhập,
// đọc tệp, khớp, chốt bảng kê; src/app/queryClient.ts), header khóa chống trùng, quyền của người xem.
import type { QueryClient } from "@tanstack/react-query";
import { queryKey, type RealtimeMeta } from "../../app/queryClient";
import { COD_TOPIC } from "./codRules";

export { usePerms } from "../don/orderData";

export const listKey = () => queryKey(COD_TOPIC, "list");
export const detailKey = (id: string, group: string) => queryKey(COD_TOPIC, "detail", id, group);
export const discrepanciesKey = () => queryKey(COD_TOPIC, "discrepancies");
export const overdueKey = () => queryKey(COD_TOPIC, "overdue");
/** Truy vấn của một bảng kê chỉ tải lại khi báo hiệu đúng bảng kê đó. */
export const entityMeta = (id: string): RealtimeMeta => ({ realtimeEntity: id });

export const headers = (key: string) => ({ "Idempotency-Key": key });

/** Đọc lại mọi dữ liệu COD đang mở (sau khi nhập: dòng mới vào danh sách). */
export function reloadCod(qc: QueryClient): void {
  void qc.invalidateQueries({ queryKey: [COD_TOPIC] });
}
