// @vitest-environment jsdom
// Khung hội thoại (Task 175; mockup hoi-thoai.html; CH01 phía hội thoại, CH06 "Nhận", đổi trạng thái, đã đọc).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../../app/queryClient";
import { conv, NAME_60 } from "../inboxTestkit";
import {
  CONV,
  convFake,
  detail,
  msg,
  msgId,
  out,
  renderConversation,
  stubConversation,
  writesTo,
} from "./conversationTestkit";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ConversationView", () => {
  it("đầu hội thoại: tên khách 60 ký tự, Page, loại, người phụ trách; nút về danh sách", async () => {
    stubConversation(
      convFake({
        detail: detail({
          conversation: conv(1, { customer: { id: "c", name: NAME_60 }, assignee: { id: "u2", name: "Trần Minh An" } }),
        }),
      }),
    );
    renderConversation();
    const name = await screen.findByText(NAME_60);
    expect(name.classList.contains("ellip")).toBe(true);
    expect(screen.getByText("ELIMAZ Official · Tin nhắn · Trần Minh An phụ trách")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Về danh sách hội thoại" }).getAttribute("href")).toBe("/inbox");
  });

  it("CH06: chưa ai nhận → 'Nhận' gọi claimConversation", async () => {
    const f = stubConversation(convFake());
    renderConversation();
    fireEvent.click(await screen.findByRole("button", { name: "Nhận" }));
    await waitFor(() => {
      expect(writesTo(f, "/claim")).toHaveLength(1);
    });
    expect(writesTo(f, "/claim")[0]?.path).toBe(`/api/v1/conversations/${CONV}/claim`);
  });

  it("đổi trạng thái gửi setConversationStatus kèm expectedVersion", async () => {
    const f = stubConversation(convFake({ detail: detail({ conversation: conv(1, { version: 7 }) }) }));
    renderConversation();
    fireEvent.change(await screen.findByRole("combobox", { name: "Trạng thái hội thoại" }), {
      target: { value: "done" },
    });
    await waitFor(() => {
      expect(writesTo(f, "/status")).toHaveLength(1);
    });
    expect(writesTo(f, "/status")[0]?.body).toEqual({ status: "done", expectedVersion: 7 });
  });

  it("có tin chưa đọc → đánh dấu đã đọc tới tin mới nhất của khách (một lần)", async () => {
    const f = stubConversation(
      convFake({
        detail: detail({ conversation: conv(1, { unreadCount: 2 }) }),
        messages: [msg(1), msg(2), out(3, "sent")],
      }),
    );
    renderConversation();
    await waitFor(() => {
      expect(writesTo(f, "/read")).toHaveLength(1);
    });
    expect(writesTo(f, "/read")[0]?.body).toEqual({ upToMessageId: msgId(2) });
  });

  it("CH01: báo hiệu conversation.changed của hội thoại này → tải lại, tin mới hiện", async () => {
    const f = stubConversation(convFake({ messages: [msg(1, { body: "Còn size L không" })] }));
    const { qc } = renderConversation();
    const log = await screen.findByRole("log", { name: "Tin của hội thoại" });
    await within(log).findByText("Còn size L không");
    f.messages = [...f.messages, msg(2, { body: "Chị lấy 2 cái" })];
    act(() => {
      onRealtimeChanged(qc, { topic: "conversation.changed", entity: { type: "conversation", id: CONV } });
    });
    expect(await within(log).findByText("Chị lấy 2 cái")).toBeTruthy();
  });

  it("còn tin cũ hơn → 'Xem tin cũ hơn' tải trang trước bằng before, giữ tin đang có", async () => {
    const f = stubConversation(
      convFake({ messages: [msg(5, { body: "Tin mới" })], older: [msg(1, { body: "Tin cũ" })] }),
    );
    renderConversation();
    await screen.findByText("Tin mới");
    fireEvent.click(screen.getByRole("button", { name: "Xem tin cũ hơn" }));
    expect(await screen.findByText("Tin cũ")).toBeTruthy();
    expect(screen.getByText("Tin mới")).toBeTruthy();
    const before = f.reads.filter((r) => r.path.endsWith("/messages") && r.q.has("before"));
    expect(before.map((r) => r.q.get("before"))).toEqual(["o1"]);
    expect(screen.queryByRole("button", { name: "Xem tin cũ hơn" })).toBeNull();
  });
});
