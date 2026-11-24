// Dữ liệu của màn Chiến dịch giảm giá: khóa truy vấn theo chủ đề realtime promotion.web_campaign (máy chủ báo hiệu mỗi
// lần tạo, lưu, đổi trạng thái, xóa; Task 452), một chiến dịch, tên các đích đang chọn.
import { useQuery, type QueryClient } from "@tanstack/react-query";
import { getWebCampaign, listCampaignTargets, type WebCampaignScope } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { entityMeta } from "../don/orderData";
import { missingTarget, type PickedTarget } from "./campaignRules";

export { usePerms } from "../don/orderData";

/** Chủ đề realtime của chiến dịch web (promotion campaignSignalTopic). */
export const CAMPAIGN_TOPIC = "promotion.web_campaign";
/** Trang lớn nhất API cho (Limit 1–100): dùng khi tìm tên đích. */
const TARGET_PAGE = 100;
/** Trần số trang đọc khi tìm tên đích (tệp task 458, Quyết định 3). */
const TARGET_PAGES_MAX = 20;

export const campaignKey = (id: string) => queryKey(CAMPAIGN_TOPIC, "detail", id);
export const headers = (key: string) => ({ "Idempotency-Key": key });

export const targetKind = (scope: WebCampaignScope): "product" | "category" =>
  scope === "products" ? "product" : "category";

export function useCampaign(id: string) {
  return useQuery({
    queryKey: campaignKey(id),
    meta: entityMeta(id),
    queryFn: () => unwrap(getWebCampaign({ path: { campaignId: id } })),
  });
}

/**
 * Tên các đích ids (giữ thứ tự ids): API của chiến dịch chỉ trả mã, nên đọc danh sách đích theo trang tới khi đủ (tối
 * đa TARGET_PAGES_MAX trang); mã không thấy → missingTarget.
 */
export async function resolveTargets(scope: WebCampaignScope, ids: readonly string[]): Promise<PickedTarget[]> {
  const found = new Map<string, PickedTarget>();
  let cursor: string | undefined;
  for (let i = 0; i < TARGET_PAGES_MAX && ids.some((id) => !found.has(id)); i++) {
    const page = await unwrap(
      listCampaignTargets({
        query: { kind: targetKind(scope), limit: TARGET_PAGE, ...(cursor === undefined ? {} : { cursor }) },
      }),
    );
    for (const t of page.items) if (ids.includes(t.id)) found.set(t.id, t);
    cursor = page.nextCursor;
    if (cursor === undefined) break;
  }
  return ids.map((id) => found.get(id) ?? missingTarget(id));
}

/** Sau một lệnh ghi: đọc lại mọi dữ liệu chiến dịch đang mở. */
export function reloadCampaigns(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: queryKey(CAMPAIGN_TOPIC) });
}
