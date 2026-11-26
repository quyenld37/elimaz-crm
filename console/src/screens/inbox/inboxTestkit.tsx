// API giả và dữ liệu mẫu cho test vitest của màn Inbox (Task 174). Chỉ test nhập tệp này. Trả đúng kiểu client sinh.
import type {
  ChannelAccount,
  ConversationCounts,
  ConversationSummary,
  ConversationTag,
  Grant,
  StaffMember,
} from "../../api";
import { NOT_FOUND, emptyWrites, page, reply, stubApi, type WriteQueue } from "../../app/screenTestkit";

const pad = (n: number) => String(n).padStart(4, "0");
export const ME = "0195f3b2-0000-7000-8000-0000000000e1";
export const U2 = "0195f3b2-0000-7000-8000-0000000000e2";
export const P1 = "0195f3b2-0000-7000-8000-00000000a001";
export const P2 = "0195f3b2-0000-7000-8000-00000000a002";

/** Tên khách 60 ký tự không dấu cách (dữ liệu cứng của mockup). */
export const NAME_60 = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";

export const convId = (i: number) => `0195f3b2-0000-7000-8000-00000001${pad(i)}`;

export function conv(i: number, extra: Partial<ConversationSummary> = {}): ConversationSummary {
  return {
    id: convId(i),
    pageId: P1,
    kind: "inbox",
    status: "open",
    customer: { id: `0195f3b2-0000-7000-8000-00000002${pad(i)}`, name: `Khách ${pad(i)}` },
    tags: [],
    unreadCount: 0,
    // Mới nhất trước: i nhỏ là mới hơn.
    lastMessageAt: new Date(Date.parse("2026-11-26T03:00:00Z") - i * 60_000).toISOString(),
    lastSnippet: `Tin cuối ${pad(i)}`,
    hasOpenDraft: false,
    version: 1,
    ...extra,
  };
}

export function tag(i: number, extra: Partial<ConversationTag> = {}): ConversationTag {
  return {
    id: `0195f3b2-0000-7000-8000-00000003${pad(i)}`,
    name: `Nhãn ${String(i)}`,
    color: "soft",
    sort: i,
    version: 1,
    ...extra,
  };
}

export function account(id: string, name: string, extra: Partial<ChannelAccount> = {}): ChannelAccount {
  return {
    id,
    channel: "facebook",
    externalId: "256660660862565",
    name,
    status: "active",
    sendMode: "enabled",
    humanAgentEnabled: false,
    subscribedFields: [],
    version: 1,
    ...extra,
  };
}

export const all = (permission: string): Grant => ({ permission, scope: { type: "all", id: "" } });
export const SALE: Grant[] = [all("conversation.read"), all("conversation.reply")];

export interface InboxFake extends WriteQueue {
  grants: Grant[];
  /** Mọi hội thoại máy chủ có (đã sắp mới nhất trước); bộ lọc giả chỉ áp updatedSince, assignee=me, unread. */
  rows: ConversationSummary[];
  accounts: ChannelAccount[];
  counts: ConversationCounts;
  tags: ConversationTag[];
  staff: StaffMember[];
  /** Tham số của mỗi lần gọi listConversations. */
  listCalls: URLSearchParams[];
  countCalls: number;
  /** Có thì listConversations trả lỗi này. */
  listError?: { status: number; body: unknown };
}

export function inboxFake(p: Partial<InboxFake> = {}): InboxFake {
  return {
    ...emptyWrites(),
    grants: SALE,
    rows: [],
    accounts: [account(P1, "ELIMAZ Official"), account(P2, "ELIMAZ Sale")],
    counts: { byPage: [], mine: 0, unassigned: 0 },
    tags: [],
    staff: [],
    listCalls: [],
    countCalls: 0,
    ...p,
  };
}

function readApi(f: InboxFake, path: string, q: URLSearchParams): Response {
  switch (path) {
    case "/api/v1/me":
      return reply({ userId: ME, email: "an@elimaz.vn", grants: f.grants });
    case "/api/v1/conversations": {
      f.listCalls.push(q);
      if (f.listError !== undefined) return reply(f.listError.body, f.listError.status);
      const since = q.get("updatedSince");
      let rows = f.rows;
      if (since !== null) rows = rows.filter((r) => Date.parse(r.lastMessageAt) >= Date.parse(since) - 30_000);
      if (q.get("assignee") === "me") rows = rows.filter((r) => r.assignee?.id === ME);
      if (q.get("unread") === "true") rows = rows.filter((r) => r.unreadCount > 0);
      return reply(page(rows, q));
    }
    case "/api/v1/conversations/counts":
      f.countCalls++;
      return reply(f.counts);
    case "/api/v1/channels":
      return reply({ items: f.accounts });
    case "/api/v1/conversation-tags":
      return reply({ items: f.tags });
    case "/api/v1/identity/users":
      return reply(f.staff);
    default:
      return reply(NOT_FOUND, 404);
  }
}

/** Thay fetch bằng API giả của Inbox; màn không có yêu cầu ghi nào (ghi → 404). */
export function stubInbox(f: InboxFake): InboxFake {
  return stubApi(f, readApi, () => reply(NOT_FOUND, 404));
}
