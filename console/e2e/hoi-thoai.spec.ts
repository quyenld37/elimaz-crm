// Màn hội thoại (kế hoạch M4 Task 35, số toàn cục 175; mockup docs/mockups/m4/hoi-thoai.html) ở 1512 × 767, 390 × 844
// và 360 × 800, API giả. Hành vi (chính sách gửi, mẫu "/", trạng thái gửi, gửi lại, thao tác comment, ai đang xem) kiểm ở
// test vitest của src/screens/inbox/conversation; ở đây kiểm bố cục thật: bong bóng, tên tệp dài, tên khách 60 ký tự,
// comment 2.000 ký tự không tràn khung; trang không cuộn ngang; chụp test-results/hoi-thoai-*.png để so với
// docs/mockups/m4/png/hoi-thoai-{1512,390,360}.png.
// VIẾT SẴN, CHƯA CHẠY (chủ shop 24/11: Playwright chạy khi xong toàn bộ task màn hình M4).
import type { Page } from "@playwright/test";
import type { ChannelAccount, ChatMessage, ConversationCustomerCard, ConversationDetail, Grant } from "../src/api";
import { OWNER_GRANTS, expect, json, noHorizontalScroll, test } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000f${String(n).padStart(4, "0")}`;
const NAME_60 = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";
const FILE_NAME = "bang-do-so-do-khach-hang-dat-may-rieng-thang-11-2026-ban-cuoi-cung-da-sua-lan-3.pdf";
const PAGE_ID = id(1);
const INBOX = id(2);
const COMMENTS = id(3);

const GRANTS: Grant[] = [
  ...OWNER_GRANTS,
  ...["conversation.read", "conversation.reply"].map((permission) => ({
    permission,
    scope: { type: "all" as const, id: "" },
  })),
];

const ACCOUNT: ChannelAccount = {
  id: PAGE_ID,
  channel: "facebook",
  externalId: "256660660862565",
  name: "ELIMAZ Official",
  status: "active",
  sendMode: "enabled",
  humanAgentEnabled: false,
  subscribedFields: [],
  version: 1,
};

function detail(conv: string, kind: "inbox" | "comment", name: string): ConversationDetail {
  return {
    conversation: {
      id: conv,
      pageId: PAGE_ID,
      kind,
      status: "open",
      customer: { id: id(9), name },
      tags: [],
      unreadCount: 0,
      lastMessageAt: new Date().toISOString(),
      hasOpenDraft: false,
      version: 1,
      ...(kind === "comment"
        ? { post: { id: id(8), snippet: "Áo khoác dạ dáng dài — hàng mới về, đủ size S đến XL" } }
        : { assignee: { id: id(7), name: "Trần Minh An" } }),
    },
    page: ACCOUNT,
    sendPolicy: { allowed: true, mode: "standard", until: new Date(Date.now() + 23 * 3_600_000).toISOString() },
    humanAgentAvailable: false,
    blockedByUser: false,
    doNotContact: false,
  };
}

/** Thẻ khách của khung bên phải (Task 176); không có customer.read nên không có SĐT. */
function card(name: string): ConversationCustomerCard {
  return {
    customer: {
      id: id(9),
      rootId: id(9),
      name,
      nameSource: "channel",
      doNotContact: false,
      orderCount: 0,
      deliveredCount: 0,
      returnedCount: 0,
      returnRateBp: 0,
      totalSpent: 0,
    },
    recentOrders: [],
    openDrafts: [],
  };
}

let seq = 100;
function m(conv: string, p: Partial<ChatMessage>): ChatMessage {
  seq++;
  return {
    id: id(seq),
    conversationId: conv,
    direction: "in",
    kind: "message",
    origin: "customer",
    attachments: [],
    isHidden: false,
    removed: false,
    editedCount: 0,
    sentAt: new Date(Date.now() - (200 - seq) * 60_000).toISOString(),
    ...p,
  };
}
const staff = { direction: "out", origin: "staff", senderName: "Trần Minh An" } as const;

const INBOX_MESSAGES: ChatMessage[] = [
  m(INBOX, { origin: "history", body: "Shop ơi áo len cổ lọ còn màu kem không?" }),
  m(INBOX, {
    kind: "system",
    origin: "system",
    body: "CRM tạo hồ sơ khách tiềm năng KH005120 cho tài khoản Facebook này",
  }),
  m(INBOX, { body: "Chị ơi đầm lụa be còn size L không, chị lấy 2 cái" }),
  m(INBOX, {
    direction: "out",
    origin: "automation",
    senderName: "Tin chào",
    body: "ELIMAZ chào chị! Shop trả lời trong ít phút.",
    sendStatus: "sent",
    quickReplies: [
      { title: "Xem bảng size", payload: "size" },
      { title: "Phí ship", payload: "ship" },
    ],
  }),
  m(INBOX, { ...staff, body: "Dạ còn chị ạ. Size L còn 3 cái màu be, em giữ 2 cái cho chị nhé", sendStatus: "sent" }),
  m(INBOX, { direction: "out", origin: "page_external", body: "Chị gửi em địa chỉ nhận hàng nha", sendStatus: "sent" }),
  m(INBOX, { ...staff, kind: "note", body: "Khách quen, hay đổi size. Gửi kèm bảng size trước khi chốt." }),
  m(INBOX, { attachments: [{ fileId: id(50), type: "image", status: "stored" }] }),
  m(INBOX, { attachments: [{ fileId: id(51), type: "file", status: "stored", name: FILE_NAME }] }),
  m(INBOX, { ...staff, body: "Em giữ hàng tới 20:00 tối nay ạ", sendStatus: "in_doubt" }),
  m(INBOX, { ...staff, body: "Shop gửi mã vận đơn ạ", sendStatus: "expired" }),
  m(INBOX, { ...staff, body: "Chị ơi mẫu này về rồi ạ", sendStatus: "failed", failReason: "blocked_by_user" }),
];

const LONG = "Shop ơi cho mình hỏi mẫu áo khoác dạ này có size cho người cao 1m58 nặng 52kg không. "
  .repeat(24)
  .slice(0, 2000);
const COMMENT_MESSAGES: ChatMessage[] = [
  m(COMMENTS, {
    kind: "comment",
    senderName: "Hoàng Văn Nam",
    body: "Giá bao nhiêu vậy shop? Có màu ghi không?",
    privateReply: { state: "sent" },
  }),
  m(COMMENTS, {
    kind: "comment",
    senderName: "Bùi Thanh Tâm",
    body: "Lấy 1 cái size M, sđt 0987 654 321 nhé",
    isHidden: true,
  }),
  m(COMMENTS, { kind: "comment", senderName: "Lý Minh Châu", body: "Lấy 2 cái size L", editedCount: 1 }),
  m(COMMENTS, {
    kind: "comment",
    senderName: "Lý Minh Châu",
    body: "Ship về Đà Nẵng mất mấy ngày shop?",
    removed: true,
  }),
  m(COMMENTS, { kind: "comment", senderName: "Trịnh Ngọc Ánh", body: LONG, privateReply: { state: "none" } }),
];

async function fakeConversation(page: Page, api: { live: (me?: { grants: Grant[] }) => Promise<void> }) {
  await api.live({ grants: GRANTS });
  await page.route(/\/api\/v1\/conversations(\?|$)/, (r) => json(r, 200, { items: [] }));
  await page.route("**/api/v1/conversations/counts", (r) => json(r, 200, { byPage: [], mine: 0, unassigned: 0 }));
  await page.route("**/api/v1/channels", (r) => json(r, 200, { items: [ACCOUNT] }));
  await page.route("**/api/v1/conversation-tags", (r) => json(r, 200, { items: [] }));
  await page.route(`**/api/v1/conversations/${INBOX}`, (r) => json(r, 200, detail(INBOX, "inbox", NAME_60)));
  await page.route(`**/api/v1/conversations/${COMMENTS}`, (r) =>
    json(r, 200, detail(COMMENTS, "comment", "Hoàng Văn Nam")),
  );
  await page.route(`**/api/v1/conversations/${INBOX}/messages*`, (r) => json(r, 200, { items: INBOX_MESSAGES }));
  await page.route(`**/api/v1/conversations/${COMMENTS}/messages*`, (r) => json(r, 200, { items: COMMENT_MESSAGES }));
  // Khung bên phải (Task 176): thẻ khách, nhắc việc của tôi, cuộc gọi gần nhất.
  await page.route(`**/api/v1/conversations/${INBOX}/customer-card`, (r) => json(r, 200, card(NAME_60)));
  await page.route(`**/api/v1/conversations/${COMMENTS}/customer-card`, (r) => json(r, 200, card("Hoàng Văn Nam")));
  await page.route("**/api/v1/me/reminders", (r) => json(r, 200, { items: [] }));
  await page.route(`**/api/v1/customers/${id(9)}/calls*`, (r) => json(r, 200, { items: [] }));
  await page.route("**/api/v1/conversations/*/presence", (r) =>
    r.request().method() === "GET"
      ? json(r, 200, { items: [{ userId: id(7), displayName: "Trần Minh An", state: "viewing" }] })
      : r.fulfill({ status: 204 }),
  );
}

/** Mọi bong bóng, tệp, thẻ bài nằm trong khung dòng tin (không tràn ngang). */
async function bubblesFit(page: Page) {
  const over = await page.evaluate(() => {
    const t = document.querySelector(".thread");
    if (t === null) return -1;
    const right = t.getBoundingClientRect().right;
    const els = [...document.querySelectorAll(".bub, .att-file, .post, .sendfix, .cacts")];
    return Math.max(...els.map((el) => el.getBoundingClientRect().right - right));
  });
  expect(over).toBeLessThanOrEqual(0.5);
}

function conversationCases(vp: { width: number; height: number }) {
  test("tin nhắn đủ loại, trạng thái gửi: không tràn, không cuộn ngang", async ({ page, api }) => {
    await fakeConversation(page, api);
    await page.goto(`/inbox/${INBOX}`);
    // Tên hiện ở đầu hội thoại và ở thẻ khách của khung bên phải (Task 176).
    await expect(page.getByText(NAME_60).first()).toBeVisible();
    await expect(page.getByText(FILE_NAME)).toBeVisible();
    await expect(page.getByText("Không rõ đã gửi chưa.")).toBeVisible();
    await expect(page.getByText("Trần Minh An đang xem")).toBeVisible();
    await bubblesFit(page);
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/hoi-thoai-${String(vp.width)}.png`, fullPage: true });
  });

  test("comment: ba nút luôn hiện, 2.000 ký tự gọn 8 dòng, không tràn", async ({ page, api }) => {
    await fakeConversation(page, api);
    await page.goto(`/inbox/${COMMENTS}`);
    await expect(page.getByRole("button", { name: "Xem hết (2.000 ký tự)" })).toBeVisible();
    await expect(page.getByText("Đã nhắn riêng", { exact: false }).first()).toBeVisible();
    await expect(page.getByText("Đã ẩn trên Page")).toBeVisible();
    await bubblesFit(page);
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/hoi-thoai-comment-${String(vp.width)}.png`, fullPage: true });
  });
}

/** Khổ màn của mockup: máy hẹp 360 (bong bóng dễ tràn nhất), điện thoại 390, laptop của chủ shop 1512. */
const HEIGHT: Readonly<Record<number, number>> = { 360: 800, 390: 844, 1512: 767 };

for (const width of [360, 390, 1512]) {
  const vp = { width, height: HEIGHT[width] ?? 800 };
  test.describe(`Hội thoại ${String(width)} px`, () => {
    test.use({ viewport: vp });
    conversationCases(vp);
  });
}
