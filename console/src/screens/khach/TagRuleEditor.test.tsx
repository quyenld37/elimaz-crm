// @vitest-environment jsdom
// Quản lý nhãn khách và quy tắc tự gắn (mockup docs/mockups/m2/khach-danh-sach.html trạng thái E; HV35).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { T1, T2, grantsOf, installFake, listItem, renderAt, stubDialog, type Fake } from "./khachTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
  fake.grants = grantsOf("customer.tags.manage");
  fake.list = [listItem()];
  fake.rules.set(T2, {
    tagId: T2,
    enabled: true,
    version: 3,
    conditions: [
      { field: "return_rate_bp", op: "gte", value: 3000 },
      { field: "returned_count", op: "gte", value: 2 },
    ],
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

async function openTags() {
  renderAt("/khach");
  fireEvent.click(await screen.findByRole("button", { name: "Nhãn" }));
  return screen.getByRole("dialog", { name: "Nhãn khách" });
}

const item = (dlg: HTMLElement, name: string) => {
  const el = within(dlg)
    .getAllByText(name)
    .map((e) => e.closest(".oitem"))
    .find((e) => e instanceof HTMLElement);
  if (!(el instanceof HTMLElement)) throw new Error(`không thấy nhãn ${name}`);
  return el;
};

/** Gõ giá trị của điều kiện thứ n. */
function type(dlg: HTMLElement, n: number, value: string) {
  fireEvent.change(within(dlg).getByRole("textbox", { name: `Giá trị ${String(n)}` }), { target: { value } });
}
const saveRule = (dlg: HTMLElement) => {
  fireEvent.click(within(dlg).getByRole("button", { name: "Lưu quy tắc" }));
};

describe("Nhãn khách", () => {
  it("mỗi nhãn ghi gắn tay hay theo quy tắc; nút Thêm quy tắc / Sửa quy tắc", async () => {
    const dlg = await openTags();
    await within(dlg).findByText("theo quy tắc");
    expect(within(item(dlg, "Hay hoàn")).getByRole("button", { name: "Sửa quy tắc" })).toBeTruthy();
    expect(within(item(dlg, "Khách quen")).getByText("gắn tay")).toBeTruthy();
    expect(within(item(dlg, "Khách quen")).getByRole("button", { name: "Thêm quy tắc" })).toBeTruthy();
  });

  it("sửa quy tắc: tỷ lệ hoàn hiện theo %, lưu đổi lại phần vạn kèm version", async () => {
    const dlg = await openTags();
    await within(dlg).findByText("theo quy tắc");
    fireEvent.click(within(item(dlg, "Hay hoàn")).getByRole("button", { name: "Sửa quy tắc" }));
    const v1 = within(dlg).getByRole("textbox", { name: "Giá trị 1" });
    expect((v1 as HTMLInputElement).value).toBe("30");
    fireEvent.change(v1, { target: { value: "25" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Bỏ điều kiện 2" }));
    fireEvent.click(within(dlg).getByRole("button", { name: "+ Thêm điều kiện (VÀ)" }));
    fireEvent.change(within(dlg).getByRole("combobox", { name: "Chỉ số 2" }), { target: { value: "total_spent" } });
    fireEvent.change(within(dlg).getByRole("combobox", { name: "Phép so 2" }), { target: { value: "lte" } });
    type(dlg, 2, "5.000.000");
    fireEvent.click(within(dlg).getByRole("checkbox", { name: "Đang bật" }));
    saveRule(dlg);
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        method: "PUT",
        path: `/api/v1/customer-tags/${T2}/rule`,
        body: {
          version: 3,
          enabled: false,
          conditions: [
            { field: "return_rate_bp", op: "gte", value: 2500 },
            { field: "total_spent", op: "lte", value: 5_000_000 },
          ],
        },
      });
    });
  });

  it("thêm quy tắc cho nhãn chưa có: không gửi version; giá trị sai báo ngay, không gửi", async () => {
    const dlg = await openTags();
    await within(dlg).findByText("theo quy tắc");
    fireEvent.click(within(item(dlg, "Khách quen")).getByRole("button", { name: "Thêm quy tắc" }));
    type(dlg, 1, "2,5");
    saveRule(dlg);
    expect(await within(dlg).findByText("Điều kiện 1: nhập số nguyên từ 0")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
    type(dlg, 1, "5");
    saveRule(dlg);
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({ method: "PUT", path: `/api/v1/customer-tags/${T1}/rule` });
    });
    expect(fake.writes.at(-1)?.body).toEqual({
      enabled: true,
      conditions: [{ field: "order_count", op: "gte", value: 5 }],
    });
  });

  it("tạo nhãn: tên và màu; đổi tên, màu gửi version", async () => {
    const dlg = await openTags();
    await within(dlg).findByText("theo quy tắc");
    fireEvent.click(within(dlg).getByRole("button", { name: "+ Tạo nhãn" }));
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Tên nhãn" }), { target: { value: "VIP" } });
    fireEvent.change(within(dlg).getByRole("combobox", { name: "Màu" }), { target: { value: "purple" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu nhãn" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        method: "POST",
        path: "/api/v1/customer-tags",
        body: { name: "VIP", color: "purple" },
      });
    });
    fireEvent.click(within(item(dlg, "Khách quen")).getByRole("button", { name: "Đổi tên, màu" }));
    fireEvent.change(within(dlg).getByRole("textbox", { name: "Tên nhãn" }), { target: { value: "Khách thân" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu nhãn" }));
    await waitFor(() => {
      expect(fake.writes.at(-1)).toMatchObject({
        method: "PATCH",
        path: `/api/v1/customer-tags/${T1}`,
        body: { version: 1, name: "Khách thân", color: "gray" },
      });
    });
  });

  it("quy tắc sai (máy chủ trả invalid_tag_rule): câu báo, giữ các điều kiện đang sửa", async () => {
    fake.replies.push({ status: 422, body: { error: "invalid_tag_rule", request_id: "r" } });
    const dlg = await openTags();
    await within(dlg).findByText("theo quy tắc");
    fireEvent.click(within(item(dlg, "Hay hoàn")).getByRole("button", { name: "Sửa quy tắc" }));
    saveRule(dlg);
    expect(await within(dlg).findByText(/Quy tắc nhãn chưa đúng/)).toBeTruthy();
    expect(within(dlg).getByRole<HTMLInputElement>("textbox", { name: "Giá trị 1" }).value).toBe("30");
  });

  it("đọc danh sách nhãn hay quy tắc lỗi: câu lỗi riêng, không treo", async () => {
    fake.failReads = [/rule$/];
    let dlg = await openTags();
    expect(await within(dlg).findAllByText("chưa đọc được quy tắc")).toHaveLength(3);
    expect(within(dlg).queryByRole("button", { name: "Sửa quy tắc" })).toBeNull();
    cleanup();
    fake.failReads = [/customer-tags$/];
    dlg = await openTags();
    expect(await within(dlg).findByText(/Chưa đọc được danh sách nhãn/)).toBeTruthy();
  });
});
