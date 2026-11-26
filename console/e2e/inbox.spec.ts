// Màn Hộp chat (kế hoạch M4 Task 34, số toàn cục 174; mockup docs/mockups/m4/inbox.html) ở 1512 × 767, 390 × 844 và
// 360 × 800, API giả. Hành vi (báo hiệu, gộp, bộ lọc trên URL, băng, âm báo) kiểm ở test vitest của src/screens/inbox;
// ở đây kiểm bố cục thật: tên khách 60 ký tự liền không làm dòng tràn khung, trang không cuộn ngang, điện thoại mỗi lúc
// một màn (HV02); chụp test-results/inbox-*.png để so với docs/mockups/m4/png/inbox-{1512,390,360}.png.
// VIẾT SẴN, CHƯA CHẠY (chủ shop 24/11: Playwright chạy khi xong toàn bộ task màn hình M4).
import type { Page } from "@playwright/test";
import type {
  ChannelAccount,
  ChatMessage,
  ConversationCounts,
  ConversationCustomerCard,
  ConversationDetail,
  ConversationSummary,
  ConversationTag,
  Grant,
} from "../src/api";
import { OWNER_GRANTS, expect, json, noHorizontalScroll, test } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000e${String(n).padStart(4, "0")}`;
const NAME_60 = "NguyễnThịThuHàShopThờiTrangCôngSởXuấtKhẩuHàNộiChiNhánhSố0205";
const OFFICIAL = id(1);
const SALE = id(2);

const GRANTS: Grant[] = [
  ...OWNER_GRANTS,
  ...["conversation.read", "conversation.reply", "channel.manage", "live.operate", "conversation.settings"].map(
    (permission) => ({ permission, scope: { type: "all" as const, id: "" } }),
  ),
];

const TAGS: ConversationTag[] = ["Hỏi giá", "Khách quen", "Khách sỉ", "Hỏi size", "Đã lên đơn"].map((name, i) => ({
  id: id(100 + i),
  name,
  color: ["soft", "success", "edge", "surface", "soft"][i] ?? "soft",
  sort: i,
  version: 1,
}));

const ACCOUNTS: ChannelAccount[] = [
  {
    id: OFFICIAL,
    channel: "facebook",
    externalId: "256660660862565",
    name: "ELIMAZ Official",
    status: "degraded",
    statusReason: "token_invalid",
    sendMode: "enabled",
    humanAgentEnabled: false,
    subscribedFields: [],
    version: 1,
  },
  {
    id: SALE,
    channel: "facebook",
    externalId: "100000000000002",
    name: "ELIMAZ Sale",
    status: "active",
    sendMode: "read_only",
    humanAgentEnabled: false,
    subscribedFields: [],
    version: 1,
  },
];

const COUNTS: ConversationCounts = {
  byPage: [
    { pageId: OFFICIAL, unread: 120, open: 150 },
    { pageId: SALE, unread: 9, open: 50 },
  ],
  mine: 3,
  unassigned: 12,
};

function row(n: number, p: Partial<ConversationSummary> = {}): ConversationSummary {
  return {
    id: id(200 + n),
    pageId: OFFICIAL,
    kind: "inbox",
    status: "open",
    customer: { id: id(400 + n), name: `Khách thứ ${String(n)}` },
    tags: [],
    unreadCount: 0,
    lastMessageAt: new Date(Date.now() - n * 60_000).toISOString(),
    lastSnippet: "Chị ơi váy này còn size M không ạ?",
    hasOpenDraft: false,
    version: 1,
    ...p,
  };
}

const ROWS: ConversationSummary[] = [
  row(1, { customer: { id: id(401), name: "Phạm Thu Trang" }, pageId: SALE, unreadCount: 1, tags: TAGS.slice(3, 4) }),
  row(2, {
    customer: { id: id(402), name: NAME_60 },
    kind: "comment",
    unreadCount: 120,
    lastSnippet: "Lấy em 2 cái màu be size L, ship Hà Đông nhé shop",
    post: { id: "p1", snippet: "Đầm lụa thu đông 2026 — bộ sưu tập mới về, giá ưu đãi tuần đầu" },
    tags: TAGS,
    assignee: { id: id(9), name: "Trần Minh An" },
  }),
  row(3, { customer: { id: id(403), name: "Lê Hà My" }, unreadCount: 9, tags: TAGS.slice(2, 5) }),
  ...Array.from({ length: 47 }, (_, i) => row(i + 4)),
];

/** Hội thoại của dòng 3 (Lê Hà My, 9 chưa đọc) mở ở ca "mở hội thoại". */
const OPEN = ROWS[2] ?? row(3);
const OPEN_DETAIL: ConversationDetail = {
  conversation: OPEN,
  page: ACCOUNTS[0] ?? ({} as ChannelAccount),
  sendPolicy: { allowed: true, mode: "standard", until: new Date(Date.now() + 23 * 3_600_000).toISOString() },
  humanAgentAvailable: false,
  blockedByUser: false,
  doNotContact: false,
};
const OPEN_CARD: ConversationCustomerCard = {
  customer: {
    id: OPEN.customer.id,
    rootId: OPEN.customer.id,
    name: OPEN.customer.name,
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
const OPEN_MESSAGES: ChatMessage[] = ["Chị ơi đầm lụa be còn size L không ạ?", "Chị lấy 2 cái nhé"].map((body, i) => ({
  id: id(300 + i),
  conversationId: OPEN.id,
  direction: "in",
  kind: "message",
  origin: "customer",
  body,
  attachments: [],
  isHidden: false,
  removed: false,
  editedCount: 0,
  sentAt: new Date(Date.now() - (5 - i) * 60_000).toISOString(),
}));

async function fakeInbox(page: Page, api: { live: (me?: { grants: Grant[] }) => Promise<void> }) {
  await api.live({ grants: GRANTS });
  await page.route(/\/api\/v1\/conversations(\?|$)/, (r) => json(r, 200, { items: ROWS, nextCursor: "50" }));
  await page.route("**/api/v1/conversations/counts", (r) => json(r, 200, COUNTS));
  await page.route("**/api/v1/channels", (r) => json(r, 200, { items: ACCOUNTS }));
  await page.route("**/api/v1/conversation-tags", (r) => json(r, 200, { items: TAGS }));
  // Hội thoại đang mở (/inbox/:id, khung giữa của Task 175): chi tiết, tin, ai đang xem, đánh dấu đã đọc.
  await page.route(`**/api/v1/conversations/${OPEN.id}`, (r) => json(r, 200, OPEN_DETAIL));
  await page.route(`**/api/v1/conversations/${OPEN.id}/messages*`, (r) => json(r, 200, { items: OPEN_MESSAGES }));
  await page.route(`**/api/v1/conversations/${OPEN.id}/read`, (r) => r.fulfill({ status: 204 }));
  await page.route(`**/api/v1/conversations/${OPEN.id}/presence`, (r) =>
    r.request().method() === "GET" ? json(r, 200, { items: [] }) : r.fulfill({ status: 204 }),
  );
  // Khung bên phải (Task 176, dưới 1200 px nằm dưới hội thoại): thẻ khách và các phần nạp sau thẻ.
  await page.route(`**/api/v1/conversations/${OPEN.id}/customer-card`, (r) => json(r, 200, OPEN_CARD));
  await page.route(`**/api/v1/conversations/${OPEN.id}/order-context`, (r) =>
    json(r, 200, {
      channelAccountId: OPEN.pageId,
      conversationId: OPEN.id,
      customerId: OPEN.customer.id,
      sellerId: OPEN.pageId,
    }),
  );
  await page.route(`**/api/v1/customers/${OPEN.customer.id}/calls*`, (r) => json(r, 200, { items: [] }));
  await page.route(`**/api/v1/channels/${OPEN.pageId}/assignable-users`, (r) =>
    json(r, 200, { items: [{ id: id(9), name: "Trần Minh An" }] }),
  );
  await page.route("**/api/v1/me/reminders*", (r) => json(r, 200, { items: [] }));
}

/** Mọi dòng hội thoại nằm trong khung danh sách (không tràn ngang). */
async function rowsFit(page: Page) {
  const over = await page.evaluate(() => {
    const list = document.querySelector(".cvlist");
    if (list === null) return -1;
    const right = list.getBoundingClientRect().right;
    return Math.max(...[...document.querySelectorAll(".cv")].map((el) => el.getBoundingClientRect().right - right));
  });
  expect(over).toBeLessThanOrEqual(0.5);
}

/** Các ca của một khổ màn (máy tính 1512, điện thoại 390, máy hẹp 360 nơi dòng dễ tràn nhất). */
function inboxCases(vp: { width: number; height: number }) {
  test("danh sách: tên 60 ký tự không tràn, không cuộn ngang, băng Page, 99+", async ({ page, api }) => {
    await fakeInbox(page, api);
    await page.goto("/inbox");
    await expect(page.getByText(NAME_60)).toBeVisible();
    await expect(page.getByText("Page ELIMAZ Official mất kết nối, cần quản trị kết nối lại.")).toBeVisible();
    await expect(page.getByText("Page ELIMAZ Sale đang ở chế độ chỉ xem.")).toBeVisible();
    await expect(page.getByLabel("120 chưa đọc").first()).toHaveText("99+");
    await rowsFit(page);
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/inbox-${String(vp.width)}.png`, fullPage: true });
  });

  test("mở hội thoại: máy tính ba cột, điện thoại chỉ hội thoại có nút quay lại", async ({ page, api }) => {
    await fakeInbox(page, api);
    await page.goto(`/inbox/${id(203)}`);
    const back = page.getByRole("link", { name: "Về danh sách hội thoại" });
    if (vp.width < 1000) {
      await expect(page.locator(".pane-list")).toBeHidden();
      await expect(back).toBeVisible();
      await back.click();
      await expect(page.locator(".pane-list")).toBeVisible();
    } else {
      await expect(page.locator(".pane-list")).toBeVisible();
      await expect(page.locator('.cv[aria-current="true"]')).toContainText("Lê Hà My");
    }
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/inbox-hoi-thoai-${String(vp.width)}.png`, fullPage: true });
  });
}

const SIZES = [
  { width: 360, height: 800 },
  { width: 390, height: 844 },
  { width: 1512, height: 767 },
] as const;

for (const vp of SIZES) {
  test.describe(`Hộp chat ${String(vp.width)} px`, () => {
    test.use({ viewport: vp });
    inboxCases(vp);
  });
}
