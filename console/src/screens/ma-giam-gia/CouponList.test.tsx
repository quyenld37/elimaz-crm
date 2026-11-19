// @vitest-environment jsdom
// Màn Mã giảm giá: danh sách, tạo mã, tạo đợt (mockup docs/mockups/m2/ma-giam-gia.html trạng thái A–C). Kế hoạch M2
// Task M2-30 Step 1: nhập 10% → gửi value 1000; tạo đợt 500 mã → hiện tiến độ và tải được danh sách mã.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BATCH, batch, coupon, grantsOf, installFake, renderAt, stubDialog, type Fake } from "./couponTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
  fake.coupons = [
    coupon(),
    coupon({
      id: "c2",
      code: "FLASH50",
      name: "Live tối 15/11",
      discountKind: "fixed",
      value: 50_000,
      maxDiscount: undefined,
      minSubtotal: 0,
      perCustomerLimit: 1,
      maxUses: 100,
      usedCount: 100,
      remainingUses: 0,
    }),
    coupon({
      id: "c3",
      code: "SN-7KQ2M9XA",
      name: "Sinh nhật tháng 12",
      batchId: BATCH,
      bound: true,
      usedCount: 0,
      minSubtotal: 0,
    }),
    coupon({ id: "c4", code: "HE2026", name: "Xả hè", validTo: "2026-08-31T16:59:00Z", maxUses: undefined }),
    coupon({ id: "c5", code: "NHAPNHAM", name: "Tạo nhầm", status: "disabled", usedCount: 0 }),
  ];
  fake.batches.set(BATCH, batch());
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const field = (dlg: HTMLElement, name: string) => within(dlg).getByLabelText(name);
const type = (el: HTMLElement, v: string) => {
  fireEvent.change(el, { target: { value: v } });
};

describe("Danh sách mã", () => {
  it("mỗi trạng thái một chip; mã thuộc đợt ghi tên đợt; mã dẫn tới chi tiết", async () => {
    renderAt("/ma-giam-gia");
    const row = (code: string) => screen.findByRole("row", { name: new RegExp(code) });
    const thu = await row("THU10");
    expect(within(thu).getByText("Đang chạy")).toBeTruthy();
    expect(within(thu).getByText("10%, tối đa 150.000 ₫")).toBeTruthy();
    expect(within(thu).getByText("87 / 200")).toBeTruthy();
    expect(within(thu).getByRole("link", { name: "THU10" }).getAttribute("href")).toBe("/ma-giam-gia/" + coupon().id);
    expect(within(await row("FLASH50")).getByText("Hết lượt")).toBeTruthy();
    expect(within(await row("FLASH50")).getByText("Mọi đơn · 1 lượt mỗi khách")).toBeTruthy();
    expect(within(await row("HE2026")).getByText("Hết hạn")).toBeTruthy();
    expect(within(await row("NHAPNHAM")).getByText("Đã tắt")).toBeTruthy();
    const sn = await row("SN-7KQ2M9XA");
    expect(await within(sn).findByText("Đợt: Sinh nhật tháng 12 (500 mã)")).toBeTruthy();
    expect(within(sn).getByText("Dành riêng một khách")).toBeTruthy();
  });

  it("tìm và lọc gửi q, status, batchId lên API", async () => {
    renderAt("/ma-giam-gia");
    await screen.findByRole("row", { name: /THU10/ });
    type(screen.getByRole("searchbox", { name: "Tìm mã" }), "thu");
    fireEvent.change(screen.getByRole("combobox", { name: "Trạng thái" }), { target: { value: "disabled" } });
    await waitFor(() => {
      const last = fake.listCalls.at(-1);
      expect(last?.get("q")).toBe("thu");
      expect(last?.get("status")).toBe("disabled");
    });
    await screen.findByRole("option", { name: "Sinh nhật tháng 12" });
    fireEvent.change(screen.getByRole("combobox", { name: "Đợt mã" }), { target: { value: BATCH } });
    await waitFor(() => {
      expect(fake.listCalls.at(-1)?.get("batchId")).toBe(BATCH);
    });
  });

  it("không có promotion.manage: không có nút tạo", async () => {
    fake.grants = grantsOf();
    renderAt("/ma-giam-gia");
    await screen.findByRole("row", { name: /THU10/ });
    expect(screen.queryByRole("button", { name: "Tạo mã" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Tạo đợt mã" })).toBeNull();
  });

  it("lỗi API của danh sách hiện câu riêng", async () => {
    vi.mocked(fetch).mockImplementation(() =>
      Promise.resolve(Response.json({ error: "internal", request_id: "t" }, { status: 500 })),
    );
    renderAt("/ma-giam-gia");
    expect(await screen.findByText(/Hệ thống gặp lỗi/)).toBeTruthy();
  });
});

async function openCreate() {
  renderAt("/ma-giam-gia");
  fireEvent.click(await screen.findByRole("button", { name: "Tạo mã" }));
  return screen.getByRole("dialog", { name: "Tạo mã" });
}

function fillTerms(dlg: HTMLElement) {
  type(field(dlg, "Giảm (%)"), "10");
  type(field(dlg, "Giảm tối đa (đồng)"), "200.000");
  type(field(dlg, "Đơn tối thiểu (đồng)"), "300.000");
  type(field(dlg, "Từ"), "2027-01-20T00:00");
  type(field(dlg, "Đến"), "2027-02-10T23:59");
}

describe("Tạo mã", () => {
  it("nhập 10% → gửi value 1000 (phần vạn); tiền theo đồng; kênh đã chọn; xong mở chi tiết", async () => {
    const dlg = await openCreate();
    type(field(dlg, "Mã"), "TET2027");
    type(field(dlg, "Tên (cho nhân viên)"), "Tết 2027 khách cũ");
    fillTerms(dlg);
    type(field(dlg, "Tổng lượt (để trống: không giới hạn)"), "300");
    type(field(dlg, "Lượt mỗi khách"), "1");
    fireEvent.click(within(dlg).getByRole("checkbox", { name: "Nhập tay" }));
    fireEvent.click(within(dlg).getByRole("checkbox", { name: "Facebook" }));
    fireEvent.click(within(dlg).getByRole("button", { name: "Tạo mã" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    const w = fake.writes[0];
    expect(w?.path).toBe("/api/v1/coupons");
    expect(w?.key).toBeTruthy();
    expect(w?.body).toEqual({
      code: "TET2027",
      name: "Tết 2027 khách cũ",
      discountKind: "percent",
      value: 1000,
      maxDiscount: 200_000,
      minSubtotal: 300_000,
      validFrom: "2027-01-19T17:00:00.000Z",
      validTo: "2027-02-10T16:59:00.000Z",
      maxUses: 300,
      perCustomerLimit: 1,
      channels: ["manual", "facebook"],
      releaseOnCancel: true,
    });
    expect(await screen.findByRole("heading", { name: "TET2027" })).toBeTruthy();
    expect(fake.listCalls.length).toBeGreaterThan(0);
  });

  it("số tiền: value theo đồng; mã trống thì không gửi code (máy chủ tự sinh)", async () => {
    const dlg = await openCreate();
    type(field(dlg, "Tên (cho nhân viên)"), "Giảm thẳng");
    fireEvent.click(within(dlg).getByRole("button", { name: "Số tiền" }));
    type(field(dlg, "Giảm (đồng)"), "50.000");
    type(field(dlg, "Từ"), "2027-01-20T00:00");
    type(field(dlg, "Đến"), "2027-02-10T23:59");
    fireEvent.click(within(dlg).getByRole("button", { name: "Tạo mã" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toMatchObject({ discountKind: "fixed", value: 50_000, minSubtotal: 0 });
    expect(fake.writes[0]?.body).not.toHaveProperty("code");
    expect(fake.writes[0]?.body).not.toHaveProperty("maxDiscount");
  });

  it("ô sai báo ngay dưới ô, không gửi", async () => {
    const dlg = await openCreate();
    type(field(dlg, "Mã"), "a b");
    type(field(dlg, "Giảm (%)"), "150");
    fireEvent.click(within(dlg).getByRole("button", { name: "Tạo mã" }));
    expect(within(dlg).getByText("Điền tên mã cho nhân viên")).toBeTruthy();
    expect(within(dlg).getByText("Chữ, số, dấu -; 3–32 ký tự")).toBeTruthy();
    expect(within(dlg).getByText("Từ 0,01 đến 100%")).toBeTruthy();
    expect(within(dlg).getByText("Chọn ngày giờ bắt đầu")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });

  it("mã trùng → 409 code_taken: báo theo mã đã nhập", async () => {
    fake.replies.push({ status: 409, body: { error: "code_taken", request_id: "t" } });
    const dlg = await openCreate();
    type(field(dlg, "Mã"), "TET2027");
    type(field(dlg, "Tên (cho nhân viên)"), "Tết");
    fillTerms(dlg);
    fireEvent.click(within(dlg).getByRole("button", { name: "Tạo mã" }));
    expect(await within(dlg).findByText("Mã TET2027 đã có.")).toBeTruthy();
    expect(within(dlg).getByText(/Chọn mã khác hoặc để trống để tự sinh/)).toBeTruthy();
  });
});

describe("Tạo đợt mã", () => {
  it("500 mã: đang sinh hiện tiến độ; xong tải được tệp đủ 500 mã", async () => {
    let release: () => void = vi.fn();
    fake.hold = new Promise((r) => {
      release = r;
    });
    fake.batchCodes = Array.from({ length: 500 }, (_, i) => `SN-${String(i).padStart(8, "0")}`);
    const blobs: Blob[] = [];
    vi.stubGlobal(
      "URL",
      Object.assign(URL, { createObjectURL: (b: Blob) => (blobs.push(b), "blob:x"), revokeObjectURL: vi.fn() }),
    );
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(vi.fn());

    renderAt("/ma-giam-gia");
    fireEvent.click(await screen.findByRole("button", { name: "Tạo đợt mã" }));
    const dlg = screen.getByRole("dialog", { name: "Tạo đợt mã" });
    type(field(dlg, "Tên đợt"), "Sinh nhật tháng 12");
    type(field(dlg, "Tiền tố"), "sn-");
    type(field(dlg, "Số mã"), "500");
    fillTerms(dlg);
    fireEvent.click(within(dlg).getByRole("button", { name: "Tạo 500 mã" }));

    expect(await screen.findByText("Đang sinh 500 mã…")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Tải danh sách mã (.csv)" }).hasAttribute("disabled")).toBe(true);
    expect(fake.writes[0]?.body).toMatchObject({
      name: "Sinh nhật tháng 12",
      prefix: "SN",
      quantity: 500,
      template: { name: "Sinh nhật tháng 12", discountKind: "percent", value: 1000, maxDiscount: 200_000 },
    });
    expect(fake.writes[0]?.body.template).not.toHaveProperty("maxUses");

    release();
    expect(await screen.findByText("500 / 500")).toBeTruthy();
    const dl = screen.getByRole("button", { name: "Tải danh sách mã (.csv)" });
    await waitFor(() => {
      expect(dl.hasAttribute("disabled")).toBe(false);
    });
    fireEvent.click(dl);
    await waitFor(() => {
      expect(click).toHaveBeenCalled();
    });
    const text = await blobs[0]?.text();
    const lines = (text ?? "").trim().split("\r\n");
    expect(lines).toHaveLength(501);
    expect(lines[1]).toBe("SN-00000000");
    expect(lines[500]).toBe("SN-00000499");
    const pages = fake.listCalls.filter((q) => q.get("batchId") === BATCH);
    expect(pages.map((q) => q.get("limit"))).toEqual(["100", "100", "100", "100", "100"]);
    click.mockRestore();
  });

  it("số mã ngoài 1–100.000 hay tiền tố sai: báo dưới ô, không gửi", async () => {
    renderAt("/ma-giam-gia");
    fireEvent.click(await screen.findByRole("button", { name: "Tạo đợt mã" }));
    const dlg = screen.getByRole("dialog", { name: "Tạo đợt mã" });
    type(field(dlg, "Tên đợt"), "X");
    type(field(dlg, "Tiền tố"), "SINHNHAT12");
    type(field(dlg, "Số mã"), "100001");
    fillTerms(dlg);
    fireEvent.click(within(dlg).getByRole("button", { name: /^Tạo .* mã$/ }));
    expect(within(dlg).getByText("Từ 1 đến 100.000 mã")).toBeTruthy();
    expect(within(dlg).getByText("Chữ in hoa và số, tối đa 8 ký tự")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });
});
