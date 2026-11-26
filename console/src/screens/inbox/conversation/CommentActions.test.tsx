// @vitest-environment jsdom
// Thao tác comment (Task 175; mockup hoi-thoai.html trạng thái C; CH05, CH11; HV19, HV20).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ChatMessage } from "../../../api";
import { P1, account, conv } from "../inboxTestkit";
import {
  convFake,
  detail,
  isDisabled,
  msg,
  msgId,
  renderConversation,
  stubConversation,
  writesTo,
} from "./conversationTestkit";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const commentConv = detail({ conversation: conv(1, { kind: "comment" }) });
const comment = (i: number, extra: Partial<ChatMessage> = {}) =>
  msg(i, {
    kind: "comment",
    senderName: "Hoàng Văn Nam",
    sentAt: new Date(Date.now() - 3_600_000).toISOString(),
    privateReply: { state: "none" },
    ...extra,
  });

const actsOf = async (text: string) => {
  const b = await screen.findByText(text);
  const m = b.closest(".msg");
  if (!(m instanceof HTMLElement)) throw new Error("không thấy comment");
  return within(m);
};

describe("CommentActions", () => {
  it("ba nút luôn hiện; Trả lời công khai gửi replyToComment kèm commentMessageId", async () => {
    const f = stubConversation(
      convFake({ detail: commentConv, messages: [comment(1, { body: "Giá bao nhiêu shop?" })] }),
    );
    renderConversation();
    const c = await actsOf("Giá bao nhiêu shop?");
    expect(isDisabled(c.getByRole("button", { name: "Nhắn riêng" }))).toBe(false);
    expect(isDisabled(c.getByRole("button", { name: "Ẩn trên Page" }))).toBe(false);
    fireEvent.click(c.getByRole("button", { name: "Trả lời công khai" }));
    fireEvent.change(c.getByRole("textbox", { name: "Trả lời công khai" }), { target: { value: "Shop đã inbox ạ" } });
    fireEvent.click(c.getByRole("button", { name: "Gửi trả lời" }));
    await waitFor(() => {
      expect(writesTo(f, "/comment-replies")).toHaveLength(1);
    });
    expect(writesTo(f, "/comment-replies")[0]?.body).toEqual({ commentMessageId: msgId(1), text: "Shop đã inbox ạ" });
  });

  it("Nhắn riêng gửi sendPrivateReply kèm commentMessageId", async () => {
    const f = stubConversation(
      convFake({ detail: commentConv, messages: [comment(1, { body: "Có màu ghi không?" })] }),
    );
    renderConversation();
    const c = await actsOf("Có màu ghi không?");
    fireEvent.click(c.getByRole("button", { name: "Nhắn riêng" }));
    fireEvent.change(c.getByRole("textbox", { name: "Nhắn riêng" }), { target: { value: "Áo 1.290.000đ ạ" } });
    fireEvent.click(c.getByRole("button", { name: "Gửi tin riêng" }));
    await waitFor(() => {
      expect(writesTo(f, "/private-replies")).toHaveLength(1);
    });
    expect(writesTo(f, "/private-replies")[0]?.body).toEqual({ commentMessageId: msgId(1), text: "Áo 1.290.000đ ạ" });
  });

  it("CH05: đã nhắn riêng → nút khóa, chữ 'Đã nhắn riêng' hiện thẳng", async () => {
    stubConversation(
      convFake({
        detail: commentConv,
        messages: [comment(1, { body: "Size M?", privateReply: { state: "sent", messageId: msgId(9) } })],
      }),
    );
    renderConversation();
    const c = await actsOf("Size M?");
    expect(isDisabled(c.getByRole("button", { name: "Nhắn riêng" }))).toBe(true);
    expect(c.getByText("Đã nhắn riêng", { exact: false })).toBeTruthy();
  });

  it("comment quá 7 ngày → nút nhắn riêng khóa, chữ 'Quá 7 ngày'", async () => {
    stubConversation(
      convFake({
        detail: commentConv,
        messages: [comment(1, { body: "Có COD không", sentAt: new Date(Date.now() - 8 * 86_400_000).toISOString() })],
      }),
    );
    renderConversation();
    const c = await actsOf("Có COD không");
    expect(isDisabled(c.getByRole("button", { name: "Nhắn riêng" }))).toBe(true);
    expect(c.getByText("Quá 7 ngày", { exact: false })).toBeTruthy();
  });

  it("CH11: comment bị ẩn → nhãn 'Đã ẩn trên Page', 'Hiện lại trên Page' gọi unhideComment; Ẩn gọi hideComment", async () => {
    const f = stubConversation(
      convFake({
        detail: commentConv,
        messages: [comment(1, { body: "sđt 0987 654 321", isHidden: true }), comment(2, { body: "Còn size S" })],
      }),
    );
    renderConversation();
    const a = await actsOf("sđt 0987 654 321");
    expect(a.getByText("Đã ẩn trên Page")).toBeTruthy();
    fireEvent.click(a.getByRole("button", { name: "Hiện lại trên Page" }));
    await waitFor(() => {
      expect(writesTo(f, "/unhide")).toHaveLength(1);
    });
    expect(writesTo(f, "/unhide")[0]?.path).toBe(`/api/v1/messages/${msgId(1)}/unhide`);
    fireEvent.click((await actsOf("Còn size S")).getByRole("button", { name: "Ẩn trên Page" }));
    await waitFor(() => {
      expect(writesTo(f, "/hide")).toHaveLength(1);
    });
  });

  it("trả lời công khai của shop hiện lồng dưới comment", async () => {
    stubConversation(
      convFake({
        detail: commentConv,
        messages: [
          comment(1, { body: "Giá?" }),
          msg(2, {
            kind: "comment",
            direction: "out",
            origin: "staff",
            body: "Shop đã nhắn riêng ạ",
            parentMessageId: msgId(1),
            sendStatus: "sent",
            senderName: "Nguyễn Thị Lan",
          }),
        ],
      }),
    );
    renderConversation();
    const r = (await screen.findByText("Shop đã nhắn riêng ạ")).closest(".reply-in");
    expect(r).not.toBeNull();
    expect(within(r as HTMLElement).getByText("Trả lời công khai · Nguyễn Thị Lan")).toBeTruthy();
  });

  it("Page không hỗ trợ nhắn riêng, ẩn (tính năng kênh) → không có hai nút đó", async () => {
    const page = account(P1, "ELIMAZ Official", {
      features: {
        messages: true,
        comments: true,
        liveComments: false,
        privateReply: false,
        hideComment: false,
        quickReplies: false,
        humanAgent: false,
      },
    });
    stubConversation(convFake({ detail: { ...commentConv, page }, messages: [comment(1, { body: "Hỏi giá" })] }));
    renderConversation();
    const c = await actsOf("Hỏi giá");
    expect(c.getByRole("button", { name: "Trả lời công khai" })).toBeTruthy();
    expect(c.queryByRole("button", { name: "Nhắn riêng" })).toBeNull();
    expect(c.queryByRole("button", { name: "Ẩn trên Page" })).toBeNull();
  });

  it("hội thoại comment: ô gõ chỉ có Ghi chú nội bộ (máy chủ chỉ nhận gửi tin ở hộp thư)", async () => {
    stubConversation(convFake({ detail: commentConv, messages: [comment(1, { body: "Hỏi size" })] }));
    renderConversation();
    await actsOf("Hỏi size");
    expect(screen.queryByRole("tab", { name: "Trả lời" })).toBeNull();
    expect(screen.queryByRole("textbox", { name: "Nội dung trả lời" })).toBeNull();
    expect(screen.getByRole("textbox", { name: "Nội dung ghi chú" })).toBeTruthy();
  });
});
