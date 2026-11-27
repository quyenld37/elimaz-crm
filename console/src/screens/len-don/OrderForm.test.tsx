// @vitest-environment jsdom
// Màn Lên đơn nhanh (kế hoạch M2 Task M2-27; mockup docs/mockups/m2/len-don.html trạng thái A–H).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Order } from "../../api";
import { C1, C3, C4, O_OLD, S_MANUAL, S_ZALO, V1, V2, installFake, order, renderForm, type Fake } from "./orderTestkit";

let fake: Fake;

beforeEach(() => {
  fake = installFake();
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const customerBox = () => screen.getByRole("combobox", { name: "Tìm khách theo đuôi SĐT, tên hoặc mã KH" });
const productBox = () => screen.getByRole("combobox", { name: "Thêm sản phẩm theo mã, tên hoặc barcode" });
const btn = (name: string) => screen.getByRole<HTMLButtonElement>("button", { name });
const confirmBtn = () => btn("Lưu và xác nhận");

async function pickCustomerByKeys(text = "5678", downs = 0) {
  const box = customerBox();
  fireEvent.change(box, { target: { value: text } });
  const list = await screen.findByRole("listbox", { name: "Kết quả tìm khách" });
  await within(list).findByText("Trần Thị Thu Hà");
  for (let i = 0; i < downs; i++) fireEvent.keyDown(box, { key: "ArrowDown" });
  fireEvent.keyDown(box, { key: "Enter" });
}

async function addProduct(sku: string) {
  const box = productBox();
  fireEvent.change(box, { target: { value: sku } });
  const list = await screen.findByRole("listbox", { name: "Kết quả tìm sản phẩm" });
  await within(list).findByText(sku, { exact: false });
  fireEvent.keyDown(box, { key: "Enter" });
  await screen.findByRole("textbox", { name: `Số lượng ${sku}` });
}

async function fillAddress() {
  fireEvent.change(screen.getByRole("textbox", { name: "Người nhận *" }), { target: { value: "Nguyễn Văn Tùng" } });
  fireEvent.change(screen.getByRole("textbox", { name: "SĐT người nhận *" }), { target: { value: "0987 654 321" } });
  const prov = screen.getByRole("combobox", { name: "Tỉnh/thành *" });
  await within(prov).findByRole("option", { name: "Hà Nội" });
  fireEvent.change(prov, { target: { value: "01" } });
  const ward = screen.getByRole("combobox", { name: "Xã/phường *" });
  fireEvent.change(ward, { target: { value: "cau" } });
  await screen.findByRole("option", { name: "Cầu Giấy" });
  fireEvent.keyDown(ward, { key: "Enter" });
  fireEvent.change(screen.getByRole("textbox", { name: "Số nhà, đường *" }), {
    target: { value: "Số 12 ngõ 68 Trần Thái Tông" },
  });
}

/** Chờ nút lưu sáng rồi lưu và xác nhận; trả sau khi màn báo đã tạo đơn. */
async function saveAndWait() {
  await waitFor(() => {
    expect(confirmBtn().disabled).toBe(false);
  });
  fireEvent.click(confirmBtn());
  await screen.findByText("Đã tạo đơn DH0001236.");
}

async function readyOrder() {
  renderForm();
  await pickCustomerByKeys();
  await screen.findByText("KH000231", { selector: ".chip" });
  await addProduct("DAM0123-BE-M");
  await fillAddress();
  await waitFor(() => {
    expect(confirmBtn().disabled).toBe(false);
  });
}

describe("bàn phím (HV03)", () => {
  it('gõ "5678" ở ô khách, ↓ rồi Enter chọn khách thứ hai; gợi ý che giữa SĐT', async () => {
    renderForm();
    const box = customerBox();
    fireEvent.change(box, { target: { value: "5678" } });
    const list = await screen.findByRole("listbox", { name: "Kết quả tìm khách" });
    await within(list).findByText("Trần Thị Thu Hà");
    expect(list.textContent).not.toContain("0912");
    expect(within(list).getAllByText("5678", { selector: "mark" })).toHaveLength(2);
    expect(within(list).getAllByRole("option")[0]?.getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(box, { key: "ArrowDown" });
    expect(within(list).getAllByRole("option")[1]?.getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(box, { key: "Enter" });
    expect(await screen.findByText("KH004410", { selector: ".chip" })).toBeTruthy();
    expect(screen.queryByRole("listbox", { name: "Kết quả tìm khách" })).toBeNull();
  });

  it("dưới 4 số: nhắc gõ ít nhất 4 số cuối, không gọi API", async () => {
    renderForm();
    fireEvent.change(customerBox(), { target: { value: "567" } });
    expect(await screen.findByText("Gõ ít nhất 4 số cuối")).toBeTruthy();
    await new Promise((r) => setTimeout(r, 400)); // quá thời gian chờ gõ (300 ms)
    expect(fake.customerCalls).toEqual([]);
  });

  it("F2 đưa tiêu điểm vào ô khách; F3 (thêm dòng) vào ô tìm sản phẩm", async () => {
    renderForm();
    await screen.findByRole("option", { name: "Nhập tay" });
    fireEvent.keyDown(document.body, { key: "F3" });
    expect(document.activeElement).toBe(productBox());
    fireEvent.keyDown(document.body, { key: "F2" });
    expect(document.activeElement).toBe(customerBox());
  });

  it("Enter trong ô số lượng chỉ sang ô kế (giảm dòng), không gửi đơn", async () => {
    await readyOrder();
    const qty = screen.getByRole("textbox", { name: "Số lượng DAM0123-BE-M" });
    qty.focus();
    fireEvent.keyDown(qty, { key: "Enter" });
    expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "Giảm dòng DAM0123-BE-M (đồng)" }));
    await new Promise((r) => setTimeout(r, 20));
    expect(fake.writes).toHaveLength(0);
  });

  it("Ctrl+Enter lưu và xác nhận; Ctrl+S lưu (mới)", async () => {
    await readyOrder();
    fireEvent.keyDown(document.body, { key: "s", ctrlKey: true });
    await screen.findByText("Đã tạo đơn DH0001236.");
    expect(fake.writes[0]?.body.intent).toBe("new");
    await pickCustomerByKeys();
    await addProduct("TUI0090-NAU");
    await fillAddress();
    fireEvent.keyDown(document.body, { key: "Enter", metaKey: true });
    await waitFor(() => {
      expect(fake.writes).toHaveLength(2);
    });
    expect(fake.writes[1]?.body.intent).toBe("confirmed");
  });
});

describe("chọn khách, sản phẩm, tiền (trạng thái A, B)", () => {
  it("0 dòng, chưa khách: ba nút mờ kèm lý do", () => {
    renderForm();
    expect(confirmBtn().disabled).toBe(true);
    expect(btn("Lưu (mới)").disabled).toBe(true);
    expect(btn("Lưu nháp").disabled).toBe(true);
    expect(screen.getByText("Chưa lưu được: chọn khách và thêm ít nhất một sản phẩm.")).toBeTruthy();
    expect(screen.getByText("Chưa có sản phẩm. Gõ mã hoặc tên ở ô trên, hoặc quét barcode.")).toBeTruthy();
  });

  it("gợi ý sản phẩm hiện giá và số có thể bán của kho mặc định; dòng có tên dài, số có thể bán đỏ khi còn 1–2", async () => {
    renderForm();
    fireEvent.change(productBox(), { target: { value: "AO" } });
    const list = await screen.findByRole("listbox", { name: "Kết quả tìm sản phẩm" });
    expect(await within(list).findByText("350.000 ₫ · có thể bán 1")).toBeTruthy();
    fireEvent.keyDown(productBox(), { key: "Enter" });
    const avail = await screen.findByText("Có thể bán: 1");
    expect(avail.classList.contains("is-low")).toBe(true);
  });

  it("biến thể không có dòng số dư: trang cuối → hết hàng; còn trang sau (nextCursor) → chưa biết, không báo hết", async () => {
    renderForm();
    fireEvent.change(productBox(), { target: { value: "TUI" } });
    let list = await screen.findByRole("listbox", { name: "Kết quả tìm sản phẩm" });
    expect(await within(list).findByText("hết hàng")).toBeTruthy();
    cleanup();
    fake.balanceNextCursor = "c2";
    renderForm();
    fireEvent.change(productBox(), { target: { value: "TUI" } });
    list = await screen.findByRole("listbox", { name: "Kết quả tìm sản phẩm" });
    expect(await within(list).findByText("290.000 ₫")).toBeTruthy();
    expect(within(list).queryByText("hết hàng")).toBeNull();
    fireEvent.keyDown(productBox(), { key: "Enter" });
    await screen.findByRole("textbox", { name: "Số lượng TUI0090-NAU" });
    expect(screen.queryByText(/^Có thể bán:/)).toBeNull();
  });

  it("tổng tính trước: 2 × 690.000 giảm dòng 100.000, ship 30.000, mã giảm 150.000 → 1.160.000", async () => {
    fake.coupon = { ok: true, discount: 150000, remainingUses: 12 };
    await readyOrder();
    fireEvent.click(screen.getByRole("button", { name: "Thêm 1 DAM0123-BE-M" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Giảm dòng DAM0123-BE-M (đồng)" }), {
      target: { value: "100.000" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "Phí ship báo khách" }), { target: { value: "30.000" } });
    const code = screen.getByRole("textbox", { name: "Mã giảm giá" });
    fireEvent.change(code, { target: { value: "thu10" } });
    fireEvent.blur(code);
    expect(await screen.findByText("Áp được: giảm 150.000 ₫ · còn 12 lượt")).toBeTruthy();
    const sum = screen.getByRole("region", { name: "Tiền" });
    expect(within(sum).getByText("1.380.000 ₫")).toBeTruthy(); // tạm tính
    expect(within(sum).getByText("−100.000 ₫")).toBeTruthy();
    expect(within(sum).getByText("−150.000 ₫")).toBeTruthy();
    expect(within(sum).getAllByText("1.160.000 ₫")).toHaveLength(2); // tổng, COD
    // Mã kiểm trên tạm tính sau giảm dòng và giảm tay cấp đơn, không tốn lượt.
    expect(fake.couponCalls.at(-1)?.get("base")).toBe("1280000");
    expect(fake.couponCalls.at(-1)?.get("code")).toBe("THU10");
  });

  it("checkCoupon từ chối: câu theo lý do ngay dưới ô mã, giảm theo mã 0", async () => {
    fake.coupon = { ok: false, discount: 0, reason: "exhausted" };
    await readyOrder();
    const code = screen.getByRole("textbox", { name: "Mã giảm giá" });
    fireEvent.change(code, { target: { value: "FLASH50" } });
    fireEvent.keyDown(code, { key: "Enter" });
    expect(await screen.findByText("Mã đã hết lượt dùng")).toBeTruthy();
    expect(code.getAttribute("aria-invalid")).toBe("true");
  });

  it("SĐT người nhận thuộc hồ sơ khác: cảnh báo có thể là cùng một khách, không tự gộp", async () => {
    await readyOrder();
    const box = await screen.findByText("Có thể là cùng một khách.");
    expect(box.parentElement?.textContent).toContain("KH001877");
    expect(screen.getByRole("link", { name: "Xem hai hồ sơ" }).getAttribute("href")).toBe(`/khach/${C1}/gop?voi=${C3}`);
    // C1 đã có SĐT: số người nhận chỉ là SĐT của đơn, không có ô gắn số vào hồ sơ (A-05).
    expect(screen.queryByRole("checkbox", { name: "Số này là của khách (gắn vào hồ sơ)" })).toBeNull();
  });

  it("A-01: khách mới mà SĐT đã thuộc hồ sơ khác: hai nút, Dùng hồ sơ đó chọn hồ sơ có sẵn, không tự gộp", async () => {
    renderForm();
    fireEvent.change(customerBox(), { target: { value: "Lan Anh" } });
    fireEvent.click(await screen.findByRole("option", { name: /Khách mới/ }));
    fireEvent.change(screen.getByRole("textbox", { name: "SĐT người nhận *" }), { target: { value: "0987 654 321" } });
    const box = (await screen.findByText("Số này đang là của khách Nguyễn Văn Tùng.")).parentElement;
    expect(box?.textContent).toContain("Khách mới sẽ không giữ số này");
    expect(box?.textContent).not.toContain("Không tự gộp.Không tự gộp");
    // Ô "Số này là của khách" không còn (số đã có chủ).
    expect(screen.queryByRole("checkbox", { name: "Số này là của khách (gắn vào hồ sơ)" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Dùng hồ sơ Nguyễn Văn Tùng" }));
    expect(await screen.findByText("KH001877", { selector: ".chip" })).toBeTruthy();
  });

  it("A-01: Vẫn tạo khách mới: ghi nhận lựa chọn, khách vẫn là khách mới và gửi phoneIsCustomers (máy chủ không gắn số)", async () => {
    renderForm();
    fireEvent.change(customerBox(), { target: { value: "Lan Anh" } });
    fireEvent.click(await screen.findByRole("option", { name: /Khách mới/ }));
    await addProduct("DAM0123-BE-M");
    await fillAddress();
    fireEvent.click(await screen.findByRole("button", { name: "Vẫn tạo khách mới" }));
    expect(await screen.findByText(/Đã chọn: vẫn tạo khách mới/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Vẫn tạo khách mới" })).toBeNull();
    await saveAndWait();
    expect(fake.writes[0]?.body.customerId).toBeUndefined();
    expect(fake.writes[0]?.body.customer).toEqual({
      fullName: "Nguyễn Văn Tùng",
      phone: "0987654321",
      phoneIsCustomers: true,
    });
  });

  it("A-05: khách có sẵn chưa có SĐT: ô Số này là của khách bật sẵn, gửi phoneIsCustomers cùng customerId; tắt thì không gửi", async () => {
    const pickLead = async () => {
      renderForm();
      fireEvent.change(customerBox(), { target: { value: "Lead Chưa Số" } });
      fireEvent.click(await screen.findByRole("option", { name: /^Lead Chưa Số/ }));
      await screen.findByText("KH000999", { selector: ".chip" });
      await addProduct("DAM0123-BE-M");
      await fillAddress();
    };
    await pickLead();
    const check = await screen.findByRole<HTMLInputElement>("checkbox", {
      name: "Số này là của khách (gắn vào hồ sơ)",
    });
    expect(check.checked).toBe(true);
    await saveAndWait();
    expect(fake.writes[0]?.body.customerId).toBe(C4);
    expect(fake.writes[0]?.body.phoneIsCustomers).toBe(true);
    expect(fake.writes[0]?.body.customer).toBeUndefined();
    cleanup();
    await pickLead();
    const off = await screen.findByRole<HTMLInputElement>("checkbox", { name: "Số này là của khách (gắn vào hồ sơ)" });
    fireEvent.click(off);
    await saveAndWait();
    expect(fake.writes).toHaveLength(2);
    expect(fake.writes[1]?.body.phoneIsCustomers).toBeUndefined();
  });

  it("A-05: khách có sẵn đã có SĐT: không gửi phoneIsCustomers, số người nhận chỉ ở địa chỉ giao", async () => {
    await readyOrder();
    fireEvent.click(confirmBtn());
    await screen.findByText("Đã tạo đơn DH0001236.");
    expect(fake.writes[0]?.body.customerId).toBe(C1);
    expect(fake.writes[0]?.body.phoneIsCustomers).toBeUndefined();
  });

  it("giảm tay vượt ngưỡng với người không có quyền duyệt: cảnh báo trước khi lưu", async () => {
    await readyOrder();
    fireEvent.change(screen.getByRole("textbox", { name: "Giảm cấp đơn" }), { target: { value: "103.500" } });
    expect(await screen.findByText("Giảm tay 15% tạm tính, vượt mức 10%.")).toBeTruthy();
  });

  it("có quyền duyệt giảm giá: không cảnh báo", async () => {
    fake.grants = [...fake.grants, { permission: "order.discount.approve", scope: { type: "all", id: "" } }];
    await readyOrder();
    fireEvent.change(screen.getByRole("textbox", { name: "Giảm cấp đơn" }), { target: { value: "103.500" } });
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByText(/vượt mức/)).toBeNull();
  });

  it("kiểm trùng trước khi lưu khi đã có khách và dòng (trạng thái H)", async () => {
    fake.duplicates = [
      {
        id: O_OLD,
        code: "DH0001198",
        label: "new",
        status: "new",
        customerId: C1,
        customerName: "Trần Thị Thu Hà",
        grandTotal: 1330000,
        codAmount: 1330000,
        itemCount: 2,
        createdAt: "2026-11-19T02:12:00Z",
        duplicateSuspect: false,
      },
    ];
    await readyOrder();
    expect(await screen.findByText("Có thể trùng đơn.")).toBeTruthy();
    expect(screen.getByRole("link", { name: "DH0001198" }).getAttribute("href")).toBe(`/don/${O_OLD}`);
    const call = fake.duplicateCalls.at(-1);
    expect(call?.get("customerId")).toBe(C1);
    expect(call?.getAll("variantIds")).toEqual([V1]);
  });
});

describe("gửi đơn (useWriteAction, trạng thái D–H)", () => {
  it("503 hai lần rồi 201: ba yêu cầu cùng Idempotency-Key, một đơn; đơn sau dùng khóa mới", async () => {
    fake.replies = [
      { status: 503, body: { error: "retry_later", request_id: "r1" } },
      { status: 503, body: { error: "retry_later", request_id: "r2" } },
      { status: 201, body: order() },
    ];
    const created: Order[] = [];
    renderForm({ onCreated: (o) => created.push(o) });
    await pickCustomerByKeys();
    await addProduct("DAM0123-BE-M");
    await fillAddress();
    fireEvent.click(confirmBtn());
    expect(await screen.findByText("Đã tạo đơn DH0001236.")).toBeTruthy();
    expect(fake.writes).toHaveLength(3);
    const key = fake.writes[0]?.key;
    expect(key).toMatch(/^[A-Za-z0-9_-]{1,128}$/);
    expect(fake.writes.map((w) => w.key)).toEqual([key, key, key]);
    expect(fake.writes[0]?.body).toEqual({
      customerId: C1,
      intent: "confirmed",
      items: [{ variantId: V1, qty: 1 }],
      shipAddress: {
        name: "Nguyễn Văn Tùng",
        phone: "0987654321",
        line: "Số 12 ngõ 68 Trần Thái Tông",
        provinceCode: "01",
        wardCode: "00004",
      },
      shippingFee: 0,
      trafficSourceId: S_MANUAL,
      channel: "manual",
    });
    expect(created).toHaveLength(1);
    // Form trống cho đơn sau; đơn sau là thao tác mới, khóa mới.
    expect(screen.getByText("Chưa có sản phẩm. Gõ mã hoặc tên ở ô trên, hoặc quét barcode.")).toBeTruthy();
    await pickCustomerByKeys();
    await addProduct("AO0456-TRANG-S");
    await fillAddress();
    fireEvent.click(confirmBtn());
    await waitFor(() => {
      expect(fake.writes).toHaveLength(4);
    });
    expect(fake.writes[3]?.key).not.toBe(key);
  });

  it("hết lượt gửi lại (503 bốn lần): báo bận, nút Gửi lại gửi đúng đơn cũ cùng khóa", async () => {
    fake.replies = Array.from({ length: 4 }, () => ({ status: 503, body: { error: "retry_later", request_id: "r" } }));
    await readyOrder();
    fireEvent.click(confirmBtn());
    expect(await screen.findByText("Hệ thống đang bận, thử lại sau ít giây.")).toBeTruthy();
    expect(fake.writes).toHaveLength(4);
    fireEvent.click(screen.getByRole("button", { name: "Gửi lại" }));
    expect(await screen.findByText("Đã tạo đơn DH0001236.")).toBeTruthy();
    expect(new Set(fake.writes.map((w) => w.key)).size).toBe(1);
  });

  it("409 out_of_stock: dòng của variantId được tô, câu 'Không đủ hàng: còn {available}', đơn chưa tạo", async () => {
    await readyOrder();
    await addProduct("AO0456-TRANG-S");
    fake.replies = [{ status: 409, body: { error: "out_of_stock", request_id: "r", variantId: V2, available: 0 } }];
    fireEvent.click(confirmBtn());
    expect(await screen.findByText("Không đủ hàng: còn 0")).toBeTruthy();
    expect(screen.getByText("Chưa lưu đơn: không đủ hàng.")).toBeTruthy();
    const row = screen.getByRole("textbox", { name: "Số lượng AO0456-TRANG-S" }).closest("tr");
    expect(row?.classList.contains("is-err")).toBe(true);
    const other = screen.getByRole("textbox", { name: "Số lượng DAM0123-BE-M" }).closest("tr");
    expect(other?.classList.contains("is-err")).toBe(false);
    expect(screen.queryByText(/Đã tạo đơn/)).toBeNull();
  });

  it("403 forbidden (kho giữ hàng ngoài phạm vi, Task 411): câu riêng, đơn chưa tạo, không nút Gửi lại", async () => {
    await readyOrder();
    fake.replies = [{ status: 403, body: { error: "forbidden", request_id: "r" } }];
    fireEvent.click(confirmBtn());
    expect(
      await screen.findByText(
        "Chưa lưu đơn: bạn chưa được giao kho giữ hàng của đơn. Nhờ chủ shop gán quyền lên đơn ở kho này hoặc toàn shop.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/Đã tạo đơn/)).toBeNull();
    expect(screen.queryByRole("button", { name: "Gửi lại" })).toBeNull();
    expect(fake.writes).toHaveLength(1);
  });

  it("422 coupon_rejected expired: 'Mã đã hết hạn' dưới ô mã", async () => {
    fake.coupon = { ok: true, discount: 50000 };
    await readyOrder();
    const code = screen.getByRole("textbox", { name: "Mã giảm giá" });
    fireEvent.change(code, { target: { value: "THU10" } });
    fireEvent.blur(code);
    await screen.findByText(/Áp được/);
    fake.replies = [{ status: 422, body: { error: "coupon_rejected", request_id: "r", reason: "expired" } }];
    fireEvent.click(confirmBtn());
    expect(await screen.findByText("Mã đã hết hạn")).toBeTruthy();
    expect(fake.writes[0]?.body.couponCode).toBe("THU10");
  });

  it("Ctrl+Enter ngay trong ô mã (chưa rời ô): gửi đúng mã đang gõ, một lần", async () => {
    await readyOrder();
    const code = screen.getByRole("textbox", { name: "Mã giảm giá" });
    fireEvent.change(code, { target: { value: "sale5" } });
    fireEvent.keyDown(code, { key: "Enter", ctrlKey: true });
    expect(await screen.findByText("Đã tạo đơn DH0001236.")).toBeTruthy();
    expect(fake.writes).toHaveLength(1);
    expect(fake.writes[0]?.body.couponCode).toBe("SALE5");
    expect(fake.writes[0]?.body.intent).toBe("confirmed");
  });

  it("đơn chờ duyệt (pendingReason discount_approval): 'Đơn đang chờ duyệt giảm giá'", async () => {
    fake.replies = [
      {
        status: 201,
        body: order({ code: "DH0001237", status: "new", pendingReason: "discount_approval" }),
      },
    ];
    await readyOrder();
    fireEvent.click(confirmBtn());
    expect(await screen.findByText("Đã tạo đơn DH0001237.")).toBeTruthy();
    expect(screen.getByText("Đơn đang chờ duyệt giảm giá.", { exact: false })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Mở đơn" }).getAttribute("href")).toBe(`/don/${order().id}`);
  });

  it("cảnh báo sau khi tạo: SĐT thuộc hồ sơ khác, nghi trùng", async () => {
    fake.replies = [
      {
        status: 201,
        body: order({
          warnings: [
            { code: "phone_belongs_to_other_customer", refId: C3 },
            { code: "possible_duplicate", refId: O_OLD },
          ],
        }),
      },
    ];
    await readyOrder();
    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" }));
    const ok = await screen.findByText("Đã tạo đơn DH0001236.");
    const box = ok.closest(".box");
    expect(box?.textContent).toContain("SĐT người nhận thuộc hồ sơ khách khác");
    expect(box?.textContent).toContain("Có thể trùng với đơn khác");
    expect(
      within(box as HTMLElement)
        .getByRole("link", { name: "Xem đơn trùng" })
        .getAttribute("href"),
    ).toBe("/don/trung");
    expect(fake.writes[0]?.body.intent).toBe("draft");
  });

  it("khách được truyền sẵn (initialCustomer, dùng lại ở M4): không phải tìm", async () => {
    renderForm({ initialCustomer: { id: C1, code: "KH000231", fullName: "Trần Thị Thu Hà" } });
    expect(await screen.findByText("KH000231", { selector: ".chip" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Đổi khách" })).toBeTruthy();
  });
});

describe("lên đơn cạnh khung chat (M4, Task 176)", () => {
  const CONV = "0195f3b2-0000-7000-8000-000000010001";
  /** Đơn nháp một dòng, sửa được mọi phần (đơn nháp từ Live). */
  const draftOf = (p: Partial<Order> = {}): Order =>
    order({
      id: O_OLD,
      status: "draft",
      version: 3,
      editScope: {
        lines: true,
        discounts: true,
        coupon: true,
        address: true,
        shipping: true,
        carrierNote: true,
        careBy: true,
        trafficSource: true,
      },
      items: [
        {
          id: "i1",
          position: 1,
          variantId: V1,
          sku: "DAM0123-BE-M",
          name: "Đầm",
          qty: 1,
          unitPrice: 690000,
          netAmount: 690000,
        },
      ],
      customer: { id: C1, rootId: C1, code: "KH000231", fullName: "Trần Thị Thu Hà" },
      ...p,
    });

  it("conversationId đi kèm thân createOrder; nguồn đơn tự điền từ bối cảnh hội thoại", async () => {
    renderForm({ conversationId: CONV, initialTrafficSourceId: S_ZALO });
    await pickCustomerByKeys();
    await addProduct("DAM0123-BE-M");
    await fillAddress();
    await waitFor(() => {
      expect(confirmBtn().disabled).toBe(false);
    });
    fireEvent.click(confirmBtn());
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toMatchObject({ conversationId: CONV, trafficSourceId: S_ZALO });
  });

  it("HV26 (Task 501): addressFromMessage → nút 'Lấy địa chỉ từ tin…' chép nguyên văn vào ô Số nhà, đường (sửa tiếp được)", () => {
    renderForm({
      addressFromMessage: { label: "Lấy địa chỉ từ tin 09:14 của khách", text: "12 ngõ 45 Trần Duy Hưng" },
    });
    const line = screen.getByRole("textbox", { name: "Số nhà, đường *" });
    expect((line as HTMLInputElement).value).toBe("");
    fireEvent.click(screen.getByRole("button", { name: "Lấy địa chỉ từ tin 09:14 của khách" }));
    expect((line as HTMLInputElement).value).toBe("12 ngõ 45 Trần Duy Hưng");
    fireEvent.change(line, { target: { value: "12 ngõ 45 Trần Duy Hưng, Trung Hòa" } });
    expect((line as HTMLInputElement).value).toBe("12 ngõ 45 Trần Duy Hưng, Trung Hòa");
  });

  it("không có addressFromMessage (màn Lên đơn của M2) → không có nút lấy địa chỉ", () => {
    renderForm();
    expect(screen.queryByRole("button", { name: /Lấy địa chỉ từ tin/ })).toBeNull();
  });

  it("không truyền conversationId (màn Lên đơn của M2) → thân không có conversationId", async () => {
    await readyOrder();
    fireEvent.click(confirmBtn());
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).not.toHaveProperty("conversationId");
  });

  it('hoàn tất đơn nháp đủ địa chỉ, không đổi gì: nút "Lưu và xác nhận" gọi onSaved với đơn đang có, không gửi updateOrder', async () => {
    const saved: Order[] = [];
    const draft = draftOf({
      shipAddress: {
        name: "Nguyễn Văn Tùng",
        phone: "0987654321",
        line: "Số 12 ngõ 68 Trần Thái Tông",
        provinceCode: "01",
        wardCode: "00004",
      },
      trafficSourceId: S_MANUAL,
    });
    renderForm({
      edit: { order: draft, confirmAfterSave: true, onSaved: (o) => saved.push(o), onReload: () => undefined },
    });
    const btn = await screen.findByRole<HTMLButtonElement>("button", { name: "Lưu và xác nhận" });
    await waitFor(() => {
      expect(btn.disabled).toBe(false);
    });
    fireEvent.click(btn);
    expect(saved).toEqual([draft]);
    expect(fake.writes).toHaveLength(0);
  });

  it("hoàn tất đơn nháp thiếu địa chỉ → nút khóa kèm lý do như khi xác nhận", async () => {
    const draft = draftOf();
    renderForm({
      edit: { order: draft, confirmAfterSave: true, onSaved: () => undefined, onReload: () => undefined },
    });
    const btn = await screen.findByRole<HTMLButtonElement>("button", { name: "Lưu và xác nhận" });
    expect(btn.disabled).toBe(true);
  });
});
