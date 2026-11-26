// @vitest-environment jsdom
// Trạng thái gửi dưới tin (Task 175; mockup hoi-thoai.html trạng thái B; CH03, CH18, CH19; HV14 sửa theo Task 157).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { stubDialog } from "../../don/donTestkit";
import { convFake, msgId, out, renderConversation, stubConversation, writesTo } from "./conversationTestkit";

beforeAll(stubDialog);
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const bubbleOf = async (text: string) => {
  const b = await screen.findByText(text);
  const m = b.closest(".msg");
  if (!(m instanceof HTMLElement)) throw new Error("không thấy tin");
  return within(m);
};

describe("SendStatus: trạng thái gửi của tin nhân viên", () => {
  it("sáu trạng thái hiện đúng chữ; chỉ in_doubt, expired có Bỏ / Gửi lại", async () => {
    stubConversation(
      convFake({
        messages: [
          out(1, "queued", { body: "A queued" }),
          out(2, "sent", { body: "B sent" }),
          out(3, "checking", { body: "C checking" }),
          out(4, "in_doubt", { body: "D in_doubt" }),
          out(5, "expired", { body: "E expired" }),
          out(6, "failed", { body: "F failed", failReason: "blocked_by_user" }),
        ],
      }),
    );
    renderConversation();
    expect((await bubbleOf("A queued")).getByText("Đang gửi…")).toBeTruthy();
    expect((await bubbleOf("B sent")).getByText("Đã gửi")).toBeTruthy();
    expect((await bubbleOf("C checking")).getByText("Đang kiểm tra")).toBeTruthy();
    const d = await bubbleOf("D in_doubt");
    expect(d.getByText("Không rõ đã gửi chưa.")).toBeTruthy();
    expect(d.getByRole("button", { name: "Bỏ" })).toBeTruthy();
    expect(d.getByRole("button", { name: "Gửi lại" })).toBeTruthy();
    const e = await bubbleOf("E expired");
    expect(e.getByText("Chưa gửi được.")).toBeTruthy();
    expect(e.getByRole("button", { name: "Gửi lại" })).toBeTruthy();
    const f = await bubbleOf("F failed");
    expect(f.getByText("Lỗi:")).toBeTruthy();
    expect(f.getByText("Khách đã chặn tin nhắn từ Page", { exact: false })).toBeTruthy();
    expect(f.queryByRole("button", { name: "Gửi lại" })).toBeNull();
    for (const t of ["A queued", "B sent", "C checking"])
      expect((await bubbleOf(t)).queryByRole("button", { name: "Gửi lại" })).toBeNull();
  });

  it("CH18: Gửi lại ở in_doubt mở hộp xác nhận 'có thể nhận tin này hai lần' rồi gọi resendMessage", async () => {
    const f = stubConversation(convFake({ messages: [out(4, "in_doubt", { body: "Em giữ hàng tới 20:00" })] }));
    renderConversation();
    fireEvent.click((await bubbleOf("Em giữ hàng tới 20:00")).getByRole("button", { name: "Gửi lại" }));
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).getByText("có thể nhận tin này hai lần", { exact: false })).toBeTruthy();
    expect(writesTo(f, "/resend")).toHaveLength(0);
    fireEvent.click(within(dlg).getByRole("button", { name: "Vẫn gửi lại" }));
    await waitFor(() => {
      expect(writesTo(f, "/resend")).toHaveLength(1);
    });
    expect(writesTo(f, "/resend")[0]?.path).toBe(`/api/v1/messages/${msgId(4)}/resend`);
    expect(writesTo(f, "/resend")[0]?.key).not.toBeNull();
  });

  it("CH19: Gửi lại ở expired gọi resendMessage ngay; Bỏ gọi discardMessage", async () => {
    const f = stubConversation(
      convFake({ messages: [out(5, "expired", { body: "Mã vận đơn" }), out(6, "expired", { body: "Bảng size" })] }),
    );
    renderConversation();
    fireEvent.click((await bubbleOf("Mã vận đơn")).getByRole("button", { name: "Gửi lại" }));
    await waitFor(() => {
      expect(writesTo(f, "/resend")).toHaveLength(1);
    });
    fireEvent.click((await bubbleOf("Bảng size")).getByRole("button", { name: "Bỏ" }));
    await waitFor(() => {
      expect(writesTo(f, "/discard")).toHaveLength(1);
    });
    expect(writesTo(f, "/discard")[0]?.path).toBe(`/api/v1/messages/${msgId(6)}/discard`);
  });

  it("HV14 sửa: Lỗi do kênh bận (rate_limited) có Gửi lại; tin tự động đã bỏ không có nút", async () => {
    stubConversation(
      convFake({
        messages: [
          out(1, "failed", { body: "Ship nội thành", failReason: "rate_limited" }),
          out(2, "discarded", { body: "Tin chào", origin: "automation", failReason: "auto_dropped" }),
        ],
      }),
    );
    renderConversation();
    const a = await bubbleOf("Ship nội thành");
    expect(a.getByText("Facebook đang giới hạn số tin gửi", { exact: false })).toBeTruthy();
    expect(a.getByRole("button", { name: "Gửi lại" })).toBeTruthy();
    const b = await bubbleOf("Tin chào");
    expect(b.getByText("Đã bỏ, không gửi", { exact: false })).toBeTruthy();
    expect(b.queryByRole("button", { name: "Gửi lại" })).toBeNull();
  });
});
