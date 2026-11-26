// @vitest-environment jsdom
// Danh sách hội thoại (Task 174; mockup inbox.html trạng thái A): danh sách ảo, dòng đủ loại (comment kèm tên bài, nhãn
// tối đa 3 rồi +n, 99+, người phụ trách), tên 60 ký tự không tràn, dòng đang mở, dòng vừa có tin (nền hồng).
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it } from "vitest";
import type { ConversationSummary } from "../../api";
import { ConversationList, type ConversationListProps } from "./ConversationList";
import { ME, NAME_60, P1, P2, U2, conv, convId, tag } from "./inboxTestkit";

afterEach(cleanup);

const NAMES = new Map([
  [P1, "ELIMAZ Official"],
  [P2, "ELIMAZ Sale"],
]);

function renderList(rows: ConversationSummary[], p: Partial<ConversationListProps> = {}) {
  return render(
    <MemoryRouter>
      <ConversationList
        rows={rows}
        flash={new Set()}
        meId={ME}
        accountName={(id) => NAMES.get(id) ?? ""}
        search=""
        now={Date.parse("2026-11-26T03:00:00Z")}
        {...p}
      />
    </MemoryRouter>,
  );
}

const rowOf = (text: string) => {
  const el = screen.getByText(text).closest(".cv");
  if (!(el instanceof HTMLElement)) throw new Error(`không thấy dòng của ${text}`);
  return within(el);
};

it("200 hội thoại → chỉ dựng phần đang thấy: dưới 60 dòng trong DOM", () => {
  const rows = Array.from({ length: 200 }, (_, i) => conv(i + 1));
  const { container } = renderList(rows);
  const n = container.querySelectorAll(".cv").length;
  expect(n).toBeGreaterThan(0);
  expect(n).toBeLessThan(60);
  expect(screen.getByText("Khách 0001")).toBeTruthy();
  expect(screen.queryByText("Khách 0200")).toBeNull();
});

it("dòng tin nhắn: Page · Tin nhắn, nhãn tối đa 3 rồi +n, 99+, Của tôi; dòng comment: chữ Comment và tên bài", () => {
  renderList([
    conv(1, {
      customer: { id: "c1", name: "Lê Hà My" },
      unreadCount: 120,
      assignee: { id: ME, name: "Trần Minh An" },
      tags: Array.from({ length: 14 }, (_, i) => tag(i + 1)),
    }),
    conv(2, {
      customer: { id: "c2", name: "Hoàng Văn Nam" },
      kind: "comment",
      pageId: P2,
      unreadCount: 1,
      post: { id: "post1", snippet: "Áo khoác dạ dáng dài" },
    }),
    conv(3, {
      customer: { id: "c3", name: "Đỗ Minh Quân" },
      status: "done",
      assignee: { id: U2, name: "Lê Thu Hằng" },
    }),
  ]);
  const my = rowOf("Lê Hà My");
  expect(my.getByText("ELIMAZ Official · Tin nhắn")).toBeTruthy();
  expect(my.getByText("99+").getAttribute("aria-label")).toBe("120 chưa đọc");
  expect(my.getByText("Nhãn 1")).toBeTruthy();
  expect(my.queryByText("Nhãn 4")).toBeNull();
  expect(my.getByText("+11").getAttribute("aria-label")).toBe("Còn 11 nhãn");
  expect(my.getByText("· Của tôi")).toBeTruthy();

  const nam = rowOf("Hoàng Văn Nam");
  expect(nam.getByText("Comment")).toBeTruthy();
  expect(nam.getByText("Bài: Áo khoác dạ dáng dài")).toBeTruthy();
  expect(nam.getByText("· Chưa ai nhận")).toBeTruthy();

  const quan = rowOf("Đỗ Minh Quân");
  expect(quan.getByText("· Xong · Lê Thu Hằng")).toBeTruthy();
  expect(quan.queryByLabelText(/chưa đọc/)).toBeNull();
});

it("tên khách 60 ký tự liền: giữ nguyên chữ, ô tên cắt một dòng (ellip), dòng là lưới có cột co được", () => {
  expect(NAME_60.length).toBe(60);
  renderList([conv(1, { customer: { id: "c1", name: NAME_60 }, unreadCount: 3 })]);
  const name = screen.getByText(NAME_60);
  expect(name.className).toContain("ellip");
  expect(name.closest(".cv")?.className).toContain("cv");
});

it("dòng đang mở có aria-current, dòng vừa có tin có nền hồng; liên kết giữ bộ lọc", () => {
  renderList([conv(1), conv(2)], { selectedId: convId(2), flash: new Set([convId(1)]), search: "?assignee=me" });
  const first = screen.getByText("Khách 0001").closest("a");
  const second = screen.getByText("Khách 0002").closest("a");
  expect(first?.className).toContain("is-new");
  expect(first?.getAttribute("href")).toBe(`/inbox/${convId(1)}?assignee=me`);
  expect(second?.getAttribute("aria-current")).toBe("true");
  expect(first?.getAttribute("aria-current")).toBeNull();
});

it("tên và đoạn tin là chữ thuần: không dựng HTML từ nội dung của khách", () => {
  const { container } = renderList([
    conv(1, { customer: { id: "c1", name: "<b>x</b>" }, lastSnippet: "<img src=x onerror=alert(1)>" }),
  ]);
  expect(container.querySelector("img")).toBeNull();
  expect(screen.getByText("<img src=x onerror=alert(1)>")).toBeTruthy();
});
