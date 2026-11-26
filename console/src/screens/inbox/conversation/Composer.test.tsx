// @vitest-environment jsdom
// Ô gõ của màn hội thoại (Task 175; mockup hoi-thoai.html trạng thái A, D; CH03, CH04, CH07, CH09; HV15–HV18, HV17 sửa).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { account, all } from "../inboxTestkit";
import {
  CONV,
  FILE_IMG,
  P1,
  TPL,
  convFake,
  detail,
  renderConversation,
  stubConversation,
  template,
  writesTo,
  isDisabled,
  valueOf,
} from "./conversationTestkit";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const box = () => screen.findByRole("textbox", { name: "Nội dung trả lời" });
const minutes = (n: number) => new Date(Date.now() + n * 60_000 + 20_000).toISOString();

describe("Composer: ô gõ theo chính sách gửi của Facebook", () => {
  it("CH09: hết 24 giờ → ô gõ khóa, câu lý do; vẫn viết được ghi chú nội bộ", async () => {
    stubConversation(convFake({ detail: detail({ sendPolicy: { allowed: false, reason: "window_closed" } }) }));
    renderConversation();
    expect(isDisabled(await box())).toBe(true);
    expect(
      screen.getByText("Đã quá 24 giờ từ tin cuối của khách, Facebook không cho gửi tin thường", { exact: false }),
    ).toBeTruthy();
    expect(isDisabled(screen.getByRole("button", { name: "Gửi" }))).toBe(true);
    fireEvent.click(screen.getByRole("tab", { name: "Ghi chú nội bộ" }));
    expect(isDisabled(screen.getByRole("textbox", { name: "Nội dung ghi chú" }))).toBe(false);
  });

  it("còn cửa sổ: đồng hồ 'Còn 30 phút để trả lời'", async () => {
    stubConversation(
      convFake({ detail: detail({ sendPolicy: { allowed: true, mode: "standard", until: minutes(30) } }) }),
    );
    renderConversation();
    expect(await screen.findByText("Còn 30 phút để trả lời")).toBeTruthy();
  });

  it("Human Agent: nút 'Trả lời (nhân viên)', gửi có humanAgent: true", async () => {
    const f = stubConversation(
      convFake({ detail: detail({ sendPolicy: { allowed: true, mode: "human_agent", until: minutes(5 * 24 * 60) } }) }),
    );
    renderConversation();
    fireEvent.change(await box(), { target: { value: "Dạ đơn của chị đã giao" } });
    fireEvent.click(screen.getByRole("button", { name: "Trả lời (nhân viên)" }));
    await waitFor(() => {
      expect(writesTo(f, "/messages")).toHaveLength(1);
    });
    expect(writesTo(f, "/messages")[0]?.body).toEqual({ text: "Dạ đơn của chị đã giao", humanAgent: true });
    expect(screen.getByText("Human Agent", { exact: false })).toBeTruthy();
  });

  it("Page chỉ xem → ô gõ khóa, câu chế độ chỉ xem", async () => {
    stubConversation(
      convFake({
        detail: detail({
          page: account(P1, "ELIMAZ Sale", { sendMode: "read_only" }),
          sendPolicy: { allowed: false, reason: "read_only" },
        }),
      }),
    );
    renderConversation();
    expect(isDisabled(await box())).toBe(true);
    expect(screen.getByText("ELIMAZ Sale đang ở chế độ chỉ xem", { exact: false })).toBeTruthy();
  });

  it("không có conversation.reply → ô gõ khóa kèm lý do quyền", async () => {
    stubConversation(convFake({ grants: [all("conversation.read")] }));
    renderConversation();
    expect(isDisabled(await box())).toBe(true);
    expect(screen.getByText("Bạn chưa được cấp quyền trả lời", { exact: false })).toBeTruthy();
  });

  it("CH03: Enter gửi chữ, ô gõ trống lại; Shift + Enter không gửi (HV17 sửa)", async () => {
    const f = stubConversation(convFake());
    renderConversation();
    const t = await box();
    fireEvent.change(t, { target: { value: "Dạ còn chị ạ" } });
    fireEvent.keyDown(t, { key: "Enter", shiftKey: true });
    expect(writesTo(f, "/messages")).toHaveLength(0);
    fireEvent.keyDown(t, { key: "Enter" });
    await waitFor(() => {
      expect(writesTo(f, "/messages")).toHaveLength(1);
    });
    expect(writesTo(f, "/messages")[0]?.body).toEqual({ text: "Dạ còn chị ạ" });
    await waitFor(() => {
      expect(valueOf(t)).toBe("");
    });
  });

  it("503 retry_later rồi 201 → hai yêu cầu cùng Idempotency-Key", async () => {
    const f = stubConversation(convFake());
    f.queued["/messages"] = [{ status: 503, body: { error: "retry_later", request_id: "t" } }];
    renderConversation();
    const t = await box();
    fireEvent.change(t, { target: { value: "Shop gửi bảng size ạ" } });
    fireEvent.click(screen.getByRole("button", { name: "Gửi" }));
    await waitFor(() => {
      expect(writesTo(f, "/messages")).toHaveLength(2);
    });
    const [a, b] = writesTo(f, "/messages");
    expect(a?.key).not.toBeNull();
    expect(b?.key).toBe(a?.key);
    await waitFor(() => {
      expect(valueOf(t)).toBe("");
    });
  });

  it("gửi bị 409 send_not_allowed → câu theo denyReason, chữ đã gõ vẫn giữ", async () => {
    const f = stubConversation(convFake());
    f.queued["/messages"] = [
      { status: 409, body: { error: "send_not_allowed", request_id: "t", denyReason: "blocked_by_user" } },
    ];
    renderConversation();
    const t = await box();
    fireEvent.change(t, { target: { value: "Chị ơi" } });
    fireEvent.click(screen.getByRole("button", { name: "Gửi" }));
    expect(await screen.findByText("Khách đã chặn tin nhắn từ Page", { exact: false })).toBeTruthy();
    expect(valueOf(t)).toBe("Chị ơi");
  });

  it("CH07: gõ /xin → menu có /xindiachi; Enter chọn → ô gõ có chữ đã điền biến (không gửi)", async () => {
    const f = stubConversation(
      convFake({
        templates: [template("xindiachi", "Chị {ten_khach} cho em xin địa chỉ"), template("size", "Size", { id: "x" })],
        rendered: { [TPL]: "Chị Châu cho em xin địa chỉ" },
      }),
    );
    renderConversation();
    const t = await box();
    fireEvent.change(t, { target: { value: "/xin" } });
    const menu = await screen.findByRole("listbox", { name: "Mẫu trả lời" });
    expect(await within(menu).findByText("/xindiachi")).toBeTruthy();
    expect(within(menu).queryByText("/size")).toBeNull();
    const call = f.reads.find((r) => r.path === "/api/v1/reply-templates");
    expect(call?.q.get("q")).toBe("xin");
    expect(call?.q.get("pageId")).toBe(P1);
    fireEvent.keyDown(t, { key: "Enter" });
    await waitFor(() => {
      expect(valueOf(t)).toBe("Chị Châu cho em xin địa chỉ");
    });
    expect(writesTo(f, "/render")[0]?.body).toEqual({ conversationId: CONV });
    expect(writesTo(f, "/messages")).toHaveLength(0);
    expect(screen.queryByRole("listbox", { name: "Mẫu trả lời" })).toBeNull();
  });

  it("menu mẫu: Esc đóng, không chèn", async () => {
    stubConversation(convFake({ templates: [template("stk", "STK")] }));
    renderConversation();
    const t = await box();
    fireEvent.change(t, { target: { value: "/st" } });
    await screen.findByRole("listbox", { name: "Mẫu trả lời" });
    fireEvent.keyDown(t, { key: "Escape" });
    expect(screen.queryByRole("listbox", { name: "Mẫu trả lời" })).toBeNull();
    expect(valueOf(t)).toBe("/st");
  });

  it("CH07: tab Ghi chú nội bộ → addConversationNote, không gửi cho khách", async () => {
    const f = stubConversation(convFake());
    renderConversation();
    await box();
    fireEvent.click(screen.getByRole("tab", { name: "Ghi chú nội bộ" }));
    const n = screen.getByRole("textbox", { name: "Nội dung ghi chú" });
    fireEvent.change(n, { target: { value: "Khách quen, hay đổi size" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu ghi chú" }));
    await waitFor(() => {
      expect(writesTo(f, "/notes")).toHaveLength(1);
    });
    expect(writesTo(f, "/notes")[0]?.body).toEqual({ text: "Khách quen, hay đổi size" });
    expect(writesTo(f, "/messages")).toHaveLength(0);
  });

  it("CH04: chọn ảnh → tải lên mục đích chat_outgoing, chờ ready, gửi tin attachmentFileId", async () => {
    const f = stubConversation(convFake());
    renderConversation();
    await box();
    const input = screen.getByLabelText("Chọn ảnh gửi khách");
    const file = new File(["x"], "bang-size.jpg", { type: "image/jpeg" });
    fireEvent.change(input, { target: { files: [file] } });
    await waitFor(() => {
      expect(writesTo(f, "/messages")).toHaveLength(1);
    });
    expect(writesTo(f, "/files/uploads")[0]?.body).toMatchObject({
      purpose: "chat_outgoing",
      fileName: "bang-size.jpg",
    });
    expect(writesTo(f, "/messages")[0]?.body).toEqual({ attachmentFileId: FILE_IMG });
  });

  it("tải ảnh bị từ chối → câu lỗi, không gửi tin", async () => {
    const f = stubConversation(convFake());
    f.queued["/files/uploads"] = [{ status: 422, body: { error: "file_type_not_allowed", request_id: "t" } }];
    renderConversation();
    await box();
    fireEvent.change(screen.getByLabelText("Chọn ảnh gửi khách"), {
      target: { files: [new File(["x"], "a.gif", { type: "image/gif" })] },
    });
    expect(await screen.findByText("Loại ảnh không nhận", { exact: false })).toBeTruthy();
    expect(writesTo(f, "/messages")).toHaveLength(0);
  });
});
