// Dữ liệu của màn Yêu cầu về đơn hàng: khóa truy vấn theo chủ đề realtime order_request.changed (máy chủ báo hiệu khi
// có yêu cầu mới hay vừa đổi, chỉ mang mã yêu cầu; Task 195). Danh sách tải lại theo mọi báo hiệu của chủ đề; chi tiết
// chỉ tải lại khi báo hiệu đúng yêu cầu đang xem.
import { useQuery, type QueryClient } from "@tanstack/react-query";
import { getOrder, getOrderRequest } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { entityMeta, orderKey } from "../don/orderData";

export { usePerms } from "../don/orderData";

/** Chủ đề realtime của yêu cầu về đơn (order.TopicOrderRequestSignal). */
export const REQUEST_TOPIC = "order_request.changed";
/** Số yêu cầu mỗi lần tải (mockup A: "tải 50 dòng mỗi lần theo con trỏ"). */
export const PAGE = 50;

export const requestKey = (id: string) => queryKey(REQUEST_TOPIC, "detail", id);
export const headers = (key: string) => ({ "Idempotency-Key": key });

export function useRequest(id: string) {
  return useQuery({
    queryKey: requestKey(id),
    meta: entityMeta(id),
    queryFn: () => unwrap(getOrderRequest({ path: { id } })),
  });
}

/** Đơn của yêu cầu (khách, tổng tiền, vận đơn hiện hành); chỉ đọc khi người xem có order.read. Cùng khóa với màn Đơn. */
export function useRequestOrder(orderId: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: orderKey(orderId ?? ""),
    meta: entityMeta(orderId ?? ""),
    enabled: enabled && orderId !== undefined,
    queryFn: () => unwrap(getOrder({ path: { id: orderId ?? "" } })),
  });
}

/** Sau một lệnh ghi (hay 409): đọc lại yêu cầu và danh sách. */
export function reloadRequests(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: queryKey(REQUEST_TOPIC) });
}
