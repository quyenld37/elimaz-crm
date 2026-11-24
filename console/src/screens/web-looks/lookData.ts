// Dữ liệu của màn Bộ phối: danh sách (chủ đề realtime content.looks, máy chủ báo hiệu mỗi lần lưu; Task 454), xem trước
// danh sách đang sửa (previewLooks, 350 ms sau khi ngừng sửa, chỉ gửi bộ đã đủ thông tin), chi tiết mẫu của các món
// (getProduct: màu và ảnh của món vừa chọn, catalog.read).
import { keepPreviousData, useQueries, useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import {
  getLooks,
  getProduct,
  previewLooks,
  type FileVariant,
  type Look,
  type LookPhoto,
  type LookPreview,
  type Product,
  type WebImageVariant,
} from "../../api";
import { unwrap } from "../../app/errors";
import { TOPICS, queryKey } from "../../app/queryClient";
import { useDebounced } from "../inventory/Balances";
import { PREVIEW_DELAY_MS } from "../web-banners/bannerData";
import { lookProblem, toRequest, type PhotoOpt } from "./lookRules";

export { headers, usePerms } from "../web-banners/bannerData";

/** Chủ đề realtime của danh sách bộ phối (content looksSignalTopic). */
export const LOOKS_TOPIC = "content.looks";
export const looksKey = queryKey(LOOKS_TOPIC, "list");

export function useLooks() {
  return useQuery({ queryKey: looksKey, queryFn: () => unwrap(getLooks()) });
}

/**
 * Hiện/ẩn, tiêu đề tự động, đường dẫn, mẫu và ảnh của danh sách đang sửa: chỉ gọi khi có thay đổi chưa lưu và chỉ gửi
 * bộ đã đủ thông tin (bộ đang điền dở làm máy chủ trả 422 cho cả danh sách). Trả undefined khi chưa có kết quả.
 */
export function useLookPreview(looks: readonly Look[], dirty: boolean) {
  // Trễ theo chuỗi JSON; chỉ gửi khi bộ trễ đã bắt kịp bản đang sửa (giống useBannerPreview).
  const req = useMemo(() => toRequest(looks.filter((l) => lookProblem(l) === null)), [looks]);
  const json = JSON.stringify(req);
  const body = useDebounced(json, PREVIEW_DELAY_MS);
  const q = useQuery({
    queryKey: queryKey(LOOKS_TOPIC, "preview", body),
    enabled: dirty && body === json,
    placeholderData: keepPreviousData,
    queryFn: () => unwrap(previewLooks({ body: { looks: req } })),
  });
  const data: LookPreview | undefined = dirty ? q.data : undefined;
  return { data, error: dirty ? q.error : null };
}

export const productKey = (id: string) => queryKey(TOPICS.product, id, "look");
export const fetchProduct = (id: string) => unwrap(getProduct({ path: { id } }));

/** Chi tiết mẫu của các món (màu, ảnh); enabled = có quyền catalog.read. */
export function useProductDetails(ids: readonly string[], enabled: boolean): ReadonlyMap<string, Product> {
  const qs = useQueries({
    queries: ids.map((id) => ({ queryKey: productKey(id), enabled, queryFn: () => fetchProduct(id) })),
  });
  const out = new Map<string, Product>();
  for (const q of qs) if (q.data !== undefined) out.set(q.data.id, q.data);
  return out;
}

type Variant = FileVariant | WebImageVariant;
const smallest = (vs: readonly Variant[]) => [...vs].sort((a, b) => a.width - b.width)[0]?.url;
const largest = (vs: readonly Variant[]) => [...vs].sort((a, b) => b.width - a.width)[0]?.url;

function opt(productId: string, fileId: string, position: number, color: string | undefined, vs: readonly Variant[]) {
  const thumb = smallest(vs);
  const large = largest(vs);
  return {
    productId,
    fileId,
    position,
    ready: vs.length > 0,
    ...(color === undefined ? {} : { color }),
    ...(thumb === undefined ? {} : { thumb }),
    ...(large === undefined ? {} : { large }),
  } satisfies PhotoOpt;
}

/** Ảnh có thể chọn từ ảnh của danh sách (getLooks, previewLooks) và từ chi tiết mẫu (ảnh đã xử lý). */
export function photoOpts(lists: readonly (readonly LookPhoto[])[], details: Iterable<Product>): PhotoOpt[] {
  const out: PhotoOpt[] = [];
  const add = (p: PhotoOpt) => {
    if (!out.some((x) => x.fileId === p.fileId && x.productId === p.productId)) out.push(p);
  };
  for (const ps of lists) for (const p of ps) add(opt(p.productId, p.fileId, p.position, p.color, p.variants));
  for (const d of details)
    for (const im of d.images)
      if (im.status === "ready") add(opt(d.id, im.fileId, im.position, im.color, im.variants ?? []));
  return out;
}

/** Màu của mẫu theo thứ tự biến thể, không trùng. */
export const colorsOf = (p: Product | undefined): string[] => [
  ...new Set((p?.variants ?? []).map((v) => v.color).filter((c) => c !== "")),
];

/** Màu mặc định khi chọn món: màu đầu có ảnh, không thì màu đầu, không có màu → rỗng (website tự chọn). */
export function defaultColor(p: Product): string {
  const colors = colorsOf(p);
  return colors.find((c) => p.images.some((im) => im.color === c)) ?? colors[0] ?? "";
}
