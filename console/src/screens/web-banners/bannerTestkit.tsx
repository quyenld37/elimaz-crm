// API giả (fetch) và cách dựng cho test vitest của màn Banner trang chủ. Chỉ test nhập tệp này. Trả đúng kiểu của
// client sinh (HomeBannerList, BannerPreview của Task 453; FileInfo, Upload của kho tệp M1).
import type {
  BannerPreview,
  FileInfo,
  Grant,
  HomeBanner,
  HomeBannerList,
  HomeBannerStatus,
  ProductListItem,
  Upload,
} from "../../api";
import { defaultSleep } from "../../app/idempotency";
import { emptyWrites, meReply, page, renderScreens, reply, stubApi, type WriteQueue } from "../../app/screenTestkit";
import type { PutFile, UploadOptions } from "../products/useUpload";
import { BannerEditor } from "./BannerEditor";

export { stubDialog } from "../don/donTestkit";

export const B = (n: number) => `0195f3b2-0000-7000-8000-0000000b${String(n).padStart(4, "0")}`;
export const F_DESK = "0195f3b2-0000-7000-8000-0000000f0001";
export const F_MOB = "0195f3b2-0000-7000-8000-0000000f0002";
export const F_NEW = "0195f3b2-0000-7000-8000-0000000f0009";
export const C1 = "0195f3b2-0000-7000-8000-00000000e001";
export const CAT1 = "0195f3b2-0000-7000-8000-0000000c0001";
export const P1 = "0195f3b2-0000-7000-8000-0000000a0001";
export const P2 = "0195f3b2-0000-7000-8000-0000000a0002";
export const P3 = "0195f3b2-0000-7000-8000-0000000a0003";
export const P4 = "0195f3b2-0000-7000-8000-0000000a0004";

/** Quyền content.read cộng perms (mặc định đủ quyền sửa, tải ảnh, tìm mẫu). */
export const grantsOf = (...perms: string[]): Grant[] =>
  ["content.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));
export const FULL = ["content.manage", "files.upload", "catalog.read"] as const;

/** Banner giảm giá thứ n (tiêu đề "Banner n"). */
export const promo = (n: number, p: Partial<HomeBanner> = {}): HomeBanner => ({
  id: B(n),
  enabled: true,
  kind: "campaign",
  campaignId: C1,
  title: `Banner ${String(n)}`,
  theme: "auto",
  ...p,
});

/** Banner ảnh "Black Friday 27.11": ảnh máy tính sẵn sàng, ảnh điện thoại theo images. */
export const imageBanner = (n: number, p: Partial<HomeBanner> = {}): HomeBanner => ({
  id: B(n),
  enabled: true,
  kind: "image",
  imageDesktop: F_DESK,
  imageMobile: F_MOB,
  alt: "Black Friday 27.11",
  linkKind: "offers",
  ...p,
});

export const shown = (id: string): HomeBannerStatus => ({ id, shown: true });

/** Danh sách banner: mặc định mọi banner đang hiện, ảnh máy tính và điện thoại sẵn sàng. */
export function list(banners: HomeBanner[], p: Partial<HomeBannerList> = {}): HomeBannerList {
  return {
    revision: 4,
    banners,
    statuses: banners.map((b) => shown(b.id)),
    images: [
      { fileId: F_DESK, status: "ready", deleted: false, desktop: "/img/desk.webp", desktopSmall: "/img/desk-s.webp" },
      { fileId: F_MOB, status: "ready", deleted: false, desktop: "/img/mob.webp", desktopSmall: "/img/mob-s.webp" },
    ],
    campaigns: [
      {
        id: C1,
        name: "Xả hàng thu đông",
        percent: 50,
        scope: "products",
        state: "enabled",
        startsAt: "2026-11-14T17:00:00Z",
        endsAt: "2026-11-25T16:59:00Z",
        targetIds: [P1, P2, P3, P4],
        running: true,
      },
    ],
    categories: [{ id: CAT1, name: "Đầm công sở" }],
    products: [],
    ...p,
  };
}

const product = (id: string, name: string): ProductListItem => ({
  id,
  code: name.slice(0, 3).toUpperCase(),
  name,
  status: "active",
  categoryPath: [],
  variantCount: 2,
  updatedAt: "2026-11-20T00:00:00Z",
});
export const PRODUCTS: ProductListItem[] = [
  product(P1, "Áo khoác dạ dáng dài"),
  product(P2, "Áo len cổ lọ"),
  product(P3, "Chân váy dạ xếp ly"),
  product(P4, "Đầm len ôm"),
];

export interface Fake extends WriteQueue {
  grants: Grant[];
  list: HomeBannerList;
  /** Trạng thái hiện tại của tệp mới tải (completeUpload, getFile đều trả trạng thái này). */
  fileStatus: FileInfo["status"];
  rejectReason?: string;
  /** Câu trả lời lỗi xếp sẵn cho lần lưu (PUT) kế tiếp; xem trước không lấy mất (khác fake.replies). */
  saveErrors: { status: number; body: unknown }[];
  previews: Record<string, unknown>[];
  previewReply: (body: Record<string, unknown>) => BannerPreview;
}

function fileInfo(fake: Fake, id: string, status: FileInfo["status"]): FileInfo {
  const isNew = id === F_NEW;
  return {
    id,
    purpose: "web_banner",
    status,
    variants: status === "ready" ? [{ width: 1200, format: "webp", url: `/img/${id}-s.webp` }] : [],
    ...(status === "ready" ? (isNew ? { width: 1920, height: 600 } : { width: 2368, height: 600 }) : {}),
    ...(status === "rejected" && fake.rejectReason !== undefined ? { rejectReason: fake.rejectReason } : {}),
  };
}

const nextFile = (fake: Fake): FileInfo => fileInfo(fake, F_NEW, fake.fileStatus);

function read(fake: Fake, p: string, q: URLSearchParams): Response {
  if (p === "/api/v1/me") return meReply(fake.grants);
  if (p === "/api/v1/content/banners") return reply(fake.list);
  if (p === "/api/v1/catalog/products") {
    const text = q.get("q")?.toLowerCase() ?? "";
    return reply(
      page(
        PRODUCTS.filter((x) => x.name.toLowerCase().includes(text)),
        q,
      ),
    );
  }
  const f = /^\/api\/v1\/files\/([^/]+)$/.exec(p);
  if (f?.[1] === F_NEW) return reply(nextFile(fake));
  return reply(fileInfo(fake, f?.[1] ?? "", "ready"));
}

const isBanner = (x: unknown): x is HomeBanner => typeof x === "object" && x !== null && "id" in x && "kind" in x;
const bannersOf = (body: Record<string, unknown>): HomeBanner[] =>
  Array.isArray(body.banners) ? body.banners.filter(isBanner) : [];

/** Mặc định: lưu trả danh sách mới (revision + 1); xem trước theo previewReply; tải tệp trả F_NEW. */
function write(fake: Fake, p: string, body: Record<string, unknown>): Response {
  if (p === "/api/v1/content/banners") {
    const err = fake.saveErrors.shift();
    if (err !== undefined) return reply(err.body, err.status);
    const banners = bannersOf(body);
    fake.list = list(banners, { ...fake.list, banners, revision: fake.list.revision + 1 });
    return reply(fake.list);
  }
  if (p === "/api/v1/content/banners/preview") {
    fake.previews.push(body);
    return reply(fake.previewReply(body));
  }
  if (p === "/api/v1/files/uploads") {
    const up: Upload = {
      fileId: F_NEW,
      putUrl: "https://kho.test/put",
      headers: {},
      expiresAt: "2026-11-24T10:00:00Z",
    };
    return reply(up, 201);
  }
  if (p === `/api/v1/files/${F_NEW}/complete`) return reply(nextFile(fake));
  return reply({ error: "not_found", request_id: "t" }, 404);
}

export function installFake(l: HomeBannerList = list([promo(1)])): Fake {
  const fake: Fake = {
    grants: grantsOf(...FULL),
    list: l,
    fileStatus: "ready",
    saveErrors: [],
    previews: [],
    previewReply: (body) => ({ statuses: bannersOf(body).map((b) => shown(b.id)) }),
    ...emptyWrites(),
  };
  return stubApi(fake, read, write);
}

export const putOk: PutFile = () => Promise.resolve();
/** Chờ xử lý ảnh 20 ms thay vì 2 s. */
const quick = (_ms: number, signal: AbortSignal) => defaultSleep(20, signal);

/** Dựng màn ở /website/banner; tải ảnh không PUT thật, hỏi trạng thái ảnh mỗi 20 ms. */
export function renderBanners(opts: UploadOptions = { put: putOk, sleep: quick }) {
  return renderScreens(
    [
      {
        path: "/website/banner",
        label: "",
        group: "Website",
        permission: "",
        order: 0,
        element: <BannerEditor uploadOptions={opts} />,
      },
    ],
    "/website/banner",
  );
}

/** Ảnh JPEG giả name (cỡ size byte). */
export const jpg = (name: string, size = 1000) => new File([new Uint8Array(size)], name, { type: "image/jpeg" });
