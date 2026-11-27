// @vitest-environment jsdom
// Chi tiết bảng kê và Chốt (mockup docs/mockups/m3/doi-soat-cod.html trạng thái C, D; HV31; TI02, TI05, TI06, TI07).
// Kế hoạch M3 Task 137, Step 1: nhóm "Ngoài hệ thống" ghi "Không tính vào số liệu"; tổng tiền theo nhóm 9 chữ số không
// tràn; chốt 409 → "Bảng kê vừa được người khác chốt hoặc đổi — tải lại" và không đổi số đã hiện.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import {
  EXTERNAL_LINE,
  KE,
  MISMATCH_LINES,
  ORDER,
  S_MATCHED,
  S_PARSING,
  TOTALS_C,
  grantsOf,
  installFake,
  line,
  renderAt,
  statement,
  stubDialog,
  type Fake,
} from "./codTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
  fake.details.set(S_MATCHED, {
    statement: statement(),
    totals: TOTALS_C,
    lines: [...MISMATCH_LINES, EXTERNAL_LINE, line({ lineNo: 1, matchStatus: "matched", diffAmount: 0 })],
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const open = async () => {
  const r = renderAt(`/cod/statements/${S_MATCHED}`);
  await screen.findByRole("heading", { name: "Viettel Post — Kho chính · 08/11–14/11" });
  await screen.findByText(/^Nhóm .* · hiện \d+$/);
  return r;
};
const group = (name: RegExp) => screen.getByRole("link", { name });

describe("Chi tiết bảng kê", () => {
  it("đầu bảng kê, năm nhóm có số dòng và tổng tiền (9 chữ số trong ô .money), Ngoài hệ thống ghi Không tính vào số liệu", async () => {
    await open();
    expect(screen.getByText(/206 dòng · tổng khai trên tệp/)).toBeTruthy();
    expect(screen.getByText(/nhập 16:05 21\/11 bởi Mai Thị Kế/)).toBeTruthy();
    const names = screen
      .getAllByRole("link")
      .filter((a) => a.classList.contains("grp"))
      .map((a) => a.textContent);
    expect(names).toEqual([
      "Khớp · 196 dòng123.456.789 ₫",
      "Lệch tiền · 3 dòng2.140.000 ₫",
      "Ngoài hệ thống · 4 dòng1.610.000 ₫Không tính vào số liệu",
      "Trùng · 1 dòng690.000 ₫",
      "Chỉ phí · 2 dòng0 ₫Phí hãng, không có thu hộ",
    ]);
    const nine = within(group(/^Khớp/)).getByText("123.456.789 ₫");
    expect(nine.className).toContain("money");
    expect(group(/^Lệch tiền/).className).toContain("is-bad");
    expect(group(/^Ngoài hệ thống/).className).toContain("is-off");
  });

  it("mở sẵn nhóm Lệch tiền: dòng, mã vận đơn, đơn (liên kết), hãng thu, CRM chờ, lệch có dấu, phí hãng âm", async () => {
    await open();
    expect(group(/^Lệch tiền/).getAttribute("aria-current")).toBe("true");
    const t = screen.getByRole("table");
    const body = within(t).getAllByRole("row").slice(1);
    expect(body).toHaveLength(3);
    const first = body[0];
    if (first === undefined) throw new Error("thiếu dòng");
    expect(within(first).getByText("Dòng 12")).toBeTruthy();
    expect(within(first).getByRole("link", { name: "DH0001101" }).getAttribute("href")).toBe(`/don/${ORDER}`);
    expect(within(first).getByText("−90.000 ₫").className).toContain("err-text");
    expect(within(first).getByText("−32.000 ₫")).toBeTruthy();
    expect(within(body[2] ?? first).getByText("+100.000 ₫").className).not.toContain("err-text");
    expect(fake.detailCalls.at(-1)?.get("matchStatus")).toBe("amount_mismatch");
    expect(screen.getByText("Lệch tiền tính bằng hãng thu − CRM chờ")).toBeTruthy();
  });

  it("bấm nhóm Ngoài hệ thống: bảng riêng có ghi chú không tính vào số liệu", async () => {
    await open();
    fireEvent.click(group(/^Ngoài hệ thống/));
    const cell = await screen.findByText(/Không có vận đơn này trong CRM \(đơn Nhanh cũ\) · không tính vào số liệu/);
    expect(cell.closest("tr")?.className).toContain("is-muted");
    expect(fake.detailCalls.at(-1)?.get("matchStatus")).toBe("external");
    expect(group(/^Ngoài hệ thống/).getAttribute("aria-current")).toBe("true");
  });

  it("thanh Chốt: câu theo tổng của máy chủ; chốt gửi version đang xem, khóa chống trùng; xong hiện Đã chốt kèm cảnh báo", async () => {
    fake.replies.push({
      status: 200,
      body: {
        statement: statement({ status: "confirmed", confirmedAt: "2026-11-21T09:22:00Z", confirmedBy: KE, version: 5 }),
        warnings: ["Tổng thực nhận của các dòng ghi tiền (127884789) khác tổng khai trên tệp (127896789)"],
      },
    });
    await open();
    const bar = screen.getByText("Chốt bảng kê", { selector: "b" }).closest(".box");
    if (!(bar instanceof HTMLElement)) throw new Error("không có thanh Chốt");
    expect(bar.textContent).toContain(
      'Ghi tiền về shop cho 196 dòng khớp và 3 dòng lệch theo số hãng thu; phần thiếu 100.000 ₫ vẫn mở và vào "Lệch đối soát". Tách phí hãng 6.240.000 ₫. Dòng ngoài hệ thống, trùng không ghi.',
    );
    fireEvent.click(within(bar).getByRole("button", { name: "Chốt bảng kê" }));
    expect(await screen.findByText("Đã chốt")).toBeTruthy();
    const w = fake.writes.find((x) => x.path === `/api/v1/cod-statements/${S_MATCHED}/confirm`);
    expect(w?.body).toEqual({ version: 4 });
    expect(w?.key).toMatch(/^[0-9a-f-]{36}$/);
    const ok = screen.getByText("Đã chốt").closest(".box");
    expect(ok?.textContent).toContain("16:22 · 21/11 bởi Mai Thị Kế");
    expect(ok?.textContent).toContain("khác tổng khai trên tệp (127896789)");
    expect(screen.queryByRole("button", { name: "Chốt bảng kê" })).toBeNull();
  });

  it("TI07: chốt 409 statement_changed → câu tải lại, số đang hiện không đổi dù báo hiệu đến; Tải lại mới đọc bản mới", async () => {
    fake.replies.push({ status: 409, body: { error: "statement_changed", request_id: "t" } });
    const { qc } = await open();
    fireEvent.click(screen.getByRole("button", { name: "Chốt bảng kê" }));
    const box = await screen.findByText("Bảng kê vừa được người khác chốt hoặc đổi — tải lại.");
    expect(box.closest(".box")?.textContent).toMatch(
      /Chưa ghi gì từ lần bấm của bạn\. Số trên màn là bản bạn đang xem lúc \d\d:\d\d\./,
    );
    // Người khác đã chốt: máy chủ giờ trả bảng kê confirmed và tổng khác; báo hiệu đến.
    fake.details.set(S_MATCHED, {
      statement: statement({ status: "confirmed", confirmedAt: "2026-11-21T09:21:00Z", confirmedBy: KE, version: 5 }),
      totals: [{ matchStatus: "matched", count: 199, codAmount: 999_999_999, feeAmount: 0, shortAmount: 0 }],
      lines: [],
    });
    onRealtimeChanged(qc, { topic: "cod_statement.changed", entity: { type: "cod_statement", id: S_MATCHED } });
    await new Promise((r) => setTimeout(r, 30));
    expect(within(group(/^Khớp/)).getByText("123.456.789 ₫")).toBeTruthy();
    expect(screen.queryByText("999.999.999 ₫")).toBeNull();
    expect(screen.getByRole("button", { name: "Chốt bảng kê" }).hasAttribute("disabled")).toBe(true);
    // Đang giữ bản cũ: bấm nhóm khác không đổi bảng (không hiện dòng nhóm cũ dưới tên nhóm mới).
    fireEvent.click(group(/^Ngoài hệ thống/));
    expect(group(/^Lệch tiền/).getAttribute("aria-current")).toBe("true");
    expect(group(/^Ngoài hệ thống/).getAttribute("aria-disabled")).toBe("true");
    expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(4);
    expect(screen.getByText("Nhóm Lệch tiền 3 dòng · hiện 3")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tải lại" }));
    expect(await screen.findByText("999.999.999 ₫")).toBeTruthy();
    expect(screen.queryByText("Bảng kê vừa được người khác chốt hoặc đổi — tải lại.")).toBeNull();
  });

  it("không có quyền chốt: không có thanh Chốt; bảng kê đã chốt: hiện Đã chốt, không có nút", async () => {
    fake.grants = grantsOf();
    await open();
    expect(screen.queryByRole("button", { name: "Chốt bảng kê" })).toBeNull();
    cleanup();
    fake.grants = grantsOf("cash.statements.confirm");
    fake.details.set(S_MATCHED, {
      statement: statement({ status: "confirmed", confirmedAt: "2026-11-21T09:22:00Z", confirmedBy: KE }),
      totals: TOTALS_C,
      lines: [],
    });
    await open();
    expect(screen.getByText("Đã chốt 21/11")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Chốt bảng kê" })).toBeNull();
  });

  it("đang phân tích: thanh chạy, chưa có nhóm; nhiều trang: Tải thêm theo con trỏ", async () => {
    fake.details.set(S_PARSING, {
      statement: statement({ id: S_PARSING, status: "parsing", periodFrom: undefined, periodTo: undefined }),
      totals: [],
      lines: [],
    });
    renderAt(`/cod/statements/${S_PARSING}`);
    expect(await screen.findByRole("progressbar", { name: "Đang phân tích" })).toBeTruthy();
    expect(screen.queryByRole("link", { name: /^Khớp/ })).toBeNull();
    cleanup();
    const d = fake.details.get(S_MATCHED);
    if (d !== undefined) d.nextCursor = "140";
    await open();
    expect(screen.getByText(/Nhóm Lệch tiền 3 dòng · hiện 3/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tải thêm" }));
    await waitFor(() => {
      expect(fake.detailCalls.at(-1)?.get("cursor")).toBe("140");
    });
  });
});
