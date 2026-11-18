// @vitest-environment jsdom
// Màn Nhà cung cấp (kế hoạch M1 Task 327; mockup docs/mockups/m1/kho-nha-cung-cap.html, trạng thái A–D): danh sách,
// hiện cả đã lưu trữ, thêm (tên trùng, số điện thoại sai), sửa khi người khác vừa sửa, lưu trữ có xác nhận, dùng lại,
// trống; nút ghi chỉ có khi có inventory.suppliers.manage.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Me } from "../../api";
import { queryKey } from "../../app/queryClient";
import { Suppliers } from "./Suppliers";
import {
  TEXT_BAD_PHONE,
  TEXT_CONFLICT_DETAIL,
  TEXT_CONFLICT_TITLE,
  TEXT_EMPTY,
  TEXT_NAME_TAKEN,
} from "./supplierRules";
import { meWith, stubDialog, stubSupplierApi, sup, type FakeSuppliers } from "./supplierTestkit";

const NAME_200 =
  "Công ty TNHH Sản xuất Thương mại và Dịch vụ May mặc Xuất khẩu Thanh Bình Hưng Thịnh Phát, chi nhánh Khu công nghiệp Phố Nối A, tỉnh Hưng Yên, xưởng số 3 chuyên hàng lụa và voan cao cấp cho thương hiệu";
const MANAGER = meWith("inventory.read", "inventory.suppliers.manage");
const READER = meWith("inventory.read");

let fake: FakeSuppliers;

beforeEach(() => {
  stubDialog();
  fake = stubSupplierApi([
    sup(NAME_200, { phone: "0912345678", note: "Giao thứ 3 hằng tuần" }),
    sup("Xưởng Hoa"),
    sup("Xưởng may Hoa Sen", { phone: "+842439361234", note: "Hàng voan, đặt trước 10 ngày", version: 4 }),
    sup("Vải Ninh Hiệp cô Thủy", { phone: "0987000111", note: "Ngừng lấy từ 09/2026", status: "archived" }),
  ]);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderScreen(me: Me = MANAGER) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  // Như khung: /api/v1/me đã có trong bộ đệm, không tải lại.
  qc.setQueryDefaults(queryKey("identity.me"), { staleTime: Infinity });
  qc.setQueryData(queryKey("identity.me"), me);
  return render(
    <QueryClientProvider client={qc}>
      <Suppliers />
    </QueryClientProvider>,
  );
}

const rowOf = async (name: string) => {
  const row = (await screen.findByText(name)).closest("tr");
  if (row === null) throw new Error(`không có dòng ${name}`);
  return within(row);
};

describe("Suppliers", () => {
  it("danh sách đang dùng; số di động hiện theo nhóm; trống hiện —; bật hiện cả đã lưu trữ", async () => {
    renderScreen();
    const first = await rowOf(NAME_200);
    expect(first.getByText("0912 345 678")).toBeTruthy();
    expect(first.getByText("Đang dùng")).toBeTruthy();
    const hoa = await rowOf("Xưởng Hoa");
    expect(hoa.getAllByText("—")).toHaveLength(2);
    expect(screen.queryByText("Vải Ninh Hiệp cô Thủy")).toBeNull();
    expect(fake.reads[0]?.get("includeArchived")).toBeNull();

    fireEvent.click(screen.getByRole("checkbox", { name: "Hiện cả đã lưu trữ" }));
    const old = await rowOf("Vải Ninh Hiệp cô Thủy");
    expect(old.getByText("Đã lưu trữ")).toBeTruthy();
    expect(old.getByRole("button", { name: "Dùng lại" })).toBeTruthy();
    expect(fake.reads.at(-1)?.get("includeArchived")).toBe("true");
  });

  it("tìm gửi q sau khi ngừng gõ; không khớp → câu trống riêng", async () => {
    renderScreen();
    await rowOf("Xưởng Hoa");
    fireEvent.change(screen.getByRole("searchbox", { name: "Tìm nhà cung cấp" }), { target: { value: "zzz" } });
    expect(await screen.findByText("Không có nhà cung cấp nào khớp")).toBeTruthy();
    expect(fake.reads.at(-1)?.get("q")).toBe("zzz");
  });

  it("thêm: số điện thoại sai báo ngay dưới ô, không gửi; tên trùng báo dưới ô Tên; đúng thì gửi và đóng", async () => {
    renderScreen();
    await rowOf("Xưởng Hoa");
    fireEvent.click(screen.getByRole("button", { name: "Thêm nhà cung cấp" }));
    const dlg = within(screen.getByRole("dialog", { name: "Thêm nhà cung cấp" }));
    fireEvent.change(dlg.getByLabelText("Tên *"), { target: { value: "Xưởng Hoa" } });
    fireEvent.change(dlg.getByLabelText("Điện thoại"), { target: { value: "09123" } });
    expect(dlg.getByText("0 / 1.000 ký tự")).toBeTruthy();
    fireEvent.click(dlg.getByRole("button", { name: "Lưu" }));
    expect(await dlg.findByText(TEXT_BAD_PHONE)).toBeTruthy();
    expect(fake.writes).toHaveLength(0);

    fireEvent.change(dlg.getByLabelText("Điện thoại"), { target: { value: "0911 222 333" } });
    fake.failNext = { status: 409, code: "supplier_name_taken" };
    fireEvent.click(dlg.getByRole("button", { name: "Lưu" }));
    expect(await dlg.findByText(TEXT_NAME_TAKEN)).toBeTruthy();
    expect(dlg.getByLabelText("Tên *").getAttribute("aria-invalid")).toBe("true");

    fireEvent.change(dlg.getByLabelText("Tên *"), { target: { value: "Xưởng Hoa 2" } });
    fireEvent.change(dlg.getByLabelText("Ghi chú"), { target: { value: "Lụa" } });
    fireEvent.click(dlg.getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(fake.writes.at(-1)).toMatchObject({
      method: "POST",
      body: { name: "Xưởng Hoa 2", phone: "0911 222 333", note: "Lụa" },
    });
    expect(await screen.findByText("Xưởng Hoa 2")).toBeTruthy();
  });

  it("sửa khi người khác vừa sửa: hộp trạng thái C, Tải lại mở lại bản mới", async () => {
    renderScreen();
    const row = await rowOf("Xưởng may Hoa Sen");
    fireEvent.click(row.getByRole("button", { name: "Sửa" }));
    const dlg = within(screen.getByRole("dialog", { name: "Sửa nhà cung cấp" }));
    expect(dlg.getByLabelText<HTMLInputElement>("Tên *").value).toBe("Xưởng may Hoa Sen");
    // Người khác sửa trước: version trên máy chủ thành 5.
    fake.list = fake.list.map((s) => (s.name === "Xưởng may Hoa Sen" ? { ...s, note: "Bản mới", version: 5 } : s));
    fireEvent.change(dlg.getByLabelText("Ghi chú"), { target: { value: "Của tôi" } });
    fireEvent.click(dlg.getByRole("button", { name: "Lưu" }));
    expect(await dlg.findByText(TEXT_CONFLICT_TITLE)).toBeTruthy();
    expect(dlg.getByText(TEXT_CONFLICT_DETAIL)).toBeTruthy();
    expect(fake.writes.at(-1)).toMatchObject({ method: "PATCH", body: { version: 4, note: "Của tôi" } });
    fireEvent.click(dlg.getByRole("button", { name: "Tải lại" }));
    await waitFor(() => {
      expect(
        within(screen.getByRole("dialog", { name: "Sửa nhà cung cấp" })).getByLabelText<HTMLTextAreaElement>("Ghi chú")
          .value,
      ).toBe("Bản mới");
    });
    expect(screen.queryByText(TEXT_CONFLICT_TITLE)).toBeNull();
  });

  it("lưu trữ có xác nhận, gửi status archived với version; dùng lại gửi active", async () => {
    renderScreen();
    const row = await rowOf("Xưởng may Hoa Sen");
    fireEvent.click(row.getByRole("button", { name: "Lưu trữ" }));
    const dlg = within(screen.getByRole("dialog", { name: 'Lưu trữ "Xưởng may Hoa Sen"?' }));
    expect(dlg.getByText(/không còn hiện trong ô chọn của phiếu nhập mới/)).toBeTruthy();
    fireEvent.click(dlg.getByRole("button", { name: "Lưu trữ" }));
    await waitFor(() => {
      expect(screen.queryByText("Xưởng may Hoa Sen")).toBeNull();
    });
    expect(fake.writes.at(-1)).toMatchObject({ method: "PATCH", body: { version: 4, status: "archived" } });

    fireEvent.click(screen.getByRole("checkbox", { name: "Hiện cả đã lưu trữ" }));
    const old = await rowOf("Vải Ninh Hiệp cô Thủy");
    fireEvent.click(old.getByRole("button", { name: "Dùng lại" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({ method: "PATCH", body: { version: 1, status: "active" } });
    });
  });

  it("lưu trữ khi người khác vừa sửa: hộp trạng thái C trên trang", async () => {
    renderScreen();
    const row = await rowOf("Xưởng Hoa");
    fake.list = fake.list.map((s) => (s.name === "Xưởng Hoa" ? { ...s, version: 2 } : s));
    fireEvent.click(row.getByRole("button", { name: "Lưu trữ" }));
    fireEvent.click(
      within(screen.getByRole("dialog", { name: 'Lưu trữ "Xưởng Hoa"?' })).getByRole("button", { name: "Lưu trữ" }),
    );
    expect(await screen.findByText(TEXT_CONFLICT_TITLE)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tải lại" }));
    await waitFor(() => {
      expect(screen.queryByText(TEXT_CONFLICT_TITLE)).toBeNull();
    });
  });

  it("chỉ có quyền xem: không có nút Thêm, Sửa, Lưu trữ", async () => {
    renderScreen(READER);
    const row = await rowOf("Xưởng Hoa");
    expect(row.queryByRole("button")).toBeNull();
    expect(screen.queryByRole("button", { name: "Thêm nhà cung cấp" })).toBeNull();
  });

  it("chưa có nhà cung cấp nào: trạng thái D", async () => {
    fake.list = [];
    renderScreen();
    expect(await screen.findByRole("heading", { name: TEXT_EMPTY })).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "Thêm nhà cung cấp" })).toHaveLength(2);
  });
});
