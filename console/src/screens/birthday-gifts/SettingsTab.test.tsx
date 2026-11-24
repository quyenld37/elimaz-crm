// @vitest-environment jsdom
// Tab Cài đặt quà: bật/tắt, mức giảm (lỗi không chia hết 1.000), điều kiện, xem trước câu khách thấy; lưu có version;
// 409 người khác vừa đổi (mockup docs/mockups/m5/birthday-gifts.html trạng thái C; Task 468).
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PATH, grantsOf, installFake, renderAt, settings, type Fake } from "./birthdayTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const open = async () => {
  renderAt(`${PATH}?tab=cai-dat`);
  return screen.findByRole("textbox", { name: "Mức giảm" });
};
const type = (name: string, v: string) => {
  fireEvent.change(screen.getByRole("textbox", { name }), { target: { value: v } });
};
const save = () => screen.getByRole<HTMLButtonElement>("button", { name: "Lưu cài đặt" });

describe("Cài đặt quà", () => {
  it("hiện cài đặt và xem trước; chưa đổi thì Lưu khóa", async () => {
    const amount = await open();
    expect((amount as HTMLInputElement).value).toBe("50.000");
    expect(screen.getByRole<HTMLInputElement>("checkbox", { name: "Bật quà sinh nhật" }).checked).toBe(true);
    expect(screen.getByText("Trước sinh nhật 7 ngày, bạn nhận mã giảm 50.000 ₫, dùng trong 14 ngày.")).toBeTruthy();
    expect(save().disabled).toBe(true);
  });

  it("mức giảm 10.500: lỗi dưới ô, đếm lỗi, Lưu khóa", async () => {
    await open();
    type("Mức giảm", "10.500");
    expect(screen.getByText("Mức giảm từ 10.000 tới 1.000.000 ₫, bội số của 1.000.")).toBeTruthy();
    expect(screen.getByText("Sửa 1 lỗi để lưu.")).toBeTruthy();
    expect(save().disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Hủy thay đổi" }));
    expect(screen.getByRole<HTMLInputElement>("textbox", { name: "Mức giảm" }).value).toBe("50.000");
  });

  it("lưu: gửi đủ trường và version, cập nhật xem trước", async () => {
    await open();
    type("Gửi trước sinh nhật", "5");
    type("Mức giảm", "100.000");
    expect(screen.getByText("Trước sinh nhật 5 ngày, bạn nhận mã giảm 100.000 ₫, dùng trong 14 ngày.")).toBeTruthy();
    fireEvent.click(save());
    expect(await screen.findByText("Đã lưu cài đặt quà.")).toBeTruthy();
    expect(fake.writes[0]).toMatchObject({
      method: "PUT",
      path: "/api/v1/birthday-settings",
      body: { enabled: true, amount: 100_000, minOrder: 0, daysBefore: 5, validDays: 14, minLeadDays: 30, version: 3 },
    });
    expect(fake.writes[0]?.key).toBeTruthy();
    await waitFor(() => {
      expect(save().disabled).toBe(true);
    });
  });

  it("tắt quà rồi lưu", async () => {
    await open();
    fireEvent.click(screen.getByRole("checkbox", { name: "Bật quà sinh nhật" }));
    fireEvent.click(save());
    await screen.findByText("Đã lưu cài đặt quà.");
    expect(fake.writes[0]?.body).toMatchObject({ enabled: false });
  });

  it("409 version_conflict: báo, tải lại bản mới, bỏ bản nháp", async () => {
    await open();
    fake.settings = settings({ amount: 70_000, version: 4 });
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    type("Mức giảm", "60.000");
    fireEvent.click(save());
    expect(await screen.findByText("Cài đặt vừa được người khác đổi.")).toBeTruthy();
    await waitFor(() => {
      expect(screen.getByRole<HTMLInputElement>("textbox", { name: "Mức giảm" }).value).toBe("70.000");
    });
  });

  it("422 của máy chủ: hiện câu lỗi", async () => {
    await open();
    fake.replies.push({ status: 422, body: { error: "invalid_gift_settings", request_id: "t" } });
    type("Dùng trong", "20");
    fireEvent.click(save());
    expect(await screen.findByText(/Cài đặt quà chưa đúng/)).toBeTruthy();
  });

  it("chỉ promotion.read: ô khóa, không nút lưu", async () => {
    fake.grants = grantsOf();
    const amount = await open();
    expect((amount as HTMLInputElement).disabled).toBe(true);
    expect(screen.queryByRole("button", { name: "Lưu cài đặt" })).toBeNull();
  });
});
