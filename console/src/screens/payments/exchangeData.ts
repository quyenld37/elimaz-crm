// Phiếu đổi đã tạo ra đơn đổi o (o.replacedReturnId; Task 116, 138): dùng ở khối Tiền đã thu (kho nhận thiếu) và khối
// Vận chuyển (chỉ gửi hãng sau khi có phiếu cấn trừ). Cùng khóa truy vấn với phiếu trả của chi tiết đơn.
import { useQuery } from "@tanstack/react-query";
import { getReturn, type Order } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { ORDER_TOPIC } from "../don/orderData";

/** Phiếu đổi của đơn đổi o; đơn không phải đơn đổi → rid undefined, không đọc. */
export function useReplacedReturn(o: Pick<Order, "orderType" | "replacedReturnId">) {
  const rid = o.orderType === "exchange" ? o.replacedReturnId : undefined;
  const ret = useQuery({
    queryKey: queryKey(ORDER_TOPIC, "return", rid ?? ""),
    enabled: rid !== undefined,
    queryFn: () => unwrap(getReturn({ path: { returnId: rid ?? "" } })),
  });
  return { rid, ret };
}
