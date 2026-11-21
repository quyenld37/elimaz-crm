// Huy hiệu nhận/hoàn của khách (kế hoạch M3 Task 134; mockup docs/mockups/m3/the-khach.html; DH03): "Nhận 3 · Hoàn 2
// (40%)" kèm chip "Có hoàn" (risk watch) hay "Hay hoàn" (risk high) theo getCustomerDeliveryStats (ngưỡng ở cài đặt vận
// chuyển, máy chủ tính). Khách chưa có đơn giao xong: câu riêng. Chỉ gợi ý, không chặn lên đơn hay gửi hãng.
import { useDeliveryStats } from "../shipping/useShipment";
import "../shipping/shipping.css";

const percent = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 2 });

/** Huy hiệu của khách customerId; đang tải hay lỗi thì không hiện gì (thẻ khách vẫn đủ thông tin khác). */
export function DeliveryStatsBadge({ customerId }: { customerId: string }) {
  const q = useDeliveryStats(customerId);
  const st = q.data;
  if (st === undefined) return null;
  if (st.delivered + st.returned === 0) {
    return (
      <span className="risk muted">
        <span>Chưa có đơn giao xong</span>
      </span>
    );
  }
  return (
    <span className="risk" aria-label="Nhận, hoàn của khách">
      <span>{`Nhận ${String(st.delivered)} · Hoàn ${String(st.returned)} (${percent.format(st.returnRateBp / 100)}%)`}</span>
      {st.risk === "high" && <span className="chip chip-err">Hay hoàn</span>}
      {st.risk === "watch" && <span className="chip">Có hoàn</span>}
    </span>
  );
}
