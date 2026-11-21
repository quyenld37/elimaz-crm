// @vitest-environment jsdom
// Cài đặt vận chuyển /settings/fulfillment (Task 132; mockup docs/mockups/m3/ket-noi-van-chuyen.html trạng thái F; HV06):
// tỷ lệ nhập theo %, "30%" gửi 3000; "0" báo lỗi tại ô, nút lưu khóa; chu kỳ tra không hiện mà gửi nguyên giá trị đang
// lưu; 422 setting_out_of_range báo đúng ô; 409 tải lại; Về mặc định chỉ điền ô.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { installFake, renderAt, type Fake } from "./carriersTestkit";

let fake: Fake;
beforeEach(() => {
  fake = installFake();
});
afterEach(() => {
  cleanup();
});

const field = (label: string) => screen.getByLabelText(label);
const rate = () => field("và tỷ lệ hoàn từ (%)");
const saveBtn = () => screen.getByRole("button", { name: "Lưu cài đặt" });

describe("cài đặt vận chuyển (trạng thái F)", () => {
  it("hiện giá trị đang lưu, đơn vị; version 0 → 'Đang dùng mặc định'; chu kỳ tra không hiện", async () => {
    renderAt("/settings/fulfillment");
    await screen.findByLabelText("Chờ thu gom (giờ)");
    expect(screen.getByText("Đang dùng mặc định (chưa lưu lần nào)")).toBeTruthy();
    expect((field("Chờ thu gom (giờ)") as HTMLInputElement).value).toBe("48");
    expect((field("Đang chuyển hoàn (giờ)") as HTMLInputElement).value).toBe("168");
    expect((field("Quá hạn chưa có tiền sau khi giao (ngày)") as HTMLInputElement).value).toBe("14");
    expect((rate() as HTMLInputElement).value).toBe("30");
    expect(screen.queryByText(/chu kỳ tra/i, { selector: "label *" })).toBeNull();
    expect(saveBtn().hasAttribute("disabled")).toBe(true);
  });

  it('nhập "30%" gửi highRateBp 3000; pollMinutes, version gửi nguyên', async () => {
    renderAt("/settings/fulfillment");
    await screen.findByLabelText("Chờ thu gom (giờ)");
    fireEvent.change(rate(), { target: { value: "35%" } });
    fireEvent.change(rate(), { target: { value: "30%" } });
    fireEvent.change(field("Đang chuyển (giờ)"), { target: { value: "96" } });
    fireEvent.click(saveBtn());
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    const w = fake.writes[0];
    expect(w?.method).toBe("PUT");
    expect(w?.path).toBe("/api/v1/fulfillment-settings");
    expect(w?.key).toEqual(expect.any(String));
    expect(w?.body).toEqual({
      version: 0,
      stuckHours: { awaiting_pickup: 48, shipping: 96, delivery_failed: 24, returning: 168, cancel_requested: 24 },
      pollMinutes: fake.settings.pollMinutes,
      callbackDeadlineHours: 24,
      codOverdueDays: 14,
      agedDays: { held: 20, inTransit: 180 },
      returnRisk: { watchMinReturned: 1, highMinReturned: 2, highRateBp: 3000 },
      defaultWeightGramsPerItem: 300,
      quoteValidMinutes: 30,
    });
  });

  it('nhập "0" ở tỷ lệ → lỗi tại ô, nút lưu khóa, không gửi', async () => {
    renderAt("/settings/fulfillment");
    await screen.findByLabelText("Chờ thu gom (giờ)");
    fireEvent.change(rate(), { target: { value: "0" } });
    expect(rate().getAttribute("aria-invalid")).toBe("true");
    const wrap = rate().closest(".field");
    if (!(wrap instanceof HTMLElement)) throw new Error("thiếu ô");
    expect(within(wrap).getByText("Nhập từ 1% đến 100%")).toBeTruthy();
    expect(saveBtn().hasAttribute("disabled")).toBe(true);
    fireEvent.change(field("Chờ thu gom (giờ)"), { target: { value: "0" } });
    expect(screen.getByText("Nhập số nguyên lớn hơn 0")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });

  it("máy chủ 422 setting_out_of_range nêu returnRisk.highRateBP → báo dưới ô tỷ lệ", async () => {
    fake.replies.push({
      status: 422,
      body: { error: "setting_out_of_range", request_id: "t", field: "returnRisk.highRateBP" },
    });
    renderAt("/settings/fulfillment");
    await screen.findByLabelText("Chờ thu gom (giờ)");
    fireEvent.change(rate(), { target: { value: "40" } });
    fireEvent.click(saveBtn());
    await waitFor(() => {
      expect(rate().getAttribute("aria-invalid")).toBe("true");
    });
  });

  it("409 version_conflict → câu tải lại", async () => {
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    renderAt("/settings/fulfillment");
    await screen.findByLabelText("Chờ thu gom (giờ)");
    fireEvent.change(rate(), { target: { value: "40" } });
    fireEvent.click(saveBtn());
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Cài đặt vừa được người khác sửa");
    expect(within(alert).getByRole("button", { name: "Tải lại" })).toBeTruthy();
  });

  it("Về mặc định chỉ điền ô (chưa gửi)", async () => {
    fake.settings = { ...fake.settings, codOverdueDays: 30, version: 2 };
    renderAt("/settings/fulfillment");
    await screen.findByLabelText("Chờ thu gom (giờ)");
    expect(screen.queryByText("Đang dùng mặc định (chưa lưu lần nào)")).toBeNull();
    expect((field("Quá hạn chưa có tiền sau khi giao (ngày)") as HTMLInputElement).value).toBe("30");
    fireEvent.click(screen.getByRole("button", { name: "Về mặc định" }));
    expect((field("Quá hạn chưa có tiền sau khi giao (ngày)") as HTMLInputElement).value).toBe("14");
    expect(fake.writes).toHaveLength(0);
    expect(saveBtn().hasAttribute("disabled")).toBe(false);
  });

  it("lưu xong: câu 'Đã lưu cài đặt.' còn sau khi form nạp bản mới (version mới)", async () => {
    fake.replies.push({ status: 200, body: { ...fake.settings, codOverdueDays: 21, version: 1 } });
    renderAt("/settings/fulfillment");
    await screen.findByLabelText("Chờ thu gom (giờ)");
    fireEvent.change(field("Quá hạn chưa có tiền sau khi giao (ngày)"), { target: { value: "21" } });
    fireEvent.click(saveBtn());
    expect(await screen.findByText("Đã lưu cài đặt.")).toBeTruthy();
    expect((field("Quá hạn chưa có tiền sau khi giao (ngày)") as HTMLInputElement).value).toBe("21");
    expect(screen.queryByText("Đang dùng mặc định (chưa lưu lần nào)")).toBeNull();
  });
});
