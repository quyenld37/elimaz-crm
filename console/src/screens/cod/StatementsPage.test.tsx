// @vitest-environment jsdom
// Danh sách bảng kê COD (mockup docs/mockups/m3/doi-soat-cod.html trạng thái A; HV30). Kế hoạch M3 Task 137, Step 1:
// bảng kê đang phân tích hiện tiến độ; lỗi missing_column hiện tên cột bằng tiếng Việt.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import { S_FAILED, S_MATCHED, grantsOf, installFake, renderAt, statement, stubDialog, type Fake } from "./codTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const rows = async () => {
  const t = await screen.findByRole("table");
  await within(t).findByText("SPX — Kho chính");
  return within(t).getAllByRole("row").slice(1);
};

describe("Danh sách bảng kê", () => {
  it("bốn trạng thái: đang phân tích (thanh chạy, Xem khóa), lỗi tệp (tên cột tiếng Việt, Nhập lại), chờ chốt, đã chốt", async () => {
    renderAt("/cod/statements");
    const [parsing, failed, matched, confirmed] = await rows();
    if (parsing === undefined || failed === undefined || matched === undefined || confirmed === undefined) {
      throw new Error("thiếu dòng");
    }
    expect(within(parsing).getByText("Viettel Post — Kho chính")).toBeTruthy();
    expect(within(parsing).getByText("Nhập 09:40 · 22/11 · Mai Thị Kế")).toBeTruthy();
    expect(within(parsing).getByText("Đang phân tích")).toBeTruthy();
    const bar = within(parsing).getByRole("progressbar", { name: "Đang phân tích" });
    expect(bar.getAttribute("aria-busy")).toBe("true");
    expect(within(parsing).getByRole("button", { name: "Xem" }).hasAttribute("disabled")).toBe(true);
    expect(within(parsing).getAllByText("—").length).toBeGreaterThanOrEqual(3);

    expect(within(failed).getByText("Lỗi tệp")).toBeTruthy();
    expect(within(failed).getByText('Thiếu cột "Tiền thu hộ"')).toBeTruthy();
    expect(failed.className).toContain("is-err");
    expect(within(failed).getByRole("button", { name: "Nhập lại" })).toBeTruthy();

    expect(within(matched).getByText("08/11–14/11")).toBeTruthy();
    expect(within(matched).getByText("206")).toBeTruthy();
    expect(within(matched).getByText("127.896.789 ₫").className).toContain("money");
    expect(within(matched).getByText("Đã khớp · chờ chốt")).toBeTruthy();
    expect(within(matched).getByRole("link", { name: "Xem và chốt" }).getAttribute("href")).toBe(
      `/cod/statements/${S_MATCHED}`,
    );
    expect(within(confirmed).getByText("Đã chốt 18/11")).toBeTruthy();
    expect(within(confirmed).getByRole("link", { name: "Xem" })).toBeTruthy();
  });

  it("Nhập lại mở hộp nhập có sẵn lỗi cũ; nút Nhập bảng kê mở hộp trống", async () => {
    renderAt("/cod/statements");
    const failed = (await rows())[1];
    if (failed === undefined) throw new Error("thiếu dòng");
    fireEvent.click(within(failed).getByRole("button", { name: "Nhập lại" }));
    const dlg = await screen.findByRole("dialog", { name: "Nhập bảng kê COD" });
    expect(
      within(dlg).getByText('Tệp lỗi: thiếu cột "Tiền thu hộ". Kiểm lại định dạng đã chọn hoặc dùng mẫu chung ELIMAZ.'),
    ).toBeTruthy();
  });

  it("báo hiệu cod_statement.changed: đọc lại danh sách (đang phân tích → chờ chốt)", async () => {
    const { qc } = renderAt("/cod/statements");
    await rows();
    fake.statements = fake.statements.map((s) => (s.status === "parsing" ? statement({ id: s.id }) : s));
    onRealtimeChanged(qc, { topic: "cod_statement.changed", entity: { type: "cod_statement", id: "x" } });
    await waitFor(() => {
      expect(screen.getAllByRole("link", { name: "Xem và chốt" })).toHaveLength(2);
    });
    expect(screen.queryByRole("progressbar")).toBeNull();
  });

  it("không có quyền nhập (cash.statements.import hay files.upload): không có nút Nhập, Nhập lại", async () => {
    fake.grants = grantsOf("cash.statements.import");
    renderAt("/cod/statements");
    await rows();
    expect(screen.queryByRole("button", { name: "Nhập bảng kê" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Nhập lại" })).toBeNull();
  });

  it("0 bảng kê: trạng thái rỗng; API lỗi: hộp lỗi, không vẽ như rỗng", async () => {
    fake.statements = [];
    renderAt("/cod/statements");
    expect(await screen.findByText("Chưa có bảng kê nào")).toBeTruthy();
    cleanup();
    fake.failReads = [/cod-statements$/];
    renderAt("/cod/statements");
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.queryByText("Chưa có bảng kê nào")).toBeNull();
  });

  it("tên tài khoản 60 ký tự không dấu cách vẫn nằm trong ô (ngắt chữ)", async () => {
    const long = "A".repeat(60);
    fake.statements = [statement({ id: S_FAILED, carrierAccountName: long })];
    renderAt("/cod/statements");
    const name = await screen.findByText(long);
    expect(name.closest(".who-c")).not.toBeNull();
  });
});
