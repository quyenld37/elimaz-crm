// Dữ liệu của màn Mã giảm giá: khóa truy vấn theo chủ đề realtime promotion.coupon (máy chủ báo hiệu khi mã, đợt mã
// đổi; Q-T71-6), một mã, lượt dùng, đợt mã, và tải toàn bộ mã của một đợt thành tệp.
import { useInfiniteQuery, useQuery, type QueryClient } from "@tanstack/react-query";
import { getCoupon, getCouponBatch, listCouponRedemptions, listCoupons, type CouponRedemptionPage } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { entityMeta } from "../don/orderData";

export { usePerms } from "../don/orderData";

/** Chủ đề realtime của mã giảm giá (promotion.signalTopic). */
export const COUPON_TOPIC = "promotion.coupon";
/** Số mã mỗi trang danh sách (mockup: "50 mã mới nhất"). */
export const PAGE = 50;
/** Trang lớn nhất API cho (Limit 1–100): dùng khi tải cả đợt. */
const DOWNLOAD_PAGE = 100;

export const couponKey = (id: string) => queryKey(COUPON_TOPIC, "detail", id);
export const batchKey = (id: string) => queryKey(COUPON_TOPIC, "batch", id);
export const headers = (key: string) => ({ "Idempotency-Key": key });

export function useCoupon(id: string) {
  return useQuery({
    queryKey: couponKey(id),
    meta: entityMeta(id),
    queryFn: () => unwrap(getCoupon({ path: { couponId: id } })),
  });
}

/** Lượt dùng của mã, mới nhất trước, tải thêm theo con trỏ (đi cùng báo hiệu của chính mã đó). */
export function useRedemptions(id: string) {
  return useInfiniteQuery({
    queryKey: queryKey(COUPON_TOPIC, "redemptions", id),
    meta: entityMeta(id),
    initialPageParam: "",
    getNextPageParam: (last: CouponRedemptionPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(
        listCouponRedemptions({
          path: { couponId: id },
          query: { limit: PAGE, ...(pageParam === "" ? {} : { cursor: pageParam }) },
        }),
      ),
  });
}

export function useBatch(id: string | undefined) {
  return useQuery({
    queryKey: batchKey(id ?? ""),
    enabled: id !== undefined,
    queryFn: () => unwrap(getCouponBatch({ path: { batchId: id ?? "" } })),
  });
}

/** Sau một lệnh ghi: đọc lại mọi dữ liệu mã giảm giá đang mở. */
export function reloadCoupons(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: queryKey(COUPON_TOPIC) });
}

/** Mọi mã của đợt batchId theo trang 100; onProgress(số đã tải) sau mỗi trang. */
export async function batchCodes(batchId: string, onProgress: (n: number) => void): Promise<string[]> {
  const codes: string[] = [];
  let cursor: string | undefined;
  do {
    const page = await unwrap(
      listCoupons({ query: { batchId, limit: DOWNLOAD_PAGE, ...(cursor === undefined ? {} : { cursor }) } }),
    );
    codes.push(...page.items.map((c) => c.code));
    onProgress(codes.length);
    cursor = page.nextCursor;
  } while (cursor !== undefined);
  return codes;
}

/** Đưa tệp chữ cho trình duyệt lưu (không qua máy chủ). */
export function saveTextFile(name: string, text: string, type: string) {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
