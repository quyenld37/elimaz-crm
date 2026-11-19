// @vitest-environment jsdom
// Nhập khách từ Excel (mockup docs/mockups/m2/khach-danh-sach.html trạng thái D; HV27). Kế hoạch M2 Task M2-29, Step 1:
// kết quả hiện số dòng lỗi và số dòng, không hiện giá trị ô. Task 70: nhắc định dạng cột SĐT là văn bản.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ImportDialog } from "./ImportDialog";
import { FILE, IMP, installFake, stubDialog, type Fake } from "./khachTestkit";

let fake: Fake;
const put = vi.fn(() => Promise.resolve());
beforeEach(() => {
  stubDialog();
  fake = installFake();
  put.mockClear();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function show(onClose = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <ImportDialog onClose={onClose} put={put} pollMs={10} />
    </QueryClientProvider>,
  );
  return screen.getByRole("dialog", { name: "Nhập khách từ Excel" });
}

const xlsx = (name = "khach-live-thang-11.xlsx", size = 2048) =>
  new File([new Uint8Array(size)], name, {
    type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  });

/** Mở hộp, chọn tệp .xlsx hợp lệ; trả hộp ở bước kết quả. */
async function importFile() {
  fireEvent.change(within(show()).getByLabelText("Chọn tệp .xlsx"), { target: { files: [xlsx()] } });
  return screen.findByRole("dialog", { name: /^Kết quả nhập · / });
}

describe("Nhập khách từ Excel", () => {
  it("bước 1–2: cột của mẫu, nhắc cột SĐT định dạng văn bản, SĐT đã có thì bỏ qua", () => {
    const dlg = show();
    expect(within(dlg).getByRole("button", { name: "Tải tệp mẫu (.xlsx)" })).toBeTruthy();
    expect(within(dlg).getByText(/Cột SĐT để định dạng Văn bản \(Text\)/)).toBeTruthy();
    expect(within(dlg).getByText(/SĐT đã có trong CRM thì bỏ qua dòng đó/)).toBeTruthy();
  });

  it("tệp không phải .xlsx: báo ngay, không tải lên", () => {
    const dlg = show();
    fireEvent.change(within(dlg).getByLabelText("Chọn tệp .xlsx"), {
      target: { files: [new File(["a"], "khach.csv", { type: "text/csv" })] },
    });
    expect(within(dlg).getByText(/Chỉ nhận tệp Excel \.xlsx/)).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });

  it("tải lên rồi nhập: createUpload customer_import, PUT, completeUpload, startCustomerImport; kết quả số đếm và lỗi theo dòng, cột, không giá trị ô", async () => {
    fake.imports = [
      { id: IMP, status: "running", errors: [], createdAt: "2026-11-19T01:00:00Z" },
      {
        id: IMP,
        status: "done",
        totalRows: 1250,
        okRows: 1180,
        skippedRows: 52,
        errorRows: 18,
        errors: [
          { row: 14, column: "SĐT", code: "invalid_phone" },
          { row: 97, column: "Xã/phường", code: "ward_not_in_province" },
          { row: 310, column: "Họ tên", code: "required" },
        ],
        createdAt: "2026-11-19T01:00:00Z",
        finishedAt: "2026-11-19T01:01:00Z",
      },
    ];
    const res = await importFile();
    expect(within(res).getByRole("heading").textContent).toBe("Kết quả nhập · khach-live-thang-11.xlsx");
    expect(fake.writes.map((w) => w.path)).toEqual([
      "/api/v1/files/uploads",
      `/api/v1/files/${FILE}/complete`,
      "/api/v1/customer-imports",
    ]);
    expect(fake.writes[0]?.body).toMatchObject({ purpose: "customer_import", fileName: "khach-live-thang-11.xlsx" });
    expect(fake.writes[2]?.body).toEqual({ fileId: FILE });
    expect(fake.writes.every((w) => w.key !== null)).toBe(true);
    expect(put).toHaveBeenCalledTimes(1);
    await within(res).findByText("1.180");
    const kpi = (label: string) => within(res).getByText(label, { selector: ".kpi p" }).parentElement?.textContent;
    expect(kpi("Đã thêm")).toContain("1.180");
    expect(kpi("Bỏ qua (SĐT đã có)")).toContain("52");
    expect(kpi("Lỗi")).toContain("18");
    expect(kpi("Tổng dòng")).toContain("1.250");
    const rows = within(res).getAllByRole("row").slice(1);
    expect(rows.map((r) => r.textContent)).toEqual([
      "14SĐTSĐT không hợp lệ",
      "97Xã/phườngXã/phường không thuộc tỉnh đã chọn",
      "310Họ tênThiếu ô bắt buộc",
      "…—15 lỗi khác",
    ]);
  });

  it("nhập thất bại cả tệp: câu báo của mã lỗi tệp, không bảng", async () => {
    fake.imports = [
      {
        id: IMP,
        status: "failed",
        errors: [{ row: 0, code: "import_file_invalid" }],
        createdAt: "2026-11-19T01:00:00Z",
      },
    ];
    const res = await importFile();
    await waitFor(() => {
      expect(within(res).getByText(/Tệp không đọc được/)).toBeTruthy();
    });
    expect(within(res).queryByRole("table")).toBeNull();
  });
});
