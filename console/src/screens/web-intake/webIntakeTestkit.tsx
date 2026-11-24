// API giả (fetch) và cách dựng cho test vitest của màn Nhận đơn web. Chỉ test nhập tệp này. Trả đúng kiểu WebIntake
// của client sinh (Task 456): PUT so version (409 version_conflict), tạm dừng thiếu lý do → 422 invalid_input.
import type { Grant, WebIntake } from "../../api";
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

export const LAN = "0195f3b2-0000-7000-8000-0000000000a1";
export const NOW = "2026-11-21T06:05:00Z";

/** Trạng thái mẫu: đang nhận đơn, Lan mở lại lúc 08:00 20/11/2026 (giờ VN); sửa bằng p. */
export function intake(p: Partial<WebIntake> = {}): WebIntake {
  return {
    paused: false,
    changedBy: LAN,
    changedByName: "Nguyễn Thị Lan",
    changedAt: "2026-11-20T01:00:00Z",
    version: 3,
    ...p,
  };
}

export const grantsOf = (...perms: string[]): Grant[] =>
  ["order.read", ...perms].map((permission) => ({ permission, scope: { type: "all", id: "" } }));

export interface Fake extends WriteQueue {
  grants: Grant[];
  state: WebIntake;
  reads: number;
  readError: { status: number; body: unknown } | null;
}

function read(fake: Fake, p: string): Response {
  if (p === "/api/v1/me") return meReply(fake.grants);
  if (p !== "/api/v1/web-intake") return reply(NOT_FOUND, 404);
  fake.reads++;
  if (fake.readError !== null) return reply(fake.readError.body, fake.readError.status);
  return reply(fake.state);
}

function write(fake: Fake, p: string, body: Record<string, unknown>): Response {
  if (p !== "/api/v1/web-intake") return reply(NOT_FOUND, 404);
  if (body.version !== fake.state.version) return reply({ error: "version_conflict", request_id: "t" }, 409);
  const paused = body.paused === true;
  const reason = typeof body.reason === "string" ? body.reason.trim() : "";
  if (paused && reason === "") return reply({ error: "invalid_input", request_id: "t" }, 422);
  fake.state = {
    paused,
    ...(reason === "" ? {} : { reason }),
    changedBy: "me",
    changedByName: "Tôi",
    changedAt: NOW,
    version: fake.state.version + 1,
  };
  return reply(fake.state);
}

export function installFake(): Fake {
  const fake: Fake = {
    grants: grantsOf("order.web_intake.manage"),
    state: intake(),
    reads: 0,
    readError: null,
    ...emptyWrites(),
  };
  return stubApi(fake, read, write);
}

export const renderAt = () => renderScreens(screens, "/website/nhan-don-web");
