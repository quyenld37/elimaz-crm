// API giả cho test của màn Kênh kết nối và Cài đặt hộp chat (Task 178). Chỉ test nhập tệp này.
import type {
  AutomationRule,
  ChannelAccount,
  ConversationTag,
  Grant,
  OrderSettings,
  PageSettings,
  ReplyTemplate,
  TrafficSource,
} from "../../api";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";
import { NOT_FOUND, emptyWrites, meReply, reply, stubApi, type WriteQueue } from "../../app/screenTestkit";

export const P1 = "0195f3b2-0000-7000-8000-0000000c0001";
export const P2 = "0195f3b2-0000-7000-8000-0000000c0002";
export const SRC1 = "0195f3b2-0000-7000-8000-0000000d0001";
export const SRC2 = "0195f3b2-0000-7000-8000-0000000d0002";
export const T1 = "0195f3b2-0000-7000-8000-0000000e0001";
export const U1 = "0195f3b2-0000-7000-8000-0000000f0001";

export const grant = (permission: string, scope: Grant["scope"] = { type: "all", id: "" }): Grant => ({
  permission,
  scope,
});
export const MANAGER: Grant[] = ["conversation.read", "conversation.settings", "channel.manage", "order.read"].map(
  (p) => grant(p),
);

export const account = (extra: Partial<ChannelAccount> = {}): ChannelAccount => ({
  id: P1,
  channel: "facebook",
  externalId: "256660660862565",
  name: "ELIMAZ",
  status: "active",
  sendMode: "read_only",
  humanAgentEnabled: false,
  subscribedFields: Array.from({ length: 11 }, (_, i) => `f${String(i)}`),
  tokenCheckedAt: "2026-11-26T13:00:00Z",
  version: 3,
  features: {
    messages: true,
    comments: true,
    liveComments: true,
    privateReply: true,
    hideComment: true,
    quickReplies: true,
    humanAgent: true,
  },
  ...extra,
});

export const settingsOf = (extra: Partial<PageSettings> = {}): PageSettings => ({
  channelAccountId: P1,
  roundRobinEnabled: false,
  roundRobinUserIds: [],
  liveSlots: [{ from: "19:00", to: "23:00", trafficSourceId: SRC1 }],
  commentPollMode: "auto",
  automationMaxAgeMinutes: 15,
  version: 2,
  ...extra,
});

export const rules = (): AutomationRule[] => [
  {
    kind: "auto_hide",
    enabled: true,
    config: { kind: "auto_hide", mode: "phone_only", exceptPostIds: [] },
    version: 4,
  },
  {
    kind: "auto_reply_comment",
    enabled: true,
    config: {
      kind: "auto_reply_comment",
      replies: ["Shop đã inbox, bạn check tin nhắn nhé ạ", "Dạ shop nhắn riêng bạn rồi ạ"],
      postIds: [],
      exceptPostIds: [],
      includeLive: false,
      perUserPerPostHours: 24,
    },
    version: 2,
  },
  {
    kind: "greeting",
    enabled: true,
    config: {
      kind: "greeting",
      text: "Chào bạn, ELIMAZ đây ạ!",
      quickReplies: [
        { title: "Tư vấn size", payload: "Tư vấn size" },
        { title: "Xem bảng giá", payload: "Xem bảng giá" },
      ],
    },
    version: 5,
  },
  {
    kind: "follow_up",
    enabled: false,
    config: { kind: "follow_up", afterHours: 4, templateId: "00000000-0000-0000-0000-000000000000" },
    version: 0,
  },
];

export interface KenhFake extends WriteQueue {
  grants: Grant[];
  channels: ChannelAccount[];
  settings: PageSettings;
  rules: AutomationRule[];
  tags: ConversationTag[];
  templates: ReplyTemplate[];
  sources: TrafficSource[];
  orderSettings: OrderSettings;
  /** GET đã nhận (đường + tham số), theo thứ tự. */
  reads: string[];
}

export function newFake(): KenhFake {
  return {
    ...emptyWrites(),
    grants: MANAGER,
    channels: [account()],
    settings: settingsOf(),
    rules: rules(),
    tags: [
      { id: "tag1", name: "Đã lên đơn", color: "success", sort: 0, version: 1 },
      { id: "tag2", name: "Hỏi size", color: "soft", sort: 1, version: 1 },
    ],
    templates: [
      {
        id: T1,
        shortcut: "xin-dia-chi",
        title: "Xin địa chỉ live",
        body: "Chào {ten_khach}",
        quickReplies: [],
        version: 1,
        createdAt: "2026-11-20T00:00:00Z",
      },
    ],
    sources: [
      { id: SRC1, name: "Live tối", active: true, position: 0, version: 1 },
      { id: SRC2, name: "Facebook inbox", active: true, position: 1, version: 1 },
    ],
    orderSettings: {
      draftTtlMinutes: 1440,
      duplicateWindowMinutes: 1440,
      discountApprovalThresholdBp: 1000,
      returnWarningDays: 7,
      version: 1,
    },
    reads: [],
  };
}

/** Thay fetch bằng API giả của màn; yêu cầu ghi không có câu xếp sẵn trả bản đã đổi tối thiểu. */
export function stubKenh(fake: KenhFake = newFake()): KenhFake {
  return stubApi(
    fake,
    (f, path, q) => {
      f.reads.push(q.size > 0 ? `${path}?${q.toString()}` : path);
      if (path === "/api/v1/me") return meReply(f.grants);
      if (path === "/api/v1/channels") return reply({ items: f.channels });
      if (path.endsWith("/page-settings")) return reply(f.settings);
      if (path.endsWith("/automation-rules")) return reply({ items: f.rules });
      if (path.endsWith("/assignable-users")) return reply({ items: [{ id: U1, name: "Trần Minh An" }] });
      if (path === "/api/v1/conversation-tags") return reply({ items: f.tags });
      if (path === "/api/v1/reply-templates") return reply({ items: f.templates });
      if (path === "/api/v1/traffic-sources") return reply({ items: f.sources });
      if (path === "/api/v1/order-settings") return reply(f.orderSettings);
      return reply(NOT_FOUND, 404);
    },
    (f, path, body) => {
      if (path.endsWith("/send-mode")) {
        const a = f.channels[0] ?? account();
        return reply({ ...a, sendMode: body.mode, version: a.version + 1 });
      }
      if (path.endsWith("/page-settings")) return reply({ ...f.settings, ...body, version: f.settings.version + 1 });
      if (path.includes("/automation-rules/")) {
        const kind = path.split("/").at(-1);
        const r = f.rules.find((x) => x.kind === kind);
        return reply({ ...r, ...body, version: (r?.version ?? 0) + 1 });
      }
      if (path === "/api/v1/reply-templates") return reply({ ...f.templates[0], ...body, id: "new" }, 201);
      if (path === "/api/v1/conversation-tags") return reply({ id: "new", sort: 9, version: 1, ...body }, 201);
      return reply(f.channels[0] ?? account());
    },
  );
}

import { stubDialog } from "../don/donTestkit";

export { stubDialog };

/** beforeEach/afterEach chung: hộp thoại giả và API giả mới cho mỗi test; t.fake là API giả của test đang chạy. */
export function setupKenh(): { fake: KenhFake } {
  const t = { fake: newFake() };
  beforeEach(() => {
    stubDialog();
    t.fake = stubKenh(newFake());
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });
  return t;
}
