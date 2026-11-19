// @vitest-environment jsdom
// Chi tiết mã (mockup docs/mockups/m2/ma-giam-gia.html trạng thái D, E). Kế hoạch M2 Task M2-30 Step 1: mã đã dùng →
// ô giá trị khóa (theo coupon_locked_after_use: chỉ đổi tên, hạn cuối, tổng lượt, trạng thái).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ID1,
  ORDER,
  coupon,
  grantsOf,
  installFake,
  redemption,
  renderAt,
  stubDialog,
  type Fake,
} from "./couponTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
  fake.coupons = [coupon()];
  fake.redemptions = [
    redemption(),
    redemption({ id: "r2", orderCode: "DH0001177", discount: 80_000, status: "released" }),
  ];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const input = (name: string) => screen.getByLabelText<HTMLInputElement>(name);
const fill = (name: string, v: string) => fireEvent.change(input(name), { target: { value: v } });

/** Mở chi tiết THU10, chờ tải xong. */
async function openThu10() {
  renderAt(`/ma-giam-gia/${ID1}`);
  await screen.findByRole("heading", { name: "THU10" });
}

/** Bấm Lưu và chờ một yêu cầu ghi; trả thân đã gửi. */
async function saveOnce() {
  fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
  await waitFor(() => {
    expect(fake.writes).toHaveLength(1);
  });
  expect(fake.writes[0]?.method).toBe("PATCH");
  return fake.writes[0]?.body;
}

describe("Mã đã có lượt dùng", () => {
  it("ô điều khoản khóa, ghi rõ vì sao; tên, hạn cuối, tổng lượt vẫn sửa được", async () => {
    renderAt(`/ma-giam-gia/${ID1}`);
    expect(await screen.findByRole("heading", { name: "THU10" })).toBeTruthy();
    expect(screen.getByText("87 / 200 lượt · còn 113")).toBeTruthy();
    expect(screen.getByText(/chỉ đổi được tên, ngày hết hạn, tổng lượt \(không dưới 87\) và tắt mã/)).toBeTruthy();
    for (const name of ["Giảm (%) · khóa", "Giảm tối đa (đồng) · khóa", "Đơn tối thiểu (đồng) · khóa", "Từ · khóa"]) {
      expect(input(name).disabled).toBe(true);
    }
    expect(input("Giảm (%) · khóa").value).toBe("10");
    expect(input("Giảm tối đa (đồng) · khóa").value).toBe("150.000");
    expect(input("Tên").disabled).toBe(false);
    expect(input("Đến").disabled).toBe(false);
    expect(input("Tổng lượt").disabled).toBe(false);
    expect(screen.getByRole("checkbox", { name: "Facebook" }).hasAttribute("disabled")).toBe(true);
  });

  it("lưu chỉ gửi tên, hạn cuối, tổng lượt kèm version", async () => {
    await openThu10();
    fill("Tên", "Mùa thu");
    fill("Tổng lượt", "250");
    expect(await saveOnce()).toEqual({
      version: 3,
      name: "Mùa thu",
      validTo: "2099-11-30T16:59:00.000Z",
      maxUses: 250,
    });
  });

  it("lưu xong: đọc lại mã và báo Đã lưu", async () => {
    await openThu10();
    fill("Tên", "Mùa thu");
    const after = coupon({ name: "Mùa thu", version: 4 });
    fake.replies.push({ status: 200, body: after });
    fake.coupons = [after];
    await saveOnce();
    expect(await screen.findByText("Đã lưu.")).toBeTruthy();
    expect(input("Tên").value).toBe("Mùa thu");
  });

  it("tổng lượt dưới số đã dùng: báo dưới ô, không gửi", async () => {
    await openThu10();
    fill("Tổng lượt", "50");
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    expect(screen.getByText("Tổng lượt không được thấp hơn số đã dùng (87).")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });

  it("máy chủ vẫn trả coupon_locked_after_use: hiện câu của mã lỗi", async () => {
    fake.replies.push({ status: 409, body: { error: "coupon_locked_after_use", request_id: "t" } });
    await openThu10();
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    expect(
      await screen.findByText(/Mã đã có người dùng: chỉ đổi được tên, hạn cuối, số lượt, trạng thái/),
    ).toBeTruthy();
  });

  it("lượt dùng theo đơn: mã đơn dẫn tới đơn, số giảm, trạng thái", async () => {
    renderAt(`/ma-giam-gia/${ID1}`);
    const row = await screen.findByRole("row", { name: /DH0001236/ });
    expect(within(row).getByRole("link", { name: "DH0001236" }).getAttribute("href")).toBe(`/don/${ORDER}`);
    expect(within(row).getByText("150.000 ₫")).toBeTruthy();
    expect(within(row).getByText("Đang tính")).toBeTruthy();
    const rel = screen.getByRole("row", { name: /DH0001177/ });
    expect(within(rel).getByText("Đã trả lượt (đơn hủy)")).toBeTruthy();
  });

  it("tắt mã: hỏi lại rồi gửi status disabled kèm version", async () => {
    await openThu10();
    fireEvent.click(screen.getByRole("button", { name: "Tắt mã" }));
    const dlg = screen.getByRole("dialog", { name: "Tắt mã THU10?" });
    expect(within(dlg).getByText(/87 đơn đã dùng giữ nguyên phần giảm/)).toBeTruthy();
    fireEvent.click(within(dlg).getByRole("button", { name: "Tắt mã" }));
    await waitFor(() => {
      expect(fake.writes[0]?.body).toEqual({ version: 3, status: "disabled" });
    });
  });

  it("mã đã tắt: nút Bật lại gửi status active", async () => {
    fake.coupons = [coupon({ status: "disabled" })];
    renderAt(`/ma-giam-gia/${ID1}`);
    fireEvent.click(await screen.findByRole("button", { name: "Bật lại" }));
    await waitFor(() => {
      expect(fake.writes[0]?.body).toEqual({ version: 3, status: "active" });
    });
  });

  it("người khác vừa sửa (409 version_conflict): báo và có nút tải lại", async () => {
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    await openThu10();
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText("Người khác vừa sửa mã này, tải lại.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Tải lại" })).toBeTruthy();
  });
});

describe("Mã chưa dùng", () => {
  it("mọi ô điều khoản sửa được; 12,5% → value 1250", async () => {
    fake.coupons = [coupon({ usedCount: 0, remainingUses: 200 })];
    fake.redemptions = [];
    await openThu10();
    expect(screen.queryByText(/· khóa/)).toBeNull();
    fill("Giảm (%)", "12,5");
    expect(await saveOnce()).toMatchObject({
      version: 3,
      discountKind: "percent",
      value: 1250,
      maxDiscount: 150_000,
    });
  });

  it("chưa dùng nhưng có lượt đã trả (đơn hủy): máy chủ vẫn coi là đã dùng → khóa", async () => {
    fake.coupons = [coupon({ usedCount: 0, remainingUses: 200 })];
    fake.redemptions = [redemption({ status: "released" })];
    renderAt(`/ma-giam-gia/${ID1}`);
    expect(await screen.findByLabelText("Giảm (%) · khóa")).toBeTruthy();
  });
});

it("chỉ có promotion.read: xem được, không có nút lưu hay tắt", async () => {
  fake.grants = grantsOf();
  await openThu10();
  expect(screen.queryByRole("button", { name: "Lưu" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Tắt mã" })).toBeNull();
  expect(input("Tên").disabled).toBe(true);
});
