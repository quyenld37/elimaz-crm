// Dữ liệu của màn Chiến dịch giảm giá: khóa truy vấn theo chủ đề realtime promotion.web_campaign (máy chủ báo hiệu mỗi
// lần tạo, lưu, đổi trạng thái, xóa; Task 452), một chiến dịch, tên các đích đang chọn, trần phần trăm.
import { useQuery, type QueryClient } from "@tanstack/react-query";
import { getWebCampaign, getWebCampaignSettings, type WebCampaign, type WebCampaignScope } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { entityMeta } from "../don/orderData";
import { missingTarget, type PickedTarget } from "./campaignRules";

export { usePerms } from "../don/orderData";

/** Chủ đề realtime của chiến dịch web (promotion campaignSignalTopic). */
export const CAMPAIGN_TOPIC = "promotion.web_campaign";

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
 * Đích đang chọn theo thứ tự targetIds: tên, mã, số mẫu lấy từ `targets` của getWebCampaign (Task 487); mã máy chủ
 * không trả (mẫu đã xóa, danh mục đã bỏ) → missingTarget.
 */
export function pickedTargets(c: WebCampaign): PickedTarget[] {
  const byId = new Map((c.targets ?? []).map((t) => [t.id, t]));
  return c.targetIds.map((id) => byId.get(id) ?? missingTarget(id));
}

/** Trần phần trăm của shop (getWebCampaignSettings); chưa đọc được thì undefined (câu chữ bỏ số). */
export function useMaxPercent(): number | undefined {
  const q = useQuery({
    queryKey: queryKey(CAMPAIGN_TOPIC, "settings"),
    queryFn: () => unwrap(getWebCampaignSettings()),
  });
  return q.data?.maxPercent;
}

/**
 * Ghi bản trả về của một lệnh ghi (không có `targets`, `bannerPositions`: chỉ getWebCampaign trả) vào bộ nhớ đệm của
 * chiến dịch: cùng đích thì giữ tên đích, banner của bản đã đọc; đích đổi thì bỏ bản đệm để lần mở sau đọc lại.
 */
export function storeCampaign(qc: QueryClient, c: WebCampaign) {
  const key = campaignKey(c.id);
  const old = qc.getQueryData<WebCampaign>(key);
  if (old?.targets !== undefined && old.targetIds.join(",") === c.targetIds.join(",")) {
    qc.setQueryData<WebCampaign>(key, { ...c, targets: old.targets, bannerPositions: old.bannerPositions });
  } else {
    qc.removeQueries({ queryKey: key, exact: true });
  }
}

/** Sau một lệnh ghi: đọc lại mọi dữ liệu chiến dịch đang mở. */
export function reloadCampaigns(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: queryKey(CAMPAIGN_TOPIC) });
}
