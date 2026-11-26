// @vitest-environment jsdom
// Kế hoạch M4 Task 177 (LV05, LV06 phía màn hình; HV49–HV51 kèm sửa HV49): bảng đơn nháp thiếu địa chỉ xếp hết hạn gần
// nhất trước; "Nhắc tất cả" → kết quả 2 đã gửi, 1 bỏ qua kèm lý do; "Nhắc" từng dòng; dòng không nhắc được hiện lý do
// thật; khách đã trả lời → mở hội thoại để hoàn tất; Page chưa chọn mẫu → khóa; 0 đơn → trạng thái rỗng.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ConversationSummary } from "../../api";
import { LONG_NAME, PAGE, S_LIVE, TPL, fakeLayout, installLiveFake, pending, renderLive } from "./liveTestkit";

beforeEach(() => {
  fakeLayout();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const D1 = pending(1, { customer: { id: "k1", name: LONG_NAME } });
const D2 = pending(2, { ask: { status: "skipped_policy", reason: "Page đang ở chế độ chỉ xem" } });
const D3 = pending(3, { canRemind: { allowed: false, reason: "Đã nhắc lúc 20:40" } });
const D0 = pending(0, { customerRepliedAfterAsk: true, customer: { id: "k0", name: "Phạm Ngọc Anh" } });

const conv = (p: Partial<ConversationSummary>): ConversationSummary => ({
  id: "0195f3b2-0000-7000-8000-0000000c0001",
  pageId: PAGE,
  kind: "inbox",
  status: "open",
  customer: { id: "k0", name: "Phạm Ngọc Anh" },
  tags: [],
  unreadCount: 0,
  lastMessageAt: "2026-11-24T13:09:00Z",
  hasOpenDraft: true,
  version: 1,
  ...p,
});

const remindWrites = (f: ReturnType<typeof installLiveFake>) => f.writes.filter((w) => w.path.endsWith("/remind"));

describe("PendingDraftsPage", () => {
  it("bảng xếp hết hạn gần nhất trước; số còn nháp, đã xác nhận, hết hạn; hộp đơn quá hạn đã tự hủy", async () => {
    installLiveFake({ drafts: [D3, D1, D2], confirmedCount: 12, expiredCount: 3 });
    renderLive(`/live/${S_LIVE}/thieu-dia-chi`);
    const table = await screen.findByRole("table");
    const codes = within(table)
      .getAllByText(/^DH000\d+ · chốt/)
      .map((e) => e.textContent.slice(0, 9));
    expect(codes).toEqual(["DH0001241", "DH0001242", "DH0001243"]);
    expect(screen.getByText("3 đơn nháp quá hạn đã tự hủy")).toBeTruthy();
    expect(screen.getByText("12")).toBeTruthy();
    expect(within(table).getByText("Chưa xin được")).toBeTruthy();
    expect(within(table).getByText("Page đang ở chế độ chỉ xem")).toBeTruthy();
    // Sửa HV49: lý do thật của máy chủ, nút khóa.
    expect(within(table).getByText("Đã nhắc lúc 20:40")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Nhắc DH0001243" })).toHaveProperty("disabled", true);
    expect(screen.getByText(LONG_NAME).className).toContain("ellip");
  });

  it("'Nhắc tất cả' → thân chỉ có templateId của Page; kết quả 2 đã xếp, 1 bỏ qua kèm lý do", async () => {
    const fake = installLiveFake({
      drafts: [D1, D2, D3],
      remindOutcomes: { [D3.orderId]: { orderId: D3.orderId, outcome: "skipped", reason: "Đã nhắc lúc 20:40" } },
    });
    renderLive(`/live/${S_LIVE}/thieu-dia-chi`);
    const all = await screen.findByRole("button", { name: "Nhắc tất cả (3)" });
    await waitFor(() => {
      expect(all).toHaveProperty("disabled", false);
    });
    fireEvent.click(all);
    await screen.findByText("Đã xếp 2 tin nhắc");
    expect(screen.getByText(/bỏ qua 1/)).toBeTruthy();
    expect(screen.getByText(`${D3.orderCode} · ${D3.customer.name}`)).toBeTruthy();
    const w = remindWrites(fake);
    expect(w).toHaveLength(1);
    expect(w[0]?.body).toEqual({ templateId: TPL });
    expect(w[0]?.key).toBeTruthy();
  });

  it("'Nhắc' một dòng → orderIds đúng một đơn; kết quả hiện ngay ở dòng", async () => {
    const fake = installLiveFake({ drafts: [D1, D2] });
    renderLive(`/live/${S_LIVE}/thieu-dia-chi`);
    const b = await screen.findByRole("button", { name: `Nhắc ${D2.orderCode}` });
    await waitFor(() => {
      expect(b).toHaveProperty("disabled", false);
    });
    fireEvent.click(b);
    await screen.findByText("Đã xếp tin nhắc");
    expect(remindWrites(fake)[0]?.body).toEqual({ orderIds: [D2.orderId], templateId: TPL });
  });

  it("Page chưa chọn mẫu xin địa chỉ → 'Nhắc tất cả' và 'Nhắc' khóa kèm lý do", async () => {
    installLiveFake({ drafts: [D1], settings: {} });
    renderLive(`/live/${S_LIVE}/thieu-dia-chi`);
    await screen.findAllByText("Page chưa chọn mẫu xin địa chỉ (Cài đặt hộp chat).");
    expect(screen.getByRole("button", { name: "Nhắc tất cả (1)" })).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: `Nhắc ${D1.orderCode}` })).toHaveProperty("disabled", true);
  });

  it("khách đã trả lời → 'Mở hội thoại để hoàn tất' mở hội thoại hộp thư của khách trên Page của phiên", async () => {
    installLiveFake({
      drafts: [D0],
      conversations: [
        conv({ id: "0195f3b2-0000-7000-8000-0000000c0009", kind: "comment" }),
        conv({ pageId: "khac" }),
        conv({}),
      ],
    });
    renderLive(`/live/${S_LIVE}/thieu-dia-chi`);
    fireEvent.click(await screen.findByRole("button", { name: "Mở hội thoại để hoàn tất" }));
    await screen.findByText("Trang hội thoại 0195f3b2-0000-7000-8000-0000000c0001");
  });

  it("khách chưa có hội thoại trên Page → báo tại dòng", async () => {
    installLiveFake({ drafts: [D0], conversations: [conv({ pageId: "khac" })] });
    renderLive(`/live/${S_LIVE}/thieu-dia-chi`);
    fireEvent.click(await screen.findByRole("button", { name: "Mở hội thoại để hoàn tất" }));
    await screen.findByText("Khách chưa có hội thoại trên Page này.");
  });

  it("lọc 'Khách đã trả lời' chỉ còn dòng đã trả lời", async () => {
    installLiveFake({ drafts: [D0, D1] });
    renderLive(`/live/${S_LIVE}/thieu-dia-chi`);
    await screen.findByText(LONG_NAME);
    fireEvent.click(screen.getByRole("button", { name: "Khách đã trả lời" }));
    expect(screen.queryByText(LONG_NAME)).toBeNull();
    expect(screen.getByText("Phạm Ngọc Anh")).toBeTruthy();
  });

  it("0 đơn nháp → 'Không còn đơn nào thiếu địa chỉ' kèm số của phiên; /live/thieu-dia-chi chọn phiên mới nhất", async () => {
    installLiveFake({ drafts: [], confirmedCount: 27 });
    renderLive("/live/thieu-dia-chi");
    await screen.findByText("Không còn đơn nào thiếu địa chỉ");
    expect(screen.getByText('Phiên "Xả hàng đầm lụa tối thứ Ba": 27 đơn đã xác nhận, 0 hết hạn.')).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Nhắc tất cả/ })).toBeNull();
  });
});
