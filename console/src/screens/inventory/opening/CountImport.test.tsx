// @vitest-environment jsdom
// Nhập số đếm từ Excel của mở sổ (Task 273; mockup trạng thái D): lỗi từng dòng theo mã, tệp sai, đã cộng, đọc lỗi.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StocktakeCountImport } from "../../../api";
import { CountImport } from "./CountImport";
import { ST, grantsAll, newFake, stub, userEvent, type Fake } from "./openingTestkit";

vi.mock("../../products/useUpload", () => ({
  xhrPut: vi.fn(() => Promise.resolve()),
}));

let fake: Fake;
beforeEach(() => {
  fake = stub(newFake(grantsAll("inventory.stocktake", "files.upload")));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const IMP = "0195f3b2-0000-7000-8000-0000000e0001";
const FILE = "0195f3b2-0000-7000-8000-0000000f0001";
const imp = (p: Partial<StocktakeCountImport>): StocktakeCountImport => ({
  id: IMP,
  stocktakeId: ST,
  mode: "replace",
  status: "done",
  appliedRows: 2996,
  errorRows: [],
  createdAt: "2026-11-30T13:00:00Z",
  ...p,
});

/** Ba câu trả lời của chuỗi tải: createUpload, completeUpload, createStocktakeCountImport. */
function queueUpload(importReply: { status: number; body: unknown }) {
  fake.replies.push(
    { status: 201, body: { fileId: FILE, putUrl: "https://kho/put", headers: {}, expiresAt: "2026-11-30T14:00:00Z" } },
    { status: 200, body: { fileId: FILE, status: "ready" } },
    importReply,
  );
}

function renderIt() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <CountImport stocktakeId={ST} />
    </QueryClientProvider>,
  );
}

async function pick(name: string, size = 1000) {
  const input = await screen.findByLabelText("Tệp số đếm .xlsx");
  fireEvent.change(input, { target: { files: [new File([new Uint8Array(size)], name)] } });
}

describe("CountImport", () => {
  it("mặc định chế độ 'Đặt bằng số trong tệp'; tải lên rồi gửi fileId và mode replace kèm khóa", async () => {
    queueUpload({ status: 202, body: imp({ status: "queued", appliedRows: 0 }) });
    fake.imports.set(IMP, imp({}));
    renderIt();
    expect(screen.getByRole("radio", { name: /Đặt bằng số trong tệp/ })).toHaveProperty("checked", true);
    await pick("dem-khu-B.xlsx");
    await waitFor(() => {
      expect(fake.writes).toHaveLength(3);
    });
    expect(fake.writes[0]?.body).toMatchObject({ purpose: "inventory_count_import", fileName: "dem-khu-B.xlsx" });
    expect(fake.writes[2]?.path).toBe(`/api/v1/inventory/stocktakes/${ST}/count-imports`);
    expect(fake.writes[2]?.body).toEqual({ fileId: FILE, mode: "replace" });
    expect(fake.writes.every((w) => w.key !== null)).toBe(true);
    expect(await screen.findByText(/2\.996 dòng/)).toBeTruthy();
  });

  it("chọn Cộng thêm → mode add", async () => {
    queueUpload({ status: 202, body: imp({ status: "queued", mode: "add" }) });
    fake.imports.set(IMP, imp({ mode: "add" }));
    renderIt();
    await userEvent.click(screen.getByRole("radio", { name: /Cộng thêm vào số đã đếm/ }));
    await pick("dem-khu-A.xlsx");
    await waitFor(() => {
      expect(fake.writes).toHaveLength(3);
    });
    expect(fake.writes[2]?.body).toEqual({ fileId: FILE, mode: "add" });
  });

  it("lỗi từng dòng theo mã: số dòng và câu, không nội dung ô", async () => {
    queueUpload({ status: 202, body: imp({ status: "queued" }) });
    fake.imports.set(
      IMP,
      imp({
        errorRows: [
          { row: 12, code: "unknown_code" },
          { row: 40, code: "ambiguous_code" },
          { row: 58, code: "not_integer" },
          { row: 60, code: "negative" },
          { row: 301, code: "formula" },
        ],
      }),
    );
    renderIt();
    await pick("dem-khu-B.xlsx");
    expect(await screen.findByText("5 dòng lỗi")).toBeTruthy();
    const table = screen.getByRole("table");
    const rows = within(table)
      .getAllByRole("row")
      .slice(1)
      .map((r) => r.textContent);
    expect(rows).toEqual([
      "12Không thấy mã trong danh mục",
      "40Mã có ở 2 người bán: ghi SKU thay barcode",
      "58Số đếm không phải số nguyên",
      "60Số đếm âm",
      "301Ô có công thức: dán giá trị",
    ]);
  });

  it("tệp không phải .xlsx → báo ngay, không gọi máy chủ", async () => {
    renderIt();
    await pick("dem-khu-A.csv");
    expect(await screen.findByText("Không nhận tệp dem-khu-A.csv.")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });

  it("tệp quá 5 MB → báo ngay", async () => {
    renderIt();
    await pick("dem-khu-A.xlsx", 5 * 1024 * 1024 + 1);
    expect(await screen.findByText("Không nhận tệp dem-khu-A.xlsx.")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });

  it("409 import_already_applied → câu báo cộng trùng, gợi ý chế độ Đặt bằng số", async () => {
    queueUpload({ status: 409, body: { error: "import_already_applied", request_id: "t" } });
    renderIt();
    await userEvent.click(screen.getByRole("radio", { name: /Cộng thêm vào số đã đếm/ }));
    await pick("dem-khu-A.xlsx");
    expect(await screen.findByText("Tệp này đã được cộng vào phiên rồi")).toBeTruthy();
    expect(screen.getByText(/Đặt bằng số trong tệp/, { selector: "span:not(.bold)" })).toBeTruthy();
  });

  it("máy chủ đọc thất bại (status failed) → Không đọc được tệp, không số nào đổi", async () => {
    queueUpload({ status: 202, body: imp({ status: "queued" }) });
    fake.imports.set(IMP, imp({ status: "failed", appliedRows: 0 }));
    renderIt();
    await pick("dem-khu-C.xlsx");
    expect(await screen.findByText("Không đọc được dem-khu-C.xlsx.")).toBeTruthy();
    expect(screen.getByText(/Không số nào được đổi/)).toBeTruthy();
  });

  it("đang đọc tệp (queued, running) → chip Đang đọc tệp", async () => {
    queueUpload({ status: 202, body: imp({ status: "queued", appliedRows: 0 }) });
    fake.imports.set(IMP, imp({ status: "running", appliedRows: 0 }));
    renderIt();
    await pick("dem-khu-A.xlsx");
    expect(await screen.findByText("Đang đọc tệp")).toBeTruthy();
  });
});
