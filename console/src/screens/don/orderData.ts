// Dữ liệu dùng chung của màn Đơn: khóa truy vấn theo chủ đề realtime order.changed (src/app/queryClient.ts), quyền của
// người xem, nguồn đơn. Truy vấn của một đơn mang meta realtimeEntity để báo hiệu của đơn khác không tải lại nó.
import { useQuery, type QueryClient } from "@tanstack/react-query";
import { getMe, getOrder, getOrderSettings, getReturn, listTrafficSources } from "../../api";
import { unwrap } from "../../app/errors";
import { hasPermission } from "../../app/nav";
import { queryKey, type RealtimeMeta } from "../../app/queryClient";
import { ORDER_TOPIC } from "../len-don/OrderForm";

export { ORDER_TOPIC };

export const orderKey = (id: string) => queryKey(ORDER_TOPIC, "detail", id);
export const timelineKey = (id: string) => queryKey(ORDER_TOPIC, "timeline", id);
export const entityMeta = (id: string): RealtimeMeta => ({ realtimeEntity: id });

/** Quyền của người xem (đọc từ /api/v1/me); máy chủ vẫn là nơi chặn. */
export function usePerms() {
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  const grants = me.data?.grants ?? [];
  return {
    loaded: me.isSuccess,
    userId: me.data?.userId ?? "",
    can: (p: string) => hasPermission(grants, p),
  };
}

/** Một đơn; chỉ tải lại khi báo hiệu order.changed đúng đơn này. */
export function useOrder(id: string) {
  return useQuery({
    queryKey: orderKey(id),
    meta: entityMeta(id),
    queryFn: () => unwrap(getOrder({ path: { id } })),
  });
}

/** Phiếu trả của đơn orderId (tải lại khi đơn đó đổi: nhận, đóng, hủy phiếu đều báo order.changed của đơn). */
export function useReturn(orderId: string, returnId: string) {
  return useQuery({
    queryKey: queryKey(ORDER_TOPIC, "return", returnId),
    meta: entityMeta(orderId),
    queryFn: () => unwrap(getReturn({ path: { returnId } })),
  });
}

export function useSources() {
  const q = useQuery({
    queryKey: queryKey(ORDER_TOPIC, "traffic-sources"),
    queryFn: () => unwrap(listTrafficSources()),
  });
  const items = q.data?.items ?? [];
  return {
    items,
    nameOf: (id: string | undefined) => (id === undefined ? "" : (items.find((s) => s.id === id)?.name ?? "")),
  };
}

export function useOrderSettings() {
  return useQuery({ queryKey: queryKey(ORDER_TOPIC, "settings"), queryFn: () => unwrap(getOrderSettings()) });
}

/**
 * Sau một lệnh ghi thành công: đọc lại đơn và lịch sử. Không đặt thân trả về của lệnh ghi vào bộ nhớ: thân đó (orderOut)
 * thiếu trường của chi tiết (editScope, khách, phiếu trả, tên tỉnh xã) mà nút và thẻ của màn cần.
 */
export function reloadOrder(qc: QueryClient, id: string) {
  void qc.invalidateQueries({ queryKey: orderKey(id) });
  void qc.invalidateQueries({ queryKey: timelineKey(id) });
}
