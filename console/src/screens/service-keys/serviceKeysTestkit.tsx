// API giả (fetch) và cách dựng cho test vitest của màn Khóa dịch vụ (Task 208). Chỉ test nhập tệp này. Trả đúng kiểu
// của client sinh (ServiceKey, CreatedServiceKey của Task 189).
import type { CreatedServiceKey, Grant, ServiceKey } from "../../api";
import {
  NOT_FOUND,
  emptyWrites,
  meReply,
  renderScreens,
  reply,
  stubApi,
  type WriteQueue,
} from "../../app/screenTestkit";
import { screens } from "./route";

export { stubDialog } from "../don/donTestkit";

export const K1 = "0195f3b2-0000-7000-8000-00000000f001";
export const K2 = "0195f3b2-0000-7000-8000-00000000f002";
export const K3 = "0195f3b2-0000-7000-8000-00000000f003";
export const K4 = "0195f3b2-0000-7000-8000-00000000f004";
export const KNEW = "0195f3b2-0000-7000-8000-00000000f0ff";
/** Tên 80 ký tự không dấu cách (dữ liệu khó của mockup). */
export const LONG_NAME = "WebsiteELIMAZBanChinhMayChuSingaporeChoDonHangVaDanhMucKhoaThayTheThang11Nam2026";
/** Bí mật giả của khóa vừa tạo (entropy thấp để gitleaks không nhận nhầm): không được lọt vào kho của trình duyệt, bộ
 * đệm, log. */
export const SECRET = `ek_aaaabbbb0001_${"AB".repeat(26)}`;

export const ALL_WEB = [
  "web.catalog.read",
  "web.orders.create",
  "web.orders.read",
  "web.orders.cancel",
  "web.requests.write",
  "web.coupons.issue",
  "web.birthdays.write",
  "web.feedback.write",
  "web.shipping.quote",
  "web.flash.buy",
];

/** Khóa "Website ELIMAZ — bản chính" đang dùng, đủ mười quyền, mọi IP. */
const MAIN_KEY: ServiceKey = {
  id: K1,
  name: "Website ELIMAZ — bản chính",
  keyPrefix: "aaaabbbb0001",
  permissions: ALL_WEB,
  allowedCidrs: [],
  createdAt: "2026-11-14T03:00:00Z",
  lastUsedAt: "2026-11-21T01:14:00Z",
  version: 1,
};

/** MAIN_KEY sửa bằng p. */
export const key = (p: Partial<ServiceKey> = {}): ServiceKey => ({ ...MAIN_KEY, ...p });

/** Mockup A: đang dùng, giới hạn IP, tên 80 ký tự chưa dùng, đã thu hồi. */
export function mockupKeys(): ServiceKey[] {
  return [
    key(),
    key({
      id: K2,
      name: "Website ELIMAZ — bản thử (staging)",
      keyPrefix: "aaaabbbb0002",
      allowedCidrs: ["13.140.64.56/32"],
      version: 2,
    }),
    key({
      id: K3,
      name: LONG_NAME,
      keyPrefix: "aaaabbbb0003",
      permissions: ["web.catalog.read", "web.orders.create"],
      createdAt: "2026-11-21T01:14:00Z",
      lastUsedAt: undefined,
    }),
    key({
      id: K4,
      name: "Thử nối website ngày 10/11",
      keyPrefix: "aaaabbbb0004",
      permissions: ["web.catalog.read"],
      revokedAt: "2026-11-11T03:00:00Z",
      lastUsedAt: "2026-11-10T10:02:00Z",
      version: 3,
    }),
  ];
}

export const grantsOf = (...perms: string[]): Grant[] =>
  perms.map((permission) => ({ permission, scope: { type: "all", id: "" } }));

export interface Fake extends WriteQueue {
  grants: Grant[];
  keys: ServiceKey[];
  listCalls: number;
}

function read(fake: Fake, p: string): Response {
  if (p === "/api/v1/me") return meReply(fake.grants);
  if (p === "/api/v1/identity/service-keys") {
    fake.listCalls++;
    return reply(fake.keys);
  }
  return reply(NOT_FOUND, 404);
}

const texts = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : []);

/** Tạo: 201 kèm secret (danh sách chỉ thêm khóa không bí mật); thu hồi: revokedAt, version + 1. */
function write(fake: Fake, p: string, body: Record<string, unknown>): Response {
  if (p === "/api/v1/identity/service-keys") {
    const k = key({
      id: KNEW,
      name: typeof body.name === "string" ? body.name : "",
      keyPrefix: "aaaabbbb0001",
      permissions: texts(body.permissions),
      allowedCidrs: texts(body.allowedCidrs),
      createdAt: "2026-11-24T03:00:00Z",
      lastUsedAt: undefined,
    });
    fake.keys = [...fake.keys, k];
    return reply({ ...k, secret: SECRET } satisfies CreatedServiceKey, 201);
  }
  const m = /^\/api\/v1\/identity\/service-keys\/([^/]+)\/revoke$/.exec(p);
  const k = fake.keys.find((x) => x.id === m?.[1]);
  if (k === undefined) return reply(NOT_FOUND, 404);
  const next = { ...k, revokedAt: "2026-11-24T03:05:00Z", version: k.version + 1 };
  fake.keys = fake.keys.map((x) => (x.id === k.id ? next : x));
  return reply(next);
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf("identity.service_keys.manage"),
    keys: mockupKeys(),
    listCalls: 0,
    ...emptyWrites(),
  };
  return stubApi(fake, (f, p) => read(f, p), write);
}

export const renderAt = (path = "/website/khoa-dich-vu") => renderScreens(screens, path);
