// Dữ liệu của màn Đợt flash sale: khóa truy vấn theo chủ đề realtime flash_sale.changed (consumer webcache phát khi lên
// lịch, thêm suất, mở, đóng; docs/views.md, Task 200, 204), một đợt, số trực tiếp. Lượt mua không phát sự kiện riêng
// (giữ đường mua nhẹ) nên đợt đang mở tự tải lại mỗi 5 s, trạng thái khác mỗi 20 s.
import { useQuery, type QueryClient } from "@tanstack/react-query";
import { flashSaleLive, getFlashSale } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { entityMeta } from "../don/orderData";
import { refreshMs } from "./flashRules";

export { usePerms } from "../don/orderData";

/** Chủ đề realtime của đợt flash sale (báo hiệu cho người có promotion.flash.read). */
export const FLASH_TOPIC = "flash_sale.changed";

export const PERM_MANAGE = "promotion.flash.manage";

export const saleKey = (id: string) => queryKey(FLASH_TOPIC, "detail", id);
export const liveKey = (id: string) => queryKey(FLASH_TOPIC, "live", id);
export const headers = (key: string) => ({ "Idempotency-Key": key });

/** Một đợt cùng các món; tự tải lại 20 s (trạng thái đổi theo giờ: mở, đóng), hoặc 5 s khi đang mở. */
export function useFlashSale(id: string) {
  return useQuery({
    queryKey: saleKey(id),
    meta: entityMeta(id),
    refetchInterval: (q) => refreshMs(q.state.data?.sale.status),
    queryFn: () => unwrap(getFlashSale({ path: { flashSaleId: id } })),
  });
}

/** Số liệu trực tiếp từng món; đợt nháp chưa có suất nên không gọi. */
export function useFlashLive(id: string, enabled: boolean, status: Parameters<typeof refreshMs>[0]) {
  return useQuery({
    queryKey: liveKey(id),
    meta: entityMeta(id),
    enabled,
    refetchInterval: refreshMs(status),
    queryFn: () => unwrap(flashSaleLive({ path: { flashSaleId: id } })),
  });
}

/** Sau một lệnh ghi: đọc lại mọi dữ liệu flash sale đang mở (danh sách, đợt, số trực tiếp). */
export function reloadFlash(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: queryKey(FLASH_TOPIC) });
}
