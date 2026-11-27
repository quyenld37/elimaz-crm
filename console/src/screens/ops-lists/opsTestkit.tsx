// API giả (fetch) và dữ liệu cứng của mockup cho test vitest của các danh sách vận hành (Task 135). Chỉ test nhập tệp
// này. Trả đúng kiểu của client sinh.
import { vi } from "vitest";
import type {
  AgedReservation,
  CallbackTask,
  Grant,
  Me,
  Role,
  ShipmentReviewCounts,
  ShipmentReviewItem,
  StaffMember,
  StuckShipment,
} from "../../api";
import { NOT_FOUND, bodyOf, page, renderScreens, reply, type Write } from "../../app/screenTestkit";
import { screens } from "./route";

export { stubDialog } from "../don/donTestkit";

export const ME = "0195f3b2-0000-7000-8000-0000000000e1";
export const AN = "0195f3b2-0000-7000-8000-0000000000e2";
export const DUC = "0195f3b2-0000-7000-8000-0000000000e3";
export const ORDER = "0195f3b2-0000-7000-8000-00000000a101";
export const SHIP = "0195f3b2-0000-7000-8000-00000000b101";
export const LONG_NAME = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";
export const PHONE = "0912888000";

const HOUR = 3_600_000;
const hex = (n: number) => n.toString(16).padStart(12, "0");
export const taskId = (i: number) => `0195f3b2-0000-7000-8003-${hex(0x1000 + i)}`;

/** Việc gọi lại thứ i: hạn sau `dueInHours` giờ tính từ bây giờ (âm = quá hạn). */
export function task(i: number, dueInHours: number, p: Partial<CallbackTask> = {}): CallbackTask {
  const now = Date.now();
  return {
    id: taskId(i),
    shipmentId: SHIP,
    orderId: ORDER,
    orderCode: `DH${String(1000 + i).padStart(7, "0")}`,
    customerName: `Khách ${String(i)}`,
    carrierKey: "vtp",
    trackingCode: "1234567890001",
    attemptNo: 1,
    failedReason: "Khách đi vắng",
    failedAt: new Date(now - 24 * HOUR).toISOString(),
    dueAt: new Date(now + dueInHours * HOUR).toISOString(),
    overdue: dueInHours < 0,
    status: "open",
    deliveryStats: { delivered: 3, returned: 2, returnRateBp: 4000, risk: "high" },
    version: 1,
    ...p,
  };
}

export const grantsOf = (...perms: string[]): Grant[] =>
  perms.map((permission) => ({ permission, scope: { type: "all", id: "" } }));
export const WORKER = [
  "fulfillment.callbacks.work",
  "fulfillment.read",
  "order.read",
  "fulfillment.correct",
  "fulfillment.ship",
  "inventory.aged.read",
];

export function staff(
  id: string,
  name: string,
  roleKey: string,
  status: StaffMember["status"] = "active",
): StaffMember {
  return {
    id,
    email: `${id.slice(-2)}@elimaz.vn`,
    displayName: name,
    status,
    version: 1,
    createdAt: "2026-11-01T00:00:00Z",
    grants: [{ roleKey, scopeType: "all", scopeId: "" }],
  };
}
export const role = (key: string, permissions: string[]): Role => ({
  id: `r-${key}`,
  key,
  name: key,
  status: "active",
  customized: false,
  version: 1,
  holderCount: 1,
  permissions,
});

export interface Fake {
  grants: Grant[];
  tasks: CallbackTask[];
  /** Số việc mở báo ở openCount (mặc định = số việc). */
  openCount?: number;
  counts: ShipmentReviewCounts;
  stuck: StuckShipment[];
  review: ShipmentReviewItem[];
  aged: AgedReservation[];
  staff: StaffMember[];
  roles: Role[];
  orderPhone: string;
  listCalls: URLSearchParams[];
  orderReads: string[];
  writes: Write[];
  /** Trả lần lượt cho mỗi yêu cầu ghi: mã HTTP và thân. */
  replies: { status: number; body: unknown }[];
  failReads: RegExp[];
}

/** Phần của đơn mà màn đọc (người nhận); đủ cho getOrder giả. */
const order = (f: Fake) => ({
  id: ORDER,
  code: "DH0001231",
  shipAddress: { name: "Trần Thị Thu Hà", phone: f.orderPhone, line: "1 Láng Hạ", provinceCode: "01", wardCode: "001" },
});

type Handler = (m: RegExpExecArray, q: URLSearchParams, f: Fake) => Response;

const ROUTES: [RegExp, Handler][] = [
  [/^\/api\/v1\/me$/, (_m, _q, f) => reply({ userId: ME, email: "a@elimaz.vn", grants: f.grants } satisfies Me)],
  [
    /^\/api\/v1\/callback-tasks$/,
    (_m, q, f) => {
      f.listCalls.push(q);
      const st = q.get("status");
      const mine = q.get("assignee") === "me";
      const all = f.tasks
        .filter((t) => (st === null ? t.status === "open" || t.status === "in_progress" : t.status === st))
        .filter((t) => !mine || t.assignee?.id === ME);
      return reply({ ...page(all, q), openCount: f.openCount ?? all.length });
    },
  ],
  [/^\/api\/v1\/orders\/([^/]+)$/, (_m, _q, f) => (f.orderReads.push(_m[1] ?? ""), reply(order(f)))],
  [/^\/api\/v1\/shipments\/stuck$/, (_m, q, f) => reply(page(f.stuck, q))],
  [/^\/api\/v1\/shipments\/review$/, (_m, q, f) => reply({ ...page(f.review, q), counts: f.counts })],
  [
    /^\/api\/v1\/inventory\/aged-reservations$/,
    (_m, q, f) => {
      const st = q.get("state");
      return reply(page(st === null ? f.aged : f.aged.filter((a) => a.state === st), q));
    },
  ],
  [/^\/api\/v1\/identity\/users$/, (_m, _q, f) => reply(f.staff)],
  [/^\/api\/v1\/identity\/roles$/, (_m, _q, f) => reply(f.roles)],
  [/^\/api\/v1\/inventory\/warehouses$/, () => reply([{ id: "w1", code: "KC", name: "Kho chính", status: "active" }])],
];

export function installFake(p: Partial<Fake> = {}): Fake {
  const fake: Fake = {
    grants: grantsOf(...WORKER),
    tasks: [],
    counts: { review: 0, stuck: 0, callbacksOpen: 0, callbacksOverdue: 0, inDoubt: 0 },
    stuck: [],
    review: [],
    aged: [],
    staff: [],
    roles: [],
    orderPhone: PHONE,
    listCalls: [],
    orderReads: [],
    writes: [],
    replies: [],
    failReads: [],
    ...p,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      if (req.method === "GET") {
        if (fake.failReads.some((re) => re.test(url.pathname))) {
          return reply({ error: "forbidden", request_id: "t" }, 403);
        }
        const hit = ROUTES.map(([re, h]) => [re.exec(url.pathname), h] as const).find(([m]) => m !== null);
        return hit?.[0] == null ? reply(NOT_FOUND, 404) : hit[1](hit[0], url.searchParams, fake);
      }
      const body = await bodyOf(req);
      fake.writes.push({ method: req.method, path: url.pathname, key: req.headers.get("Idempotency-Key"), body });
      const next = fake.replies.shift();
      if (next !== undefined) return reply(next.body, next.status);
      return applyWrite(fake, url.pathname, body);
    }),
  );
  return fake;
}

/** Ghi giả: đổi việc gọi lại như máy chủ (kết quả → trạng thái; nhận việc → người làm). */
function applyWrite(f: Fake, path: string, body: Record<string, unknown>): Response {
  const m = /^\/api\/v1\/callback-tasks\/([^/]+)\/(outcomes|assign)$/.exec(path);
  const t = f.tasks.find((x) => x.id === m?.[1]);
  if (m === null || t === undefined) return reply({ ...t }, 200);
  if (m[2] === "assign") {
    const s = f.staff.find((x) => x.id === body.assigneeId);
    t.assignee = { id: String(body.assigneeId), name: s?.displayName ?? "Tôi" };
  } else {
    const o = body.outcome;
    t.status = o === "no_answer" || o === "reschedule" ? "in_progress" : "done";
    if (typeof body.nextCallAt === "string") t.nextCallAt = body.nextCallAt;
  }
  t.version += 1;
  return reply(t);
}

export const renderAt = (path: string) =>
  renderScreens(screens, path, [{ path: "/don/:id", element: <p>Trang đơn</p> }]);

/** Bố cục giả cho jsdom (không bố cục): khung cuộn `.cb-list` cao 640 rộng 600, mỗi dòng `.cb-slot` cao 120. */
export function fakeLayout(): void {
  const sizeOf = (el: HTMLElement, list: number, other = 0) => (el.classList.contains("cb-list") ? list : other);
  Object.defineProperties(HTMLElement.prototype, {
    clientHeight: {
      configurable: true,
      get: function get(this: HTMLElement) {
        return sizeOf(this, 640);
      },
    },
    clientWidth: {
      configurable: true,
      get: function get(this: HTMLElement) {
        return sizeOf(this, 600);
      },
    },
  });
  HTMLElement.prototype.getBoundingClientRect = function rect(this: HTMLElement) {
    const height = this.classList.contains("cb-slot") ? 120 : 0;
    return new DOMRect(0, 0, 600, height);
  };
}
