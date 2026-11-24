// API giả (fetch) và cách dựng cho test vitest của màn Chiến dịch giảm giá. Chỉ test nhập tệp này. Trả đúng kiểu của
// client sinh (WebCampaign, CampaignTarget, WebCampaignPreview của Task 452).
import type { CampaignTarget, Grant, WebCampaign, WebCampaignPreview } from "../../api";
import {
  NOT_FOUND,
  emptyWrites,
  meReply,
  page,
  renderScreens,
  reply,
  stubApi,
  type WriteQueue,
} from "../../app/screenTestkit";
import { grantsOf } from "../ma-giam-gia/couponTestkit";
import { screens } from "./route";

export { stubDialog } from "../don/donTestkit";

export const C1 = "0195f3b2-0000-7000-8000-00000000e001";
export const C2 = "0195f3b2-0000-7000-8000-00000000e002";
export const C3 = "0195f3b2-0000-7000-8000-00000000e003";
export const CAT1 = "0195f3b2-0000-7000-8000-0000000c0001";
export const CAT2 = "0195f3b2-0000-7000-8000-0000000c0002";
export const CAT3 = "0195f3b2-0000-7000-8000-0000000c0003";
/** Tên 120 ký tự (dữ liệu khó của mockup). */
export const LONG_NAME =
  "Siêu sale cuối năm cho dân văn phòng: đầm công sở, áo sơ mi lụa, chân váy bút chì, quần âu ống suông và áo khoác dạ giảm";

/** Chiến dịch "Xả hàng thu đông" đang áp dụng (50%, 2 sản phẩm, hàng cắt mã); sửa bằng p. */
export function campaign(p: Partial<WebCampaign> = {}): WebCampaign {
  return {
    id: C1,
    name: "Xả hàng thu đông",
    percent: 50,
    scope: "categories",
    targetIds: [CAT1, CAT2],
    allowCoupon: false,
    clearance: true,
    startsAt: "2026-11-14T17:00:00Z",
    endsAt: "2026-11-25T16:59:00Z",
    state: "enabled",
    status: "active",
    revision: 3,
    linkKey: "0193a7c2",
    path: "/uu-dai/xa-hang-thu-dong-0193a7c2",
    createdAt: "2026-11-10T00:00:00Z",
    updatedAt: "2026-11-10T00:00:00Z",
    ...p,
  };
}

/** Chiến dịch tạm dừng "Tri ân khách quen tháng 11" (15%, 2 danh mục). */
export const paused = (p: Partial<WebCampaign> = {}) =>
  campaign({
    id: C2,
    name: "Tri ân khách quen tháng 11",
    percent: 15,
    clearance: false,
    state: "paused",
    status: "paused",
    revision: 5,
    linkKey: "0193a7c3",
    path: "/uu-dai/tri-an-khach-quen-0193a7c3",
    ...p,
  });

export const TARGETS: CampaignTarget[] = [
  { id: CAT1, code: "dam-cong-so", name: "Đầm công sở" },
  { id: CAT2, code: "ao-dai-cach-tan", name: "Áo dài cách tân" },
  { id: CAT3, code: "dam-du-tiec", name: "Đầm dự tiệc" },
];

export const preview = (revision: number, p: Partial<WebCampaignPreview> = {}): WebCampaignPreview => ({
  revision,
  modelCount: 142,
  variantCount: 610,
  samples: [
    {
      productId: "0195f3b2-0000-7000-8000-0000000a0001",
      variantId: "0195f3b2-0000-7000-8000-0000000b0001",
      productName: "Chân váy bút chì",
      listPrice: 420_000,
      salePrice: 357_000,
    },
  ],
  ...p,
});

// Cùng quyền promotion.read + perms như màn Mã giảm giá.
export { grantsOf };

export interface Fake extends WriteQueue {
  grants: Grant[];
  rows: WebCampaign[];
  listCalls: URLSearchParams[];
  targets: CampaignTarget[];
  targetCalls: URLSearchParams[];
}

function read(fake: Fake, p: string, q: URLSearchParams): Response {
  if (p === "/api/v1/me") return meReply(fake.grants);
  if (p === "/api/v1/web-campaigns") {
    fake.listCalls.push(q);
    const status = q.get("status");
    return reply(page(status === null ? fake.rows : fake.rows.filter((r) => r.status === status), q));
  }
  if (p === "/api/v1/web-campaign-targets") {
    fake.targetCalls.push(q);
    const text = q.get("q")?.toLowerCase() ?? "";
    return reply(
      page(
        fake.targets.filter((t) => t.name.toLowerCase().includes(text)),
        q,
      ),
    );
  }
  const one = /^\/api\/v1\/web-campaigns\/([^/]+)$/.exec(p);
  const c = fake.rows.find((r) => r.id === one?.[1]);
  return c === undefined ? reply(NOT_FOUND, 404) : reply(c);
}

const isText = (x: unknown): x is string => typeof x === "string";

/** base với tên, phần trăm, đích lấy từ thân WebCampaignInput đã gửi (chỉ các trường test cần). */
function applyInput(base: WebCampaign, raw: unknown): WebCampaign {
  if (typeof raw !== "object" || raw === null) return base;
  const v = new Map<string, unknown>(Object.entries(raw));
  const name = v.get("name");
  const percent = v.get("percent");
  const ids = v.get("targetIds");
  return {
    ...base,
    ...(typeof name === "string" ? { name } : {}),
    ...(typeof percent === "number" ? { percent } : {}),
    ...(Array.isArray(ids) ? { targetIds: ids.filter(isText) } : {}),
  };
}

/** Câu trả lời mặc định của yêu cầu ghi: tạo/lưu/đổi trạng thái trả bản mới (revision + 1), xem trước trả preview. */
function write(fake: Fake, p: string, body: Record<string, unknown>): Response {
  if (p === "/api/v1/web-campaigns") {
    const created = applyInput(campaign({ id: C3, state: "draft", status: "draft", revision: 1 }), body);
    fake.rows = [created, ...fake.rows];
    return reply(created, 201);
  }
  const m = /^\/api\/v1\/web-campaigns\/([^/]+)(?:\/(preview|state|delete))?$/.exec(p);
  const c = fake.rows.find((r) => r.id === m?.[1]);
  if (m === null || c === undefined) return reply(NOT_FOUND, 404);
  if (m[2] === "preview") return reply(preview(c.revision));
  if (m[2] === "delete") return reply({ revision: c.revision + 1 });
  const next =
    m[2] === "state"
      ? campaign({ ...c, state: body.state === "enabled" ? "enabled" : "paused", revision: c.revision + 1 })
      : applyInput({ ...c, revision: c.revision + 1 }, body.campaign);
  fake.rows = fake.rows.map((r) => (r.id === c.id ? next : r));
  return reply(next);
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf("promotion.manage"),
    rows: [campaign(), paused()],
    listCalls: [],
    targets: TARGETS,
    targetCalls: [],
    ...emptyWrites(),
  };
  return stubApi(fake, read, write);
}

export const renderAt = (path: string) => renderScreens(screens, path);
