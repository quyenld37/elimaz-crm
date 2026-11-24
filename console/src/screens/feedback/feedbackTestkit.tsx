// API giả (fetch) và cách dựng cho test vitest của màn Góp ý. Chỉ test nhập tệp này. Trả đúng kiểu Feedback của client
// sinh (Task 455).
import type { Feedback, Grant } from "../../api";
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
import { screens } from "./route";

export const FB1 = "0195f3b2-0000-7000-8000-00000000fb01";
export const CUST = "0195f3b2-0000-7000-8000-00000000c001";
export const LONG_NAME = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";
export const SCRIPT_BODY = "Trang mới dễ xem hơn hẳn <script>alert(1)</script>";

/** Góp ý mẫu: giao hàng 2/5, gắn khách CUST, có đơn, trang, thiết bị, ghi chú; sửa bằng p. */
export function feedback(p: Partial<Feedback> = {}): Feedback {
  return {
    id: FB1,
    topic: "delivery",
    mood: 2,
    body: "Shipper giao trễ 2 ngày, không gọi trước, hộp bị móp một góc.",
    name: "Trần Thị Thu Hà",
    contact: "0912 345 678",
    orderRef: "W0001312",
    page: "/don-hang/W0001312",
    device: "iPhone · Safari 18",
    state: "new",
    note: "Đã báo Viettel Post.",
    customerId: CUST,
    customerName: "Trần Thị Thu Hà",
    customerCode: "KH000231",
    createdAt: "2026-11-21T03:12:00Z",
    updatedAt: "2026-11-21T03:12:00Z",
    version: 1,
    ...p,
  };
}

export const grantsOf = (...perms: string[]): Grant[] =>
  ["customer.feedback.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

export interface Fake extends WriteQueue {
  grants: Grant[];
  /** Mọi góp ý; listFeedback lọc state, topic, customerId rồi trả theo trang `limit`, con trỏ là vị trí bắt đầu. */
  rows: Feedback[];
  listCalls: URLSearchParams[];
  /** Lỗi trả cho mọi lần đọc danh sách (ví dụ 403). */
  listError: { status: number; body: unknown } | null;
  /** Số lần đọc countFeedback (đếm từ rows như máy chủ). */
  countReads: number;
}

function counts(fake: Fake): Response {
  fake.countReads++;
  const states = (["new", "seen", "answered"] as const).map((state) => ({
    state,
    count: fake.rows.filter((r) => r.state === state).length,
  }));
  return reply({ states, total: fake.rows.length });
}

/** Tham số lọc của listFeedback và trường của góp ý nó so. */
const FILTERS: readonly [string, (r: Feedback) => string | undefined][] = [
  ["state", (r) => r.state],
  ["topic", (r) => r.topic],
  ["customerId", (r) => r.customerId],
];

function read(fake: Fake, p: string, q: URLSearchParams): Response {
  if (p === "/api/v1/me") return meReply(fake.grants);
  if (p === "/api/v1/feedback/counts") return counts(fake);
  if (p !== "/api/v1/feedback") return reply(NOT_FOUND, 404);
  fake.listCalls.push(q);
  if (fake.listError !== null) return reply(fake.listError.body, fake.listError.status);
  const match = (r: Feedback) => FILTERS.every(([k, get]) => q.get(k) === null || get(r) === q.get(k));
  return reply(page(fake.rows.filter(match), q));
}

const isState = (v: unknown): v is Feedback["state"] => v === "new" || v === "seen" || v === "answered";

/** PATCH mặc định: áp trường gửi lên, tăng version, lưu vào rows. */
function writeReply(fake: Fake, p: string, body: Record<string, unknown>): Response {
  const id = /^\/api\/v1\/feedback\/([^/]+)$/.exec(p)?.[1];
  const i = fake.rows.findIndex((r) => r.id === id);
  const r = fake.rows[i];
  if (r === undefined) return reply(NOT_FOUND, 404);
  const next: Feedback = {
    ...r,
    ...(isState(body.state) ? { state: body.state } : {}),
    ...(typeof body.note === "string" ? { note: body.note.trim() } : {}),
    version: r.version + 1,
    updatedByName: "Tôi",
  };
  fake.rows[i] = next;
  return reply(next);
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf("customer.feedback.manage", "customer.read"),
    rows: [feedback()],
    listCalls: [],
    listError: null,
    countReads: 0,
    ...emptyWrites(),
  };
  return stubApi(fake, read, writeReply);
}

export const renderAt = (path: string) =>
  renderScreens(screens, path, [{ path: "/khach/:id", element: <p>Trang hồ sơ khách</p> }]);
