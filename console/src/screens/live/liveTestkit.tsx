// API giả (fetch) và dữ liệu cứng của mockup cho test vitest của màn Live và Đã chốt, thiếu địa chỉ (Task 177). Chỉ
// test nhập tệp này. Trả đúng kiểu của client sinh (LiveSession, LiveComment của Task 170, 172; PendingDraft của 173).
import type {
  ChannelAccount,
  ClaimLiveCommentResult,
  Grant,
  LiveComment,
  LiveCommentContext,
  LiveSession,
  PageSettings,
  PendingDraft,
  PresenceEntry,
  RemindOutcome,
  ReplyTemplate,
  Warehouse,
} from "../../api";
import {
  NOT_FOUND,
  emptyWrites,
  meReply,
  renderScreens,
  reply,
  stubApi,
  type WriteQueue,
} from "../../app/screenTestkit";
import { useParams } from "react-router";
import { HITS, balance } from "../len-don/orderTestkit";
import { afterOf } from "./liveRules";
import { screens } from "./route";

export { HITS } from "../len-don/orderTestkit";

export const PAGE = "0195f3b2-0000-7000-8000-00000000e001";
export const S_LIVE = "0195f3b2-0000-7000-8000-00000000e101";
export const S_ENDED = "0195f3b2-0000-7000-8000-00000000e102";
export const TPL = "0195f3b2-0000-7000-8000-00000000e201";
export const W1 = "0195f3b2-0000-7000-8000-00000000a001";
export const ORD_NEW = "0195f3b2-0000-7000-8000-00000000d241";
export const ORD_OLD = "0195f3b2-0000-7000-8000-00000000d238";
export const AN = { id: "0195f3b2-0000-7000-8000-00000000f0a1", name: "Trần Minh An" };
export const LONG_NAME = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";

const BASE = Date.parse("2026-11-24T13:00:00Z"); // 20:00 giờ Việt Nam

const hex = (n: number) => n.toString(16).padStart(12, "0");
/** Mã comment thứ i (uuid hợp lệ, tăng theo i). */
export const msgId = (i: number) => `0195f3b2-0000-7000-8000-${hex(0x100000 + i)}`;
const convId = (i: number) => `0195f3b2-0000-7000-8001-${hex(0x100000 + i)}`;
const custId = (i: number) => `0195f3b2-0000-7000-8002-${hex(0x100000 + i)}`;

/** Comment thứ i: gửi lúc 20:00 + i giây; sửa bằng p. */
export function comment(i: number, p: Partial<LiveComment> = {}): LiveComment {
  return {
    messageId: msgId(i),
    conversationId: convId(i),
    customer: { id: custId(i), name: `Khách ${String(i)}` },
    text: `comment số ${String(i)}`,
    sentAt: new Date(BASE + i * 1000).toISOString(),
    hasPhone: false,
    isHidden: false,
    privateReply: { state: "none" },
    customerHasOpenDraft: false,
    ...p,
  };
}

export function session(p: Partial<LiveSession> = {}): LiveSession {
  return {
    id: S_LIVE,
    pageId: PAGE,
    liveVideoId: "123456789",
    title: "Xả hàng đầm lụa tối thứ Ba",
    status: "live",
    startedAt: new Date(BASE).toISOString(),
    detectedBy: "webhook",
    receivedCount: 600,
    activeSource: "sse",
    version: 1,
    ...p,
  };
}

export const ENDED = session({
  id: S_ENDED,
  title: "Live ca trưa: áo len",
  status: "ended",
  startedAt: "2026-11-24T05:15:00Z",
  endedAt: "2026-11-24T06:40:00Z",
  receivedCount: 600,
  metaCommentCount: 605,
});

export const CHANNEL: ChannelAccount = {
  id: PAGE,
  channel: "facebook",
  externalId: "1001",
  name: "ELIMAZ",
  status: "active",
  sendMode: "enabled",
  humanAgentEnabled: false,
  subscribedFields: [],
  version: 1,
};

export function context(p: Partial<LiveCommentContext> = {}): LiveCommentContext {
  return {
    customer: {
      id: custId(5),
      rootId: custId(5),
      name: "Phạm Ngọc Anh",
      nameSource: "channel",
      doNotContact: false,
      orderCount: 3,
      deliveredCount: 3,
      returnedCount: 0,
      returnRateBp: 0,
      totalSpent: 3_870_000,
    },
    openDrafts: [],
    phones: ["0903222333"],
    sellerId: "0195f3b2-0000-7000-8000-00000000c0c0",
    ...p,
  };
}

export const OLD_DRAFT = {
  orderId: ORD_OLD,
  code: "DH0001238",
  items: [{ variantId: "0195f3b2-0000-7000-8000-00000000b001", name: "Đầm lụa be L", quantity: 2 }],
  grandTotal: 2_580_000,
  version: 4,
};

export function claimResult(p: Partial<ClaimLiveCommentResult> = {}): ClaimLiveCommentResult {
  return {
    orderId: ORD_NEW,
    orderCode: "DH0001241",
    claim: { by: AN, orderId: ORD_NEW, orderCode: "DH0001241" },
    ask: { status: "queued" },
    ...p,
  };
}

export function pending(i: number, p: Partial<PendingDraft> = {}): PendingDraft {
  return {
    orderId: `0195f3b2-0000-7000-8003-${hex(0x100000 + i)}`,
    orderCode: `DH000${String(1240 + i)}`,
    conversationId: `0195f3b2-0000-7000-8004-${hex(0x100000 + i)}`,
    customer: { id: custId(i), name: `Khách ${String(i)}` },
    items: [{ variantId: "0195f3b2-0000-7000-8000-00000000b001", name: "Đầm lụa be L", quantity: 1 }],
    grandTotal: 1_290_000,
    expiresAt: new Date(BASE + 86_400_000 + i * 60_000).toISOString(),
    claimedBy: AN,
    claimedAt: new Date(BASE + i * 60_000).toISOString(),
    ask: { status: "queued" },
    customerRepliedAfterAsk: false,
    canRemind: { allowed: true },
    ...p,
  };
}

export interface LiveFake extends WriteQueue {
  grants: Grant[];
  sessions: LiveSession[];
  comments: LiveComment[];
  /** Tham số của từng lần gọi listLiveComments. */
  commentCalls: URLSearchParams[];
  /** Tham số của từng lần gọi searchVariants. */
  searchCalls: URLSearchParams[];
  ctx: LiveCommentContext;
  presence: PresenceEntry[];
  drafts: PendingDraft[];
  expiredCount: number;
  confirmedCount: number;
  settings: Partial<PageSettings>;
  templates: ReplyTemplate[];
  /** Kết quả nhắc theo mã đơn (thiếu → queued). */
  remindOutcomes: Record<string, RemindOutcome>;
  /** Số có thể bán của HITS[0..2] ở kho chính. */
  available: [number, number, number];
}

export const grantsOf = (...perms: string[]): Grant[] =>
  perms.map((permission) => ({ permission, scope: { type: "all", id: "" } }));

/** Quyền của nhân viên chốt live: đọc hội thoại, chốt, tạo và sửa đơn, đọc tồn. */
export const LIVE_GRANTS = grantsOf(
  "conversation.read",
  "live.operate",
  "order.create",
  "order.update",
  "inventory.read",
);

export function template(p: Partial<ReplyTemplate> = {}): ReplyTemplate {
  return {
    id: TPL,
    pageId: PAGE,
    shortcut: "xindiachi",
    title: "Xin địa chỉ live",
    body: "Chị gửi giúp em địa chỉ nhé",
    quickReplies: [],
    version: 1,
    createdAt: new Date(BASE).toISOString(),
    ...p,
  };
}

/** Trang comment theo after (lùi 30 s như máy chủ; "p:" là đúng vị trí), limit, phoneOnly, unhandledOnly. */
function commentPage(all: readonly LiveComment[], q: URLSearchParams) {
  let list = [...all];
  if (q.get("phoneOnly") === "true") list = list.filter((c) => c.hasPhone);
  if (q.get("unhandledOnly") === "true") list = list.filter((c) => c.claim === undefined);
  const after = q.get("after");
  if (after !== null) {
    const exact = after.startsWith("p:");
    const [sentAt = "", id = ""] = (exact ? after.slice(2) : after).split(",");
    const mark = Date.parse(sentAt) - (exact ? 0 : 30_000);
    list = list.filter((c) => {
      const t = Date.parse(c.sentAt);
      return exact ? t > mark || (t === mark && c.messageId > id) : t >= mark;
    });
  }
  const limit = Number(q.get("limit") ?? "200");
  const items = list.slice(0, limit);
  const last = items.at(-1);
  return list.length > limit && last !== undefined ? { items, nextCursor: `p:${afterOf(last)}` } : { items };
}

export function installLiveFake(p: Partial<LiveFake> = {}): LiveFake {
  const fake: LiveFake = {
    ...emptyWrites(),
    grants: LIVE_GRANTS,
    sessions: [session(), ENDED],
    comments: [],
    commentCalls: [],
    searchCalls: [],
    ctx: context(),
    presence: [],
    drafts: [],
    expiredCount: 0,
    confirmedCount: 0,
    settings: { askAddressTemplateId: TPL },
    templates: [template()],
    remindOutcomes: {},
    available: [5, 0, 1],
    ...p,
  };
  return stubApi(
    fake,
    (f, path, q) => {
      if (path === "/api/v1/me") return meReply(f.grants);
      if (path === "/api/v1/live-sessions") return reply({ items: f.sessions });
      if (path === "/api/v1/channels") return reply({ items: [CHANNEL] });
      if (path === `/api/v1/channels/${PAGE}/page-settings`) {
        return reply({
          channelAccountId: PAGE,
          roundRobinEnabled: false,
          roundRobinUserIds: [],
          liveSlots: [],
          commentPollMode: "auto",
          automationMaxAgeMinutes: 15,
          version: 1,
          ...f.settings,
        } satisfies PageSettings);
      }
      if (path === "/api/v1/reply-templates") return reply({ items: f.templates });
      if (path === "/api/v1/catalog/variants/search") {
        f.searchCalls.push(q);
        const t = (q.get("q") ?? "").toUpperCase();
        return reply(HITS.filter((h) => h.sku.startsWith(t)));
      }
      if (path === "/api/v1/inventory/warehouses") {
        const ws: Warehouse[] = [{ id: W1, code: "MAIN", name: "Kho chính", status: "active" }];
        return reply(ws);
      }
      if (path === "/api/v1/inventory/balances")
        return reply({ items: HITS.map((h, i) => balance(h, f.available[i] ?? 0)) });
      const cm = /^\/api\/v1\/live-sessions\/([^/]+)\/comments$/.exec(path);
      if (cm !== null) {
        f.commentCalls.push(q);
        return reply(commentPage(f.comments, q));
      }
      if (/^\/api\/v1\/live-sessions\/[^/]+\/pending-drafts$/.test(path)) {
        return reply({ items: f.drafts, expiredCount: f.expiredCount, confirmedCount: f.confirmedCount });
      }
      if (/^\/api\/v1\/live-comments\/[^/]+\/context$/.test(path)) return reply(f.ctx);
      if (/^\/api\/v1\/conversations\/[^/]+\/presence$/.test(path)) return reply({ items: f.presence });
      return reply(NOT_FOUND, 404);
    },
    (f, path, body) => {
      if (path.endsWith("/claim")) return reply(claimResult(), 201);
      if (path.endsWith("/presence")) return new Response(null, { status: 204 });
      if (path.endsWith("/pending-drafts/remind")) {
        const ids =
          Array.isArray(body.orderIds) && body.orderIds.length > 0 ? body.orderIds : f.drafts.map((d) => d.orderId);
        const results = ids
          .filter((id): id is string => typeof id === "string")
          .map((orderId) => f.remindOutcomes[orderId] ?? { orderId, outcome: "queued" as const });
        return reply({ results });
      }
      if (path.endsWith("/live-sessions")) return reply(session({ id: "0195f3b2-0000-7000-8000-00000000e109" }), 201);
      return reply(NOT_FOUND, 404);
    },
  );
}

/** Trang hội thoại giả: hiện mã hội thoại trên URL để test biết màn đã mở hội thoại nào. */
function ConversationStub() {
  const { id } = useParams();
  return <p>{`Trang hội thoại ${id ?? ""}`}</p>;
}

/** Trang giả cho liên kết sang màn khác (đơn, hội thoại). */
export const OTHER_PAGES = [
  { path: "/don/:id", element: <p>Trang đơn</p> },
  { path: "/inbox/:id", element: <ConversationStub /> },
] as const;

/** Dựng các màn Live ở đường path. */
export const renderLive = (path: string) => renderScreens(screens, path, OTHER_PAGES);

/** Bố cục giả cho jsdom (không bố cục): khung cuộn comment cao 640 (clientHeight, virtualizer đọc), mỗi dòng 64. */
export function fakeLayout(): void {
  const rect = (h: number): DOMRect => ({
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: 600,
    bottom: h,
    width: 600,
    height: h,
    toJSON: () => ({}),
  });
  HTMLElement.prototype.getBoundingClientRect = function getBoundingClientRect(this: HTMLElement) {
    return rect(this.classList.contains("lc-slot") ? 64 : 0);
  };
  Object.defineProperty(HTMLElement.prototype, "clientHeight", {
    configurable: true,
    get(this: HTMLElement) {
      return this.classList.contains("lclist") ? 640 : 0;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "clientWidth", {
    configurable: true,
    get(this: HTMLElement) {
      return this.classList.contains("lclist") ? 600 : 0;
    },
  });
}
