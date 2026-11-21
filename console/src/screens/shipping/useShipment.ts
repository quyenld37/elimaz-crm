// Dữ liệu của khối Vận chuyển trên đơn (kế hoạch M3 Task 133): vận đơn hiện hành, vận đơn gần nhất của đơn, kết quả so
// phí, tài khoản nhập mã tay của kho, mức nhận/hoàn của khách, cài đặt vận chuyển. Báo hiệu chỉ là "X vừa đổi": truy
// vấn tải lại bằng API. shipping_quote.ready mang entity là đơn; shipment.changed mang entity là vận đơn; gắn, gỡ vận
// đơn báo order.changed của đơn (đọc lại đơn và danh sách vận đơn của đơn).
import { keepPreviousData, useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import {
  getCustomerDeliveryStats,
  getFulfillmentSettings,
  getMe,
  getShipment,
  getShipmentCod,
  listOrderShipments,
  listOrderShippingAccounts,
  listShipmentAddressChanges,
  listShipmentEvents,
  listShippingQuotes,
  type Grant,
  type ShipAccount,
} from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { ORDER_TOPIC, entityMeta } from "../don/orderData";

/** Chủ đề realtime của vận đơn (fulfillment.signalShipmentChanged); entity là vận đơn. */
export const SHIPMENT_TOPIC = "shipment.changed";
/** Chủ đề realtime của kết quả báo phí (fulfillment.signalQuoteReady); entity là đơn. */
export const QUOTE_TOPIC = "shipping_quote.ready";
/** Chủ đề realtime của tài khoản hãng (fulfillment.accountSignalTopic). */
export const ACCOUNT_TOPIC = "fulfillment.carrier_account";

/** Khóa hãng "tự giao" (fulfillment.SelfCarrierKey): CRM tự sinh mã, không dán mã. */
export const SELF_CARRIER = "self";

export const shipmentKey = (id: string) => queryKey(SHIPMENT_TOPIC, "detail", id);

/** Quyền của người xem kèm phạm vi (để kiểm theo kho); cùng khóa truy vấn /me với usePerms của màn Đơn. */
export function useGrants(): { loaded: boolean; grants: readonly Grant[] } {
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  return { loaded: me.isSuccess, grants: me.data?.grants ?? [] };
}

/** Vận đơn id (không có id thì không đọc); chỉ tải lại khi báo hiệu shipment.changed đúng vận đơn này. */
export function useCurrentShipment(id: string | undefined) {
  return useQuery({
    queryKey: shipmentKey(id ?? ""),
    meta: entityMeta(id ?? ""),
    enabled: id !== undefined,
    queryFn: () => unwrap(getShipment({ path: { id: id ?? "" } })),
  });
}

/** Vận đơn mới nhất của đơn (cả đã hủy); đọc lại khi đơn đổi (gắn, gỡ vận đơn báo order.changed của đơn). */
export function useLatestShipment(orderId: string, enabled: boolean) {
  return useQuery({
    queryKey: queryKey(ORDER_TOPIC, "shipments", orderId),
    meta: entityMeta(orderId),
    enabled,
    queryFn: () => unwrap(listOrderShipments({ path: { id: orderId }, query: { limit: 1 } })),
    select: (p) => p.items[0] ?? null,
  });
}

/** Các vận đơn của đơn (mới nhất trước, cả đã hủy) cho thẻ "Vận đơn của đơn này"; đọc lại khi đơn đổi. */
export function useOrderShipments(orderId: string, enabled: boolean) {
  return useQuery({
    queryKey: queryKey(ORDER_TOPIC, "shipments", orderId, "all"),
    meta: entityMeta(orderId),
    enabled,
    queryFn: () => unwrap(listOrderShipments({ path: { id: orderId }, query: { limit: SHIPMENT_HISTORY_LIMIT } })),
  });
}

/** Số vận đơn đọc một lần cho thẻ "Vận đơn của đơn này" (một đơn hiếm khi quá vài lần đổi hãng). */
export const SHIPMENT_HISTORY_LIMIT = 20;
/** Số sự kiện mỗi trang của dòng thời gian vận đơn. */
export const EVENT_PAGE = 50;

/** Dòng thời gian của vận đơn id (mới nhất trước, theo trang); tải lại khi shipment.changed đúng vận đơn này. */
export function useShipmentEvents(id: string) {
  return useInfiniteQuery({
    queryKey: queryKey(SHIPMENT_TOPIC, "events", id),
    meta: entityMeta(id),
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      unwrap(
        listShipmentEvents({
          path: { id },
          query: { limit: EVENT_PAGE, ...(pageParam === "" ? {} : { cursor: pageParam }) },
        }),
      ),
    getNextPageParam: (last) => last.nextCursor,
  });
}

/** Khung COD của vận đơn id (cần cash.cod.read; enabled false thì không đọc); tải lại theo shipment.changed. */
export function useShipmentCod(id: string, enabled: boolean) {
  return useQuery({
    queryKey: queryKey(SHIPMENT_TOPIC, "cod", id),
    meta: entityMeta(id),
    enabled,
    queryFn: () => unwrap(getShipmentCod({ path: { id } })),
  });
}

export const addressChangeKey = (id: string) => queryKey(SHIPMENT_TOPIC, "address-change", id);

/** Yêu cầu đổi địa chỉ đang mở của vận đơn id (null: không có); tải lại theo shipment.changed. */
export function useOpenAddressChange(id: string) {
  return useQuery({
    queryKey: addressChangeKey(id),
    meta: entityMeta(id),
    queryFn: () => unwrap(listShipmentAddressChanges({ path: { id } })),
    select: (p) => p.items[0] ?? null,
  });
}

/** Kết quả một lần so phí của đơn (requestId null: lần gần nhất); đọc lại khi shipping_quote.ready của đơn này. */
export function useQuotes(orderId: string, requestId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: queryKey(QUOTE_TOPIC, orderId, requestId ?? "latest"),
    meta: entityMeta(orderId),
    enabled,
    // Lần so phí mới đang đọc: giữ bảng cũ (isPlaceholderData; nút gửi khóa tới khi có giá mới).
    placeholderData: keepPreviousData,
    queryFn: () =>
      unwrap(listShippingQuotes({ path: { id: orderId }, ...(requestId === null ? {} : { query: { requestId } }) })),
  });
}

/**
 * Tài khoản nhập mã tay (mode manual, kể cả tự giao) gửi được cho đơn orderId (máy chủ lọc active, cùng kho, cùng
 * người bán). So phí bỏ qua tài khoản này (không gọi hãng) nên phải đọc riêng; cần quyền fulfillment.ship ở kho của đơn
 * như gửi hãng (Task 449: vai Bán hàng không có quyền xem kết nối hãng vẫn chọn được), enabled false thì không đọc.
 */
export function useManualAccounts(orderId: string, enabled: boolean) {
  return useQuery({
    queryKey: queryKey(ACCOUNT_TOPIC, "for-order", orderId),
    enabled,
    queryFn: () => unwrap(listOrderShippingAccounts({ path: { id: orderId } })),
    select: (p): ShipAccount[] => p.items.filter((a) => a.mode === "manual"),
  });
}

/** Số đơn nhận, hoàn và mức "khách hay hoàn" (Task 112). */
export function useDeliveryStats(customerId: string | undefined) {
  return useQuery({
    queryKey: queryKey("customer.delivery_stats", customerId ?? ""),
    enabled: customerId !== undefined && customerId !== "",
    queryFn: () => unwrap(getCustomerDeliveryStats({ path: { id: customerId ?? "" } })),
  });
}

/** Cài đặt vận chuyển (cân nặng mặc định mỗi món để gợi ý). */
export function useFulfillmentSettings(enabled: boolean) {
  return useQuery({
    queryKey: queryKey("fulfillment.settings"),
    enabled,
    queryFn: () => unwrap(getFulfillmentSettings()),
  });
}

/** Giờ hiện tại (ms), tự cập nhật đúng lúc `deadline` (ISO) qua đi để màn đổi "còn hạn" sang "quá hạn". */
export function useNowUntil(deadline: string | undefined): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (deadline === undefined) return;
    const wait = Math.max(Date.parse(deadline) - Date.now(), 0);
    if (!Number.isFinite(wait)) return;
    // Hạn đã qua lúc dữ liệu tới (now là giờ lúc dựng khối): đặt lại ngay ở lượt sau.
    const t = setTimeout(
      () => {
        setNow(Date.now());
      },
      Math.min(wait + 50, 2_147_000_000),
    );
    return () => {
      clearTimeout(t);
    };
  }, [deadline]);
  return now;
}
