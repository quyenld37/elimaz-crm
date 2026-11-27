// @vitest-environment jsdom
// Hộp Nhập bảng kê COD (mockup docs/mockups/m3/doi-soat-cod.html trạng thái B; HV30; TI02, TI04). Kế hoạch M3 Task
// 137, Step 1: nhập tệp trùng → câu "Tệp này đã nhập ngày … (bảng kê …)" kèm liên kết bảng kê cũ.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ImportDialog } from "./ImportDialog";
import { ACC_SPX, ACC_VTP, FILE, S_CONFIRMED, installFake, okPut, statement, stubDialog } from "./codTestkit";

const put = vi.fn(okPut);
let fake = installFake();
beforeEach(() => {
  put.mockClear();
  stubDialog();
  fake = installFake();
});
afterEach(() => {
  vi.unstubAllGlobals();
  cleanup();
});

function show(props: { accountId?: string; problem?: string } = {}, onClose = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      { path: "/cod/statements", element: <ImportDialog onClose={onClose} put={put} {...props} /> },
      { path: "/cod/statements/:id", element: <p>Trang bảng kê</p> },
    ],
    { initialEntries: ["/cod/statements"] },
  );
  render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { dlg: screen.getByRole("dialog", { name: "Nhập bảng kê COD" }), onClose, router };
}

const csv = (name = "BK_VTP_08-14_11.csv", size = 86_016) => new File([new Uint8Array(size)], name, { type: "" });

async function chooseFile(dlg: HTMLElement, f = csv()) {
  fireEvent.change(within(dlg).getByLabelText("Chọn tệp"), { target: { files: [f] } });
  await within(dlg).findByText(/đã tải lên/);
}

describe("Nhập bảng kê COD", () => {
  it("tài khoản hãng (đã thu hồi ghi rõ), hai định dạng, gợi ý; Nhập khóa tới khi có tệp", async () => {
    const { dlg } = show();
    const acc = await within(dlg).findByRole("combobox", { name: /^Tài khoản hãng \*/ });
    await within(acc).findByRole("option", { name: "Viettel Post — Kho chính" });
    const opts = within(acc)
      .getAllByRole("option")
      .map((o) => o.textContent);
    expect(opts).toEqual(["Viettel Post — Kho chính", "SPX — Kho chính (đã thu hồi)"]);
    const fmt = within(dlg).getByRole("combobox", { name: /^Định dạng/ });
    expect(
      within(fmt)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Mặc định của hãng", "Mẫu chung ELIMAZ"]);
    expect(within(dlg).getByText("Mỗi dòng chỉ khớp với vận đơn của đúng tài khoản này.")).toBeTruthy();
    expect(within(dlg).getByRole("button", { name: "Nhập" }).hasAttribute("disabled")).toBe(true);
    // Hợp đồng: limit tối đa 100 (quá thì máy chủ trả 400).
    expect(fake.accountCalls.at(-1)?.get("limit")).toBe("100");
  });

  it("tải tệp CSV lên (cod_statement, text/csv) rồi Nhập gửi tài khoản, tệp, định dạng với khóa chống trùng; xong thì đóng", async () => {
    fake.replies.push({ status: 202, body: statement({ status: "imported" }) });
    const { dlg, onClose } = show();
    await within(dlg).findByRole("option", { name: "Viettel Post — Kho chính" });
    await chooseFile(dlg);
    expect(
      within(dlg)
        .getByText(/BK_VTP_08-14_11\.csv/)
        .closest(".oitem")?.textContent,
    ).toContain("84 KB");
    fireEvent.change(within(dlg).getByRole("combobox", { name: /^Định dạng/ }), { target: { value: "elimaz_v1" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Nhập" }));
    await waitFor(() => {
      expect(onClose).toHaveBeenCalled();
    });
    const up = fake.writes.find((w) => w.path === "/api/v1/files/uploads");
    expect(up?.body).toMatchObject({ purpose: "cod_statement", contentType: "text/csv", sizeBytes: 86_016 });
    expect(put).toHaveBeenCalledTimes(1);
    const imp = fake.writes.find((w) => w.path === "/api/v1/cod-statements");
    expect(imp?.body).toEqual({ carrierAccountId: ACC_VTP, fileId: FILE, format: "elimaz_v1" });
    expect(imp?.key).toMatch(/^[0-9a-f-]{36}$/);
  });

  it("TI04: tệp đã nhập → câu có ngày, tên bảng kê, kỳ; nút Xem bảng kê đó mở bảng kê cũ; hộp không đóng", async () => {
    fake.replies.push({
      status: 409,
      body: { error: "statement_duplicate", request_id: "t", statementId: S_CONFIRMED },
    });
    const { dlg, onClose, router } = show();
    await within(dlg).findByRole("option", { name: "Viettel Post — Kho chính" });
    await chooseFile(dlg);
    fireEvent.click(within(dlg).getByRole("button", { name: "Nhập" }));
    const box = await within(dlg).findByText(/Tệp này đã nhập ngày 18\/11/);
    expect(box.closest(".box")?.textContent).toBe(
      "Tệp này đã nhập ngày 18/11 (bảng kê SPX — Kho chính, kỳ 01/11–07/11). Không nhập lần hai.Xem bảng kê đó",
    );
    fireEvent.click(within(dlg).getByRole("link", { name: "Xem bảng kê đó" }));
    expect(router.state.location.pathname).toBe(`/cod/statements/${S_CONFIRMED}`);
    expect(onClose).toHaveBeenCalled();
  });

  it("tệp không phải .xlsx/.csv hay quá 10 MB: báo ngay, không tải lên", async () => {
    const { dlg } = show();
    fireEvent.change(within(dlg).getByLabelText("Chọn tệp"), {
      target: { files: [new File(["a"], "bang-ke.pdf", { type: "application/pdf" })] },
    });
    expect(await within(dlg).findByText("Chỉ nhận tệp Excel (.xlsx) hoặc CSV, tối đa 10 MB.")).toBeTruthy();
    fireEvent.change(within(dlg).getByLabelText("Chọn tệp"), {
      target: { files: [csv("to.csv", 10 * 1024 * 1024 + 1)] },
    });
    expect(within(dlg).getByText("Chỉ nhận tệp Excel (.xlsx) hoặc CSV, tối đa 10 MB.")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });

  it("Nhập lại từ dòng lỗi: chọn sẵn tài khoản của bảng kê, hiện lỗi cũ kèm cách sửa", async () => {
    const { dlg } = show({ accountId: ACC_SPX, problem: 'Tệp lỗi: thiếu cột "Tiền thu hộ".' });
    const acc = await within(dlg).findByRole("combobox", { name: /^Tài khoản hãng \*/ });
    await waitFor(() => {
      expect(acc).toHaveProperty("value", ACC_SPX);
    });
    expect(within(dlg).getByText('Tệp lỗi: thiếu cột "Tiền thu hộ".')).toBeTruthy();
  });

  it("không có quyền xem tài khoản hãng (403): câu riêng, Nhập khóa", async () => {
    fake.accounts = "forbidden";
    const { dlg } = show();
    expect(await within(dlg).findByText(/chưa có quyền xem tài khoản hãng/)).toBeTruthy();
    expect(within(dlg).getByRole("button", { name: "Nhập" }).hasAttribute("disabled")).toBe(true);
  });
});
