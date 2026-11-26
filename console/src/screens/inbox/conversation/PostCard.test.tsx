// @vitest-environment jsdom
// Thẻ bài viết của hội thoại comment và lý do ẩn comment (Task 501; mockup hoi-thoai.html trạng thái C).
import { cleanup, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ChatMessage } from "../../../api";
import { conv } from "../inboxTestkit";
import { convFake, detail, msg, renderConversation, stubConversation } from "./conversationTestkit";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const IMG = "0195f3b2-0000-7000-8000-0000000a0001";
const thread = () => screen.findByRole("log", { name: "Tin của hội thoại" });

function commentDetail(post: NonNullable<ReturnType<typeof conv>["post"]>) {
  return detail({ conversation: conv(1, { kind: "comment", customer: { id: "c1", name: "Lý Minh Châu" }, post }) });
}

describe("PostCard", () => {
  it("bài có ảnh, giờ đăng, đường dẫn: ảnh nạp qua getAttachmentUrl của hội thoại, 'Mở bài' mở tab mới", async () => {
    const f = stubConversation(
      convFake({
        detail: commentDetail({
          id: "p1",
          snippet: "Áo khoác dạ dáng dài — hàng mới về",
          postedAt: "2026-11-18T13:00:00Z",
          permalink: "https://www.facebook.com/p1",
          imageFileId: IMG,
        }),
      }),
    );
    renderConversation();
    const t = within(await thread());
    expect(t.getByText("Áo khoác dạ dáng dài — hàng mới về")).toBeTruthy();
    expect(t.getByText("Bài trên Page · đăng 20:00 18/11")).toBeTruthy();
    const open = t.getByRole("link", { name: "Mở bài" });
    expect(open.getAttribute("href")).toBe("https://www.facebook.com/p1");
    expect(open.getAttribute("target")).toBe("_blank");
    expect(open.getAttribute("rel")).toContain("noopener");
    const img = await t.findByRole("img", { name: "Ảnh bài" });
    expect(img.getAttribute("src")).toBe(`https://kho.example/${IMG}`);
    expect(f.reads.some((r) => r.path.endsWith(`/attachments/${IMG}/url`))).toBe(true);
  });

  it("bài chưa đọc được (không ảnh, giờ, đường dẫn): chữ 'Bài', câu chung, không có nút Mở bài, không xin ảnh", async () => {
    const f = stubConversation(convFake({ detail: commentDetail({ id: "p1" }) }));
    renderConversation();
    const t = within(await thread());
    expect(t.getByText("Bài viết trên Page")).toBeTruthy();
    expect(t.getByText("Comment trên bài này")).toBeTruthy();
    expect(t.queryByRole("link", { name: "Mở bài" })).toBeNull();
    expect(t.queryByRole("img")).toBeNull();
    expect(f.reads.some((r) => r.path.includes("/attachments/"))).toBe(false);
  });
});

describe("lý do ẩn comment", () => {
  const hidden = (i: number, hiddenReason?: ChatMessage["hiddenReason"]) =>
    msg(i, { kind: "comment", isHidden: true, ...(hiddenReason === undefined ? {} : { hiddenReason }) });

  it("mỗi lý do một câu; không lý do → câu chung", async () => {
    stubConversation(
      convFake({
        detail: commentDetail({ id: "p1" }),
        messages: [
          hidden(1, "auto_phone"),
          hidden(2, "auto_all"),
          hidden(3, "automation"),
          hidden(4, "staff"),
          hidden(5, "page_external"),
          hidden(6),
        ],
      }),
    );
    renderConversation();
    const t = within(await thread());
    await t.findAllByText("Đã ẩn trên Page");
    for (const text of [
      "tự ẩn vì có SĐT",
      "tự ẩn (ẩn mọi comment)",
      "tự ẩn theo cài đặt",
      "nhân viên ẩn trong CRM",
      "ẩn trên Facebook ngoài CRM",
    ])
      expect(t.getByText(`· ${text}; trên Facebook chỉ người comment và bạn bè họ thấy, CRM vẫn hiện đủ`)).toBeTruthy();
    expect(t.getAllByText("Đã ẩn trên Page")).toHaveLength(6);
    expect(t.getByText("· trên Facebook chỉ người comment và bạn bè họ thấy, CRM vẫn hiện đủ")).toBeTruthy();
  });
});
