// @vitest-environment jsdom
// Hồ sơ khách theo gốc (mockup docs/mockups/m2/khach-ho-so.html trạng thái A, B; HV28). Kế hoạch M2 Task M2-29:
// nhãn rule có dấu riêng và không gỡ tay được; SĐT hiện đủ cho người có customer.read, không ghi vào log trình duyệt.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { C1, C2, C3, T1, T3, customer, grantsOf, installFake, renderAt, stubDialog, type Fake } from "./khachTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
  fake.customers.set(C1, customer());
  fake.timeline = [
    {
      at: "2026-11-18T07:31:00Z",
      kind: "order_created",
      refId: "o1",
      actor: { id: "u1", name: "Phạm Thu Trang" },
      summary: { code: "DH0001236", status: "new", grand_total: "1800000" },
    },
    {
      at: "2026-11-18T03:02:00Z",
      kind: "note",
      refId: "n1",
      actor: { id: "u1", name: "Phạm Thu Trang" },
      note: "Khách hay đổi size, tư vấn kỹ số đo trước khi chốt.",
    },
    {
      at: "2026-11-02T13:40:00Z",
      kind: "order_status",
      refId: "h1",
      summary: { order_id: "o0", code: "DH0001010", from_status: "delivered", to_status: "returned" },
    },
    {
      at: "2026-11-01T08:00:00Z",
      kind: "merge",
      refId: "m1",
      actor: { id: "u2", name: "Nguyễn Thị Lan" },
      summary: { survivor_id: C1, merged_id: C3, matched_by: "phone" },
    },
    {
      at: "2026-10-20T04:20:00Z",
      kind: "tag",
      refId: "t2",
      summary: { name: "Hay hoàn", color: "red", source: "rule" },
    },
  ];
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const card = (name: string) => {
  const h = screen.getByRole("heading", { name });
  const c = h.closest(".card");
  if (!(c instanceof HTMLElement)) throw new Error(`không thấy thẻ ${name}`);
  return c;
};

describe("Hồ sơ khách", () => {
  it("đầu trang, chỉ số của cả nhóm, dòng thời gian của cả nhóm (đơn có liên kết, ghi chú, đổi trạng thái, gộp, nhãn)", async () => {
    renderAt(`/khach/${C1}`);
    expect(await screen.findByRole("heading", { name: "Trần Thị Thu Hà", level: 1 })).toBeTruthy();
    expect(screen.getByText("Đã mua")).toBeTruthy();
    expect(screen.getByText(/KH000231 · tạo 03\/2026 · ngày sinh 12\/08/)).toBeTruthy();
    expect(screen.getByText("9 / 2")).toBeTruthy();
    expect(screen.getByText("tỷ lệ hoàn 18,18%")).toBeTruthy();
    expect(screen.getByText("8.450.000 ₫")).toBeTruthy();
    const tl = card("Dòng thời gian");
    expect(await within(tl).findByRole("link", { name: "DH0001236" })).toBeTruthy();
    expect(within(tl).getByRole("link", { name: "DH0001236" }).getAttribute("href")).toBe("/don/o1");
    expect(within(tl).getByText("1.800.000 ₫")).toBeTruthy();
    expect(within(tl).getByText("Khách hay đổi size, tư vấn kỹ số đo trước khi chốt.")).toBeTruthy();
    const st = within(tl).getByRole("link", { name: "DH0001010" });
    expect(st.getAttribute("href")).toBe("/don/o0");
    expect(st.closest("li")?.textContent).toMatch(/Đơn DH0001010 → Đã hoàn/);
    expect(within(tl).getByText(/Gộp một hồ sơ vào hồ sơ này/)).toBeTruthy();
    expect(within(tl).getByText(/Gắn nhãn "Hay hoàn" \(theo quy tắc\)/)).toBeTruthy();
    expect(within(tl).getByText("Của cả 2 hồ sơ")).toBeTruthy();
  });

  it("nhãn rule có dấu 'theo quy tắc' và không có nút gỡ; nhãn gắn tay có nút × gửi DELETE kèm khóa", async () => {
    fake.grants = grantsOf("customer.write");
    renderAt(`/khach/${C1}`);
    await screen.findByRole("heading", { name: "Trần Thị Thu Hà", level: 1 });
    const tags = card("Nhãn");
    expect(within(tags).getByText("Hay hoàn · theo quy tắc")).toBeTruthy();
    expect(within(tags).queryByRole("button", { name: "Gỡ nhãn Hay hoàn" })).toBeNull();
    fireEvent.click(within(tags).getByRole("button", { name: "Gỡ nhãn Khách quen" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)?.method).toBe("DELETE");
    });
    expect(fake.writes.at(-1)?.path).toBe(`/api/v1/customers/${C1}/tags/${T1}`);
    expect(fake.writes.at(-1)?.key).not.toBeNull();
  });

  it("thêm nhãn: chỉ nhãn chưa gắn trong danh sách; chọn → PUT", async () => {
    fake.grants = grantsOf("customer.write");
    renderAt(`/khach/${C1}`);
    await screen.findByRole("heading", { name: "Trần Thị Thu Hà", level: 1 });
    const sel = await within(card("Nhãn")).findByRole("combobox", { name: "Thêm nhãn" });
    await within(sel).findByRole("option", { name: "Khách sỉ" });
    expect(within(sel).queryByRole("option", { name: "Khách quen" })).toBeNull();
    fireEvent.change(sel, { target: { value: T3 } });
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({ method: "PUT", path: `/api/v1/customers/${C1}/tags/${T3}` });
    });
  });

  it("chỉ xem: không có nút gỡ nhãn, thêm nhãn, ghi chú, sửa, gộp, lên đơn", async () => {
    renderAt(`/khach/${C1}`);
    await screen.findByRole("heading", { name: "Trần Thị Thu Hà", level: 1 });
    expect(screen.queryByRole("button", { name: "Gỡ nhãn Khách quen" })).toBeNull();
    expect(screen.queryByRole("combobox", { name: "Thêm nhãn" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Lưu ghi chú" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Sửa" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Gộp với khách khác" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Lên đơn" })).toBeNull();
  });

  it("ghi chú: gửi nội dung kèm khóa, xong thì ô trống và dòng thời gian tải lại", async () => {
    fake.grants = grantsOf("customer.write");
    renderAt(`/khach/${C1}`);
    await screen.findByRole("heading", { name: "Trần Thị Thu Hà", level: 1 });
    const box = screen.getByRole("textbox", { name: "Ghi chú" });
    fireEvent.change(box, { target: { value: "Khách muốn giao giờ hành chính" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu ghi chú" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        method: "POST",
        path: `/api/v1/customers/${C1}/notes`,
        body: { body: "Khách muốn giao giờ hành chính" },
      });
    });
    await waitFor(() => {
      expect((box as HTMLTextAreaElement).value).toBe("");
    });
  });

  it("liên hệ: SĐT hiện đủ; danh tính Facebook không có nút Gỡ; SĐT có, gửi DELETE", async () => {
    fake.grants = grantsOf("customer.write");
    renderAt(`/khach/${C1}`);
    await screen.findByRole("heading", { name: "Trần Thị Thu Hà", level: 1 });
    const c = card("Liên hệ");
    expect(within(c).getByText("0912 345 678")).toBeTruthy();
    expect(within(c).getByText("0355 999 000")).toBeTruthy();
    expect(within(c).getAllByRole("button", { name: /^Gỡ / })).toHaveLength(2);
    fireEvent.click(within(c).getByRole("button", { name: "Gỡ 0355 999 000" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({ method: "DELETE", path: `/api/v1/customers/${C1}/identities/i2` });
    });
  });

  it("không ghi SĐT, ghi chú vào log của trình duyệt", async () => {
    const spies = (["log", "info", "warn", "error", "debug"] as const).map((m) => vi.spyOn(console, m));
    fake.grants = grantsOf("customer.write");
    renderAt(`/khach/${C1}`);
    await screen.findAllByText("0912 345 678");
    const logged = spies.flatMap((s) => s.mock.calls).map((c) => JSON.stringify(c));
    expect(logged.filter((l) => /912345678|0912 345 678|355999000|đổi size/.test(l))).toEqual([]);
  });

  it("gợi ý có thể là cùng một khách: nút So sánh và gộp chỉ cho người có quyền gộp", async () => {
    fake.candidates = [
      {
        rootId: C2,
        code: "KH001877",
        fullName: "Nguyễn Văn Tùng",
        matchedPhones: ["+84987654321"],
        stats: { orderCount: 2, deliveredCount: 2, returnedCount: 0, totalSpent: 980_000, returnRateBp: 0 },
      },
    ];
    renderAt(`/khach/${C1}`);
    expect(await screen.findByText(/KH001877 · Nguyễn Văn Tùng/)).toBeTruthy();
    expect(screen.getByText(/0987 654 321/)).toBeTruthy();
    expect(screen.queryByRole("link", { name: "So sánh và gộp" })).toBeNull();
    cleanup();
    fake.grants = grantsOf("customer.merge");
    renderAt(`/khach/${C1}`);
    const link = await screen.findByRole("link", { name: "So sánh và gộp" });
    expect(link.getAttribute("href")).toBe(`/khach/${C1}/gop?voi=${C2}`);
  });

  it("mở bằng mã hồ sơ con: hiện hồ sơ của cả nhóm theo gốc, thẻ Hồ sơ đã gộp", async () => {
    fake.customers.set(C3, customer());
    renderAt(`/khach/${C3}`);
    await screen.findByRole("heading", { name: "Trần Thị Thu Hà", level: 1 });
    expect(screen.getByRole("heading", { name: "Hồ sơ đã gộp" })).toBeTruthy();
    const c = card("Hồ sơ đã gộp");
    expect(within(c).getByText("Hồ sơ đại diện")).toBeTruthy();
    expect(within(c).getAllByText(/KH000231/).length).toBeGreaterThan(0);
  });

  it("khách tiềm năng chưa có SĐT, chưa có đơn: chip Tiềm năng, dòng thời gian trống", async () => {
    fake.customers.set(
      C2,
      customer({
        id: C2,
        rootId: C2,
        code: "KH004511",
        fullName: "Mai (fb)",
        lifecycle: "lead",
        birthday: undefined,
        memberIds: [C2],
        identities: [],
        addresses: [],
        tags: [],
        stats: { orderCount: 0, deliveredCount: 0, returnedCount: 0, totalSpent: 0, returnRateBp: 0 },
      }),
    );
    fake.timeline = [];
    renderAt(`/khach/${C2}`);
    expect(await screen.findByRole("heading", { name: "Mai (fb)", level: 1 })).toBeTruthy();
    expect(screen.getByText("Tiềm năng")).toBeTruthy();
    expect(await screen.findByText("Chưa có hoạt động.")).toBeTruthy();
    expect(within(card("Liên hệ")).getByText("Chưa có SĐT hay email.")).toBeTruthy();
  });

  it("sửa hồ sơ gửi version; 409 version_conflict → câu báo, phần đang gõ còn, Tải lại hồ sơ", async () => {
    fake.grants = grantsOf("customer.write");
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "r" } });
    renderAt(`/khach/${C1}`);
    await screen.findByRole("heading", { name: "Trần Thị Thu Hà", level: 1 });
    fireEvent.click(screen.getByRole("button", { name: "Sửa" }));
    const dlg = screen.getByRole("dialog", { name: "Sửa hồ sơ khách" });
    fireEvent.change(within(dlg).getByLabelText("Họ tên"), { target: { value: "Trần Thu Hà" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu" }));
    expect(await within(dlg).findByText("Hồ sơ đã thay đổi, tải lại.")).toBeTruthy();
    expect(fake.writes.at(-1)).toMatchObject({
      method: "PATCH",
      path: `/api/v1/customers/${C1}`,
      body: { version: 4, fullName: "Trần Thu Hà" },
    });
    expect(within(dlg).getByLabelText<HTMLInputElement>("Họ tên").value).toBe("Trần Thu Hà");
    fake.customers.set(C1, customer({ version: 5, fullName: "Trần T. Thu Hà" }));
    fireEvent.click(within(dlg).getByRole("button", { name: "Tải lại hồ sơ" }));
    expect(await screen.findByRole("heading", { name: "Trần T. Thu Hà", level: 1 })).toBeTruthy();
  });

  it("đọc gợi ý gộp lỗi: câu lỗi riêng, hồ sơ vẫn hiện", async () => {
    fake.failReads = [/merge-candidates$/];
    renderAt(`/khach/${C1}`);
    expect(await screen.findByText(/Chưa đọc được gợi ý "có thể là cùng một khách"/)).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Trần Thị Thu Hà", level: 1 })).toBeTruthy();
  });
});
