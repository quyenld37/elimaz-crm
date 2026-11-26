// API giả và dữ liệu mẫu cho test vitest của màn hội thoại (Task 175). Chỉ test nhập tệp này. Trả đúng kiểu client sinh.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import type {
  ChatMessage,
  ConversationDetail,
  FileInfo,
  FollowUpItem,
  Grant,
  MessageRevision,
  PresenceEntry,
  ReplyTemplate,
} from "../../../api";
import { NOT_FOUND, emptyWrites, reply, stubApi, type WriteQueue } from "../../../app/screenTestkit";
import type { UploadOptions } from "../../products/useUpload";
import { ME, P1, account, all, conv } from "../inboxTestkit";
import ConversationPane from "./index";

export { ME, P1 };
export const CONV = conv(1).id;
const pad = (n: number) => String(n).padStart(4, "0");
export const msgId = (i: number) => `0195f3b2-0000-7000-8000-00000005${pad(i)}`;
export const FILE_IMG = "0195f3b2-0000-7000-8000-0000000f0001";
export const FILE_PDF = "0195f3b2-0000-7000-8000-0000000f0002";
export const TPL = "0195f3b2-0000-7000-8000-0000000e0001";

export const REPLY: Grant[] = [all("conversation.read"), all("conversation.reply")];

export function detail(extra: Partial<ConversationDetail> = {}): ConversationDetail {
  return {
    conversation: conv(1, { customer: { id: "c1", name: "Lý Minh Châu" }, unreadCount: 0 }),
    page: account(P1, "ELIMAZ Official"),
    sendPolicy: { allowed: true, mode: "standard", until: new Date(Date.now() + 23 * 3_600_000).toISOString() },
    humanAgentAvailable: false,
    blockedByUser: false,
    doNotContact: false,
    ...extra,
  };
}

/** Tin thứ i: mặc định tin khách gửi vào lúc 09:0i. */
export function msg(i: number, extra: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: msgId(i),
    conversationId: CONV,
    direction: "in",
    kind: "message",
    origin: "customer",
    body: `Tin ${String(i)}`,
    attachments: [],
    isHidden: false,
    removed: false,
    editedCount: 0,
    sentAt: `2026-11-26T02:${pad(i).slice(2)}:00Z`,
    ...extra,
  };
}

/** Tin nhân viên gửi ra với trạng thái gửi status. */
export const out = (i: number, sendStatus: ChatMessage["sendStatus"], extra: Partial<ChatMessage> = {}) =>
  msg(i, {
    direction: "out",
    origin: "staff",
    sendStatus,
    sender: { id: ME, name: "Trần Minh An" },
    senderName: "Trần Minh An",
    ...extra,
  });

export function template(shortcut: string, body: string, extra: Partial<ReplyTemplate> = {}): ReplyTemplate {
  return {
    id: TPL,
    shortcut,
    title: shortcut,
    body,
    quickReplies: [],
    version: 1,
    createdAt: "2026-11-20T00:00:00Z",
    ...extra,
  };
}

export interface ConvFake extends WriteQueue {
  grants: Grant[];
  detail: ConversationDetail;
  messages: ChatMessage[];
  /** Trang tin cũ hơn (trả khi có before); có thì trang mới nhất mang olderCursor "o1". */
  older: ChatMessage[];
  presence: PresenceEntry[];
  templates: ReplyTemplate[];
  /** Chữ renderReplyTemplate trả theo mã mẫu. */
  rendered: Record<string, string>;
  revisions: Record<string, MessageRevision[]>;
  followUps: FollowUpItem[];
  /** Câu trả lời xếp sẵn theo đuôi đường ghi (lấy lần lượt), trước câu mặc định; nhịp tim không lấy nhầm. */
  queued: Record<string, { status: number; body: unknown }[]>;
  /** Tham số của mỗi lần gọi GET theo đường. */
  reads: { path: string; q: URLSearchParams }[];
}

export function convFake(p: Partial<ConvFake> = {}): ConvFake {
  return {
    ...emptyWrites(),
    grants: REPLY,
    detail: detail(),
    messages: [],
    older: [],
    presence: [],
    templates: [],
    rendered: {},
    revisions: {},
    followUps: [],
    queued: {},
    reads: [],
    ...p,
  };
}

function readApi(f: ConvFake, path: string, q: URLSearchParams): Response {
  f.reads.push({ path, q });
  const base = `/api/v1/conversations/${CONV}`;
  if (path === "/api/v1/me") return reply({ userId: ME, email: "an@elimaz.vn", grants: f.grants });
  if (path === base) return reply(f.detail);
  if (path === `${base}/messages`) {
    if (q.has("before")) return reply({ items: f.older, newerCursor: "n" });
    return reply({ items: f.messages, newerCursor: "n", ...(f.older.length > 0 ? { olderCursor: "o1" } : {}) });
  }
  if (path === `${base}/presence`) return reply({ items: f.presence });
  if (path === "/api/v1/reply-templates") {
    const s = q.get("q") ?? "";
    return reply({ items: f.templates.filter((t) => t.shortcut.startsWith(s)) });
  }
  if (path === "/api/v1/follow-ups") return reply({ items: f.followUps });
  const att = /^\/api\/v1\/conversations\/[^/]+\/attachments\/([^/]+)\/url$/.exec(path);
  if (att !== null) return reply({ url: `https://kho.example/${att[1] ?? ""}`, expiresAt: "2026-11-26T03:05:00Z" });
  const rev = /^\/api\/v1\/messages\/([^/]+)\/revisions$/.exec(path);
  if (rev !== null) return reply({ items: f.revisions[rev[1] ?? ""] ?? [] });
  const file = /^\/api\/v1\/files\/([^/]+)$/.exec(path);
  if (file !== null) return reply(fileInfo(file[1] ?? ""));
  return reply(NOT_FOUND, 404);
}

const fileInfo = (id: string): FileInfo => ({ id, purpose: "chat_outgoing", status: "ready", variants: [] });

function writeApi(f: ConvFake, path: string, body: Record<string, unknown>): Response {
  const suffix = Object.keys(f.queued).find((k) => path.endsWith(k) && (f.queued[k]?.length ?? 0) > 0);
  const next = suffix === undefined ? undefined : f.queued[suffix]?.shift();
  if (next !== undefined) return reply(next.body, next.status);
  if (path.endsWith("/messages") || path.endsWith("/notes") || path.endsWith("-replies")) {
    const text = typeof body.text === "string" ? body.text : "";
    return reply(out(99, "queued", { body: text }), 201);
  }
  const tpl = /^\/api\/v1\/reply-templates\/([^/]+)\/render$/.exec(path);
  if (tpl !== null) return reply({ text: f.rendered[tpl[1] ?? ""] ?? "", quickReplies: [] });
  if (path.endsWith("/resend")) return reply({ message: out(98, "queued"), mayDuplicate: true }, 201);
  if (path.endsWith("/presence") || path.endsWith("/read")) return new Response(null, { status: 204 });
  if (path === "/api/v1/files/uploads")
    return reply(
      { fileId: FILE_IMG, putUrl: "https://kho.example/put", headers: {}, expiresAt: "2026-11-26T03:00:00Z" },
      201,
    );
  if (path.endsWith("/complete")) return reply(fileInfo(FILE_IMG));
  if (path.endsWith("/status") || path.endsWith("/claim")) return reply(f.detail);
  return reply(msg(1), 200);
}

export const stubConversation = (f: ConvFake): ConvFake => stubApi(f, readApi, writeApi);

/** PUT giả lên kho tệp (ảnh gửi khách). */
export const UPLOAD_OPTS: UploadOptions = {
  put: () => Promise.resolve(),
  sleep: () => Promise.resolve(),
};

/** Dựng khung hội thoại CONV ở /inbox/CONV. */
export function renderConversation(uploadOptions: UploadOptions = UPLOAD_OPTS) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      {
        path: "/inbox/:conversationId",
        element: <ConversationPane conversationId={CONV} backTo="/inbox" uploadOptions={uploadOptions} />,
      },
    ],
    { initialEntries: [`/inbox/${CONV}`] },
  );
  const view = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { qc, ...view };
}

/** Yêu cầu ghi có đường kết thúc bằng suffix. */
export const writesTo = (f: ConvFake, suffix: string) => f.writes.filter((w) => w.path.endsWith(suffix));

/** Ô nhập, nút đang khóa. */
export const isDisabled = (el: Element) => el.matches(":disabled");

/** Chữ trong ô nhập (textarea, input); phần tử khác → "". */
export const valueOf = (el: Element) =>
  el instanceof HTMLTextAreaElement || el instanceof HTMLInputElement ? el.value : "";
