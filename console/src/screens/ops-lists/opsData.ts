// Dữ liệu dùng chung của các danh sách vận hành: khóa truy vấn theo chủ đề realtime (callback_task.changed,
// shipment.changed; src/app/queryClient.ts), danh sách theo trang bằng con trỏ, số đếm của cả shop, quyền người xem.
import { useInfiniteQuery, useQuery, useQueryClient, type QueryClient, type QueryKey } from "@tanstack/react-query";
import { useEffect } from "react";
import { listShipmentsForReview } from "../../api";
import { unwrap } from "../../app/errors";
import { TOPICS, queryKey, subscribeChanged } from "../../app/queryClient";
import { CALLBACK_TOPIC, PAGE, SHIPMENT_TOPIC } from "./opsRules";

export { usePerms } from "../don/orderData";

export const callbacksKey = (status: string, mine: boolean) => queryKey(CALLBACK_TOPIC, "list", status, mine);
export const stuckKey = () => queryKey(SHIPMENT_TOPIC, "stuck");
export const reviewKey = () => queryKey(SHIPMENT_TOPIC, "review");
export const countsKey = () => queryKey(SHIPMENT_TOPIC, "counts");
/** Hàng giữ quá hạn đổi cùng sổ tồn nên dùng chủ đề số dư. */
export const agedKey = (state: string) => queryKey(TOPICS.balance, "aged", state);

export const headers = (key: string) => ({ "Idempotency-Key": key });

interface Page {
  items: unknown[];
  nextCursor?: string;
}

/** Danh sách theo trang của key; fetch nhận limit và con trỏ (trang đầu: không có). Dòng: list.data.pages.flatMap(p => p.items). */
export function useOpsPaged<P extends Page>(
  key: QueryKey,
  fetch: (q: { limit: number; cursor?: string }) => Promise<P>,
) {
  const list = useInfiniteQuery({
    queryKey: key,
    initialPageParam: "",
    getNextPageParam: (last: P) =>
      last.nextCursor === undefined || last.nextCursor === "" ? undefined : last.nextCursor,
    queryFn: ({ pageParam }) => fetch(pageParam === "" ? { limit: PAGE } : { limit: PAGE, cursor: pageParam }),
  });
  return { list };
}

/**
 * Số đếm của cả shop (Cần xem, Đơn kẹt, Cần gọi lại mở và quá hạn, Không rõ). Đọc kèm một việc để lấy `counts`; tải lại
 * theo shipment.changed (cùng chủ đề của khóa) và callback_task.changed (nghe riêng vì khóa mang một chủ đề).
 */
export function useOpsCounts() {
  const qc = useQueryClient();
  useEffect(
    () =>
      subscribeChanged(qc, (e) => {
        if (e.topic === CALLBACK_TOPIC) void qc.invalidateQueries({ queryKey: countsKey(), refetchType: "active" });
      }),
    [qc],
  );
  return useQuery({
    queryKey: countsKey(),
    queryFn: async () => (await unwrap(listShipmentsForReview({ query: { limit: 1 } }))).counts,
  });
}

/** Sau một lệnh ghi (hay 409): đọc lại mọi danh sách vận hành và số đếm. */
export function reloadOps(qc: QueryClient): void {
  void qc.invalidateQueries({ queryKey: [CALLBACK_TOPIC] });
  void qc.invalidateQueries({ queryKey: [SHIPMENT_TOPIC] });
}
