// Dữ liệu của màn Bộ phối: danh sách (chủ đề realtime content.looks, máy chủ báo hiệu mỗi lần lưu; Task 454), xem trước
// danh sách đang sửa (previewLooks, 350 ms sau khi ngừng sửa, gửi cả danh sách; máy chủ bỏ bộ chưa đủ kèm lý do, Task
// 488), màu của món (colors của mẫu trong getLooks/previewLooks), chi tiết mẫu (getProduct: ảnh và màu mặc định của món
// vừa chọn, catalog.read).
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
  type LookProductRef,
  type Product,
  type WebImageVariant,
} from "../../api";
import { ERROR_MESSAGES, unwrap } from "../../app/errors";
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
 * Hiện/ẩn, tiêu đề tự động, đường dẫn, mẫu và ảnh của danh sách đang sửa: chỉ gọi khi có thay đổi chưa lưu. Gửi mọi bộ
 * đã chọn ảnh (máy chủ bỏ bộ đang điền dở, trả lý do ở skipped — Task 488; lỗi của cả danh sách như quá 6 bộ trang chủ
 * vẫn 422). Bộ chưa chọn ảnh không gửi: hợp đồng đòi photoFileId là uuid, chuỗi rỗng làm cả yêu cầu 400 — lý do của bộ
 * đó do màn tự nêu (lookProblem). Trả undefined khi chưa có kết quả.
 */
export function useLookPreview(looks: readonly Look[], dirty: boolean) {
  // Trễ theo chuỗi JSON; chỉ gửi khi bộ trễ đã bắt kịp bản đang sửa (giống useBannerPreview).
  const req = useMemo(() => toRequest(looks.filter((l) => l.photoFileId !== "")), [looks]);
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

/**
 * Màu bán được theo mẫu từ danh sách (getLooks, previewLooks: colors của LookProductRef, Task 488), không cần quyền
 * catalog.read; danh sách sau đè danh sách trước.
 */
export function refColors(lists: readonly (readonly LookProductRef[])[]): ReadonlyMap<string, readonly string[]> {
  const out = new Map<string, readonly string[]>();
  for (const ps of lists)
    for (const p of ps)
      if (p.colors !== undefined)
        out.set(
          p.id,
          p.colors.map((c) => c.name),
        );
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

/**
 * Câu "chưa đủ để lên trang chủ" của bộ l khi đang sửa: bộ chưa chọn ảnh (không gửi xem trước) → lý do màn tự tính;
 * bộ máy chủ bỏ qua → câu của mã lỗi máy chủ trả; còn lại → undefined.
 */
export function skipText(l: Look, preview: LookPreview | undefined): string | undefined {
  if (l.photoFileId === "") return lookProblem(l) ?? undefined;
  const k = preview?.skipped.find((x) => x.id === l.id);
  return k === undefined ? undefined : (ERROR_MESSAGES[k.reason] ?? k.reason);
}
