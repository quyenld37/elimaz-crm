// API giả (fetch) và cách dựng cho test vitest của màn Nhật ký thao tác. Chỉ test nhập tệp này. Trả đúng kiểu AuditLog
// của client sinh (Task 457): 50 dòng mỗi trang, con trỏ là vị trí bắt đầu.
import type { AuditActor, AuditLog, Grant } from "../../api";
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

export const LAN = "0195f3b2-0000-7000-8000-0000000000a1";
export const KEY = "0195f3b2-0000-7000-8000-0000000000b1";
export const OLD = "0195f3b2-0000-7000-8000-0000000000a2";

/** Dòng mẫu: Lan tạm dừng nhận đơn web; sửa bằng p. */
export function log(p: Partial<AuditLog> = {}): AuditLog {
  return {
    id: "0195f3b2-0000-7000-8000-000000000001",
    actorType: "staff",
    actorId: LAN,
    actorName: "Nguyễn Thị Lan",
    action: "order.web_intake.set",
    entityType: "order.web_intake",
    entityId: "0195f3b2-0000-7000-8000-0000000000ff",
    changedFields: ["paused", "reason"],
    createdAt: "2026-11-21T06:05:00Z",
    ...p,
  };
}

/** n dòng khác mã, mới nhất trước (mỗi dòng cách 1 phút). */
export const many = (n: number) =>
  Array.from({ length: n }, (_, i) =>
    log({
      id: `row-${String(i).padStart(4, "0")}`,
      createdAt: new Date(Date.UTC(2026, 10, 21, 6, 0) - i * 60_000).toISOString(),
    }),
  );

export interface Fake extends WriteQueue {
  grants: Grant[];
  rows: AuditLog[];
  /** Người làm của listAuditActors (Task 488); actorsError → trả lỗi đó. */
  actors: AuditActor[];
  actorsError: { status: number; body: unknown } | null;
  listCalls: URLSearchParams[];
  listError: { status: number; body: unknown } | null;
}

function read(fake: Fake, p: string, q: URLSearchParams): Response {
  if (p === "/api/v1/me") return meReply(fake.grants);
  if (p === "/api/v1/identity/audit-actors") {
    return fake.actorsError === null
      ? reply({ items: fake.actors })
      : reply(fake.actorsError.body, fake.actorsError.status);
  }
  if (p !== "/api/v1/identity/audit-logs") return reply(NOT_FOUND, 404);
  fake.listCalls.push(q);
  if (fake.listError !== null) return reply(fake.listError.body, fake.listError.status);
  const from = q.get("from");
  const to = q.get("to");
  const match = (r: AuditLog) =>
    (q.get("action") === null || r.action === q.get("action")) &&
    (q.get("actorId") === null || r.actorId === q.get("actorId")) &&
    (from === null || r.createdAt >= from) &&
    (to === null || r.createdAt < to);
  return reply(page(fake.rows.filter(match), q));
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: [{ permission: "audit.read", scope: { type: "all", id: "" } }],
    rows: [log()],
    actors: [
      { actorType: "staff", id: LAN, name: "Nguyễn Thị Lan", active: true },
      { actorType: "staff", id: OLD, name: "Trần Văn Cũ", active: false },
      { actorType: "service", id: KEY, name: "Website ELIMAZ", active: true },
    ],
    actorsError: null,
    listCalls: [],
    listError: null,
    ...emptyWrites(),
  };
  return stubApi(fake, read, () => reply(NOT_FOUND, 404));
}

export const renderAt = (path = "/nhat-ky-thao-tac") => renderScreens(screens, path);
