// @vitest-environment jsdom
// Bong bóng tin (Task 175; mockup hoi-thoai.html trạng thái A, C; HV10–HV13, HV21 sửa, HV22; 04 mục 13; TP04).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  CONV,
  FILE_IMG,
  FILE_PDF,
  convFake,
  msg,
  msgId,
  out,
  renderConversation,
  stubConversation,
} from "./conversationTestkit";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const thread = () => screen.findByRole("log", { name: "Tin của hội thoại" });

describe("MessageBubble", () => {
  it("04 mục 13: nội dung có thẻ HTML hiện nguyên văn, DOM không có img từ nội dung", async () => {
    const evil = "<img src=x onerror=alert(1)>";
    stubConversation(convFake({ messages: [msg(1, { body: evil, senderName: "<b>Khách</b>" })] }));
    renderConversation();
    const t = await thread();
    expect(await within(t).findByText(evil)).toBeTruthy();
    expect(t.querySelector("img")).toBeNull();
    expect(t.querySelector("b > b")).toBeNull();
  });

  it("đủ loại tin: lịch sử, hệ thống, tự động, ngoài CRM, ghi chú 'Chỉ nhân viên thấy', nhân viên ghi tên", async () => {
    stubConversation(
      convFake({
        messages: [
          msg(1, { origin: "history", body: "Áo len còn màu kem không?" }),
          msg(2, { kind: "system", origin: "system", direction: "in", body: "CRM tạo hồ sơ khách tiềm năng KH005120" }),
          out(3, "sent", { origin: "automation", body: "ELIMAZ chào chị!", sender: undefined, senderName: "Tin chào" }),
          out(4, "sent", {
            origin: "page_external",
            body: "Chị gửi em địa chỉ",
            sender: undefined,
            senderName: undefined,
          }),
          out(5, undefined, { kind: "note", body: "Khách quen, hay đổi size" }),
          out(6, "sent", { body: "Dạ còn chị ạ" }),
        ],
      }),
    );
    renderConversation();
    const t = within(await thread());
    expect(await t.findByText("trước khi kết nối CRM", { exact: false })).toBeTruthy();
    expect(t.getByText("CRM tạo hồ sơ khách tiềm năng KH005120").classList.contains("sys")).toBe(true);
    expect(t.getByText("Tin tự động")).toBeTruthy();
    expect(t.getByText("Gửi từ ứng dụng Meta (ngoài CRM)")).toBeTruthy();
    expect(t.getByText("Ghi chú nội bộ · Chỉ nhân viên thấy")).toBeTruthy();
    expect(t.getByText("không gửi cho khách", { exact: false })).toBeTruthy();
    expect(t.getAllByText("Trần Minh An", { exact: false }).length).toBeGreaterThan(0);
  });

  it("TP04: bấm ảnh mới xin getAttachmentUrl rồi hiện ảnh trong trang", async () => {
    const f = stubConversation(
      convFake({
        messages: [msg(1, { body: undefined, attachments: [{ fileId: FILE_IMG, type: "image", status: "stored" }] })],
      }),
    );
    renderConversation();
    const t = within(await thread());
    const btn = await t.findByRole("button", { name: "Ảnh khách gửi, bấm để xem" });
    expect(f.reads.some((r) => r.path.includes("/attachments/"))).toBe(false);
    fireEvent.click(btn);
    const img = await t.findByRole("img", { name: "Ảnh khách gửi" });
    expect(img.getAttribute("src")).toBe(`https://kho.example/${FILE_IMG}`);
    expect(f.reads.find((r) => r.path.includes("/attachments/"))?.path).toBe(
      `/api/v1/conversations/${CONV}/attachments/${FILE_IMG}/url`,
    );
  });

  it("TP04: tệp PDF chỉ có liên kết tải (download), không xem trước; tên dài giữ nguyên chữ", async () => {
    const name = "bang-do-so-do-khach-hang-dat-may-rieng-thang-11-2026-ban-cuoi-cung-da-sua-lan-3.pdf";
    stubConversation(
      convFake({
        messages: [
          msg(1, { body: undefined, attachments: [{ fileId: FILE_PDF, type: "file", status: "stored", name }] }),
        ],
      }),
    );
    renderConversation();
    const t = within(await thread());
    expect(await t.findByText(name)).toBeTruthy();
    fireEvent.click(t.getByRole("button", { name: "Tải về" }));
    const link = await t.findByRole("link", { name: "Tải về" });
    expect(link.hasAttribute("download")).toBe(true);
    expect(link.getAttribute("href")).toBe(`https://kho.example/${FILE_PDF}`);
    expect(t.queryByRole("img")).toBeNull();
  });

  it("HV22: comment 2.000 ký tự gọn 8 dòng, 'Xem hết (2.000 ký tự)' mở hết", async () => {
    stubConversation(convFake({ messages: [msg(1, { kind: "comment", body: "a".repeat(2000) })] }));
    renderConversation();
    const btn = await screen.findByRole("button", { name: "Xem hết (2.000 ký tự)" });
    expect(btn.closest(".msg")?.querySelector(".clamp8")).not.toBeNull();
    fireEvent.click(btn);
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Xem hết (2.000 ký tự)" })).toBeNull();
    });
  });

  it("CH13, HV21 sửa: comment khách sửa → chip, lịch sử thu gọn; bấm 'Xem bản trước' mới tải", async () => {
    const f = stubConversation(
      convFake({
        messages: [msg(1, { kind: "comment", body: "Lấy 2 cái size L", editedCount: 1 })],
        revisions: {
          [msgId(1)]: [
            {
              body: "Lấy 1 cái size M",
              isHidden: false,
              removed: false,
              source: "customer",
              recordedAt: "2026-11-26T01:20:00Z",
            },
          ],
        },
      }),
    );
    renderConversation();
    expect(await screen.findByText("Khách đã sửa")).toBeTruthy();
    expect(screen.queryByText("Lấy 1 cái size M")).toBeNull();
    expect(f.reads.some((r) => r.path.endsWith("/revisions"))).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Xem bản trước" }));
    expect(await screen.findByText("Lấy 1 cái size M")).toBeTruthy();
    expect(screen.getByRole("list", { name: "Lịch sử sửa" })).toBeTruthy();
  });

  it("CH13: khách xóa comment → 'Khách đã xóa' và nội dung trước khi xóa", async () => {
    stubConversation(
      convFake({ messages: [msg(1, { kind: "comment", body: "Ship về Đà Nẵng mất mấy ngày?", removed: true })] }),
    );
    renderConversation();
    expect(await screen.findByText("Khách đã xóa comment này", { exact: false })).toBeTruthy();
    const list = screen.getByRole("list", { name: "Nội dung trước khi xóa" });
    expect(within(list).getByText("Ship về Đà Nẵng mất mấy ngày?")).toBeTruthy();
  });
});
