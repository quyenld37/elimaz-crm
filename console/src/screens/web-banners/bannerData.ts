// Dữ liệu của màn Banner trang chủ: danh sách (chủ đề realtime content.banners, máy chủ báo hiệu mỗi lần lưu; Task
// 453), xem trước danh sách đang sửa (previewBanners, 350 ms sau khi ngừng sửa), kích thước ảnh (getFile), tìm mẫu.
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { getBanners, getFile, listProducts, previewBanners, type HomeBanner, type HomeBannerStatus } from "../../api";
import { useMemo } from "react";
import { unwrap } from "../../app/errors";
import { TOPICS, queryKey } from "../../app/queryClient";
import { useDebounced } from "../inventory/Balances";
import { toRequest } from "./bannerRules";

export { usePerms } from "../don/orderData";

/** Chủ đề realtime của danh sách banner (content bannersSignalTopic). */
export const BANNERS_TOPIC = "content.banners";
export const bannersKey = queryKey(BANNERS_TOPIC, "list");
/** Trễ xem trước sau lần sửa cuối (kế hoạch Task 459). */
export const PREVIEW_DELAY_MS = 350;
const SEARCH_DELAY_MS = 300;
const SEARCH_LIMIT = 20;

export const headers = (key: string) => ({ "Idempotency-Key": key });

export function useBanners() {
  return useQuery({ queryKey: bannersKey, queryFn: () => unwrap(getBanners()) });
}

/**
 * Trạng thái hiện/ẩn của danh sách đang sửa: chỉ gọi khi có thay đổi chưa lưu (chưa sửa thì dùng trạng thái của
 * getBanners). Trả undefined khi chưa có kết quả; lỗi (422 luật sai) trong error.
 */
export function useBannerPreview(banners: readonly HomeBanner[], dirty: boolean) {
  // Trễ theo chuỗi JSON (mảng mới mỗi lần vẽ làm bộ trễ không bao giờ dừng). Chỉ gửi khi bộ trễ đã bắt kịp bản đang
  // sửa (body === json): lúc đó req đúng là bản của khóa truy vấn, không gửi bản cũ ngay lúc vừa sửa.
  const req = useMemo(() => toRequest(banners), [banners]);
  const json = JSON.stringify(req);
  const body = useDebounced(json, PREVIEW_DELAY_MS);
  const q = useQuery({
    queryKey: queryKey(BANNERS_TOPIC, "preview", body),
    enabled: dirty && body === json,
    placeholderData: keepPreviousData,
    queryFn: async () => (await unwrap(previewBanners({ body: { banners: req } }))).statuses,
  });
  const statuses: readonly HomeBannerStatus[] | undefined = dirty ? q.data : undefined;
  return { statuses, error: dirty ? q.error : null };
}

/** Thông tin tệp (kích thước, biến thể) của ảnh đã xử lý xong; enabled = có quyền files.upload và ảnh sẵn sàng. */
export function useFileInfo(fileId: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: queryKey(TOPICS.file, fileId ?? "", "info"),
    enabled: enabled && fileId !== undefined,
    queryFn: () => unwrap(getFile({ path: { id: fileId ?? "" } })),
  });
}

/** Tìm mẫu đang bán theo chữ (và danh mục, gồm con) để ghim hay làm đích; chữ rỗng → không tìm. */
export function useProductSearch(q: string, categoryId: string | undefined, enabled: boolean) {
  const text = useDebounced(q.trim(), SEARCH_DELAY_MS);
  return useQuery({
    queryKey: queryKey(TOPICS.product, "banner-search", text, categoryId ?? ""),
    enabled: enabled && text !== "",
    queryFn: () =>
      unwrap(
        listProducts({
          query: {
            q: text,
            status: ["active"],
            limit: SEARCH_LIMIT,
            ...(categoryId === undefined ? {} : { categoryId }),
          },
        }),
      ),
  });
}
