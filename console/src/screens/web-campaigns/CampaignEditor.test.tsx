// @vitest-environment jsdom
// Form chiến dịch giảm giá (mockup docs/mockups/m5/web-campaigns.html trạng thái B, D). Kế hoạch M5 Task 458 Step 1:
// 409 giữ form; không promotion.manage → không nút ghi; chọn đích tối đa 30; giờ Việt Nam; xem trước giá.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CampaignTarget } from "../../api";
import {
  C2,
  C3,
  CAT1,
  CAT2,
  CAT3,
  campaign,
  grantsOf,
  installFake,
  paused,
  renderAt,
  stubDialog,
  type Fake,
} from "./campaignTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const type = (el: HTMLElement, v: string) => {
  fireEvent.change(el, { target: { value: v } });
};
const field = (name: string) => screen.getByLabelText(name);
/** Ô bị khóa: chính ô hay fieldset bao nó (jsdom không đổi thuộc tính disabled của ô trong fieldset khóa). */
const locked = (el: HTMLElement) =>
  el.closest("fieldset")?.disabled === true || (el instanceof HTMLInputElement && el.disabled);

async function openNew() {
  renderAt("/khuyen-mai/chien-dich/moi");
  return screen.findByRole("heading", { name: "Tạo chiến dịch", level: 1 });
}

function fillNew() {
  type(field("Tên chiến dịch *"), "Bộ sưu tập Tết 2027");
  type(field("Phần trăm giảm *"), "10");
  type(field("Bắt đầu (giờ Việt Nam) *"), "2027-01-05T00:00");
  type(field("Kết thúc (giờ Việt Nam) *"), "2027-01-25T23:59");
}

describe("Tạo chiến dịch", () => {
  it("chọn 2 danh mục, dùng kèm mã → gửi đúng thân (giờ VN → UTC), xong mở form của chiến dịch mới", async () => {
    await openNew();
    fillNew();
    fireEvent.click(await screen.findByRole("checkbox", { name: /Đầm công sở/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Áo dài cách tân/ }));
    expect(screen.getByText("Đã chọn 2/30")).toBeTruthy();
    fireEvent.click(screen.getByRole("checkbox", { name: "Cho dùng mã giảm giá cùng chiến dịch" }));
    fireEvent.click(screen.getByRole("button", { name: "Lưu bản nháp" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({ method: "POST", path: "/api/v1/web-campaigns" });
    expect(fake.writes[0]?.key).toBeTruthy();
    expect(fake.writes[0]?.body).toEqual({
      name: "Bộ sưu tập Tết 2027",
      percent: 10,
      scope: "categories",
      targetIds: [CAT1, CAT2],
      allowCoupon: true,
      clearance: false,
      startsAt: "2027-01-04T17:00:00.000Z",
      endsAt: "2027-01-25T16:59:00.000Z",
    });
    expect(await screen.findByRole("heading", { name: "Sửa chiến dịch", level: 1 })).toBeTruthy();
    expect(fake.rows[0]?.id).toBe(C3);
  });

  it("ô sai báo dưới ô, không gửi", async () => {
    await openNew();
    type(field("Phần trăm giảm *"), "120");
    fireEvent.click(screen.getByRole("button", { name: "Lưu bản nháp" }));
    expect(screen.getByText("Nhập tên chiến dịch")).toBeTruthy();
    expect(screen.getByText("Phần trăm là số nguyên 1–99")).toBeTruthy();
    expect(screen.getByText("Chọn ít nhất một danh mục")).toBeTruthy();
    expect(screen.getByText("Chọn giờ bắt đầu")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });

  it("đổi phạm vi sang sản phẩm: bỏ đích đã chọn, tìm theo kind=product", async () => {
    await openNew();
    fireEvent.click(await screen.findByRole("checkbox", { name: /Đầm công sở/ }));
    fireEvent.change(screen.getByRole("combobox", { name: "Phạm vi" }), { target: { value: "products" } });
    expect(screen.getByText("Đã chọn 0/30")).toBeTruthy();
    await waitFor(() => {
      expect(fake.targetCalls.at(-1)?.get("kind")).toBe("product");
    });
    expect(screen.getByLabelText("Tìm sản phẩm theo mã hoặc tên")).toBeTruthy();
    fireEvent.change(screen.getByRole("combobox", { name: "Phạm vi" }), { target: { value: "all" } });
    expect(screen.queryByText(/Đã chọn/)).toBeNull();
  });

  it("đủ 30 đích: ô chưa chọn bị khóa, câu nhắc bỏ bớt; bỏ một mục (×) thì chọn tiếp được", async () => {
    const many: CampaignTarget[] = Array.from({ length: 31 }, (_, i) => ({
      id: `0195f3b2-0000-7000-8000-0000000d${String(i).padStart(4, "0")}`,
      code: `dm-${String(i)}`,
      name: `Danh mục ${String(i)}`,
    }));
    fake.targets = many;
    await openNew();
    for (let i = 0; i < 30; i++) {
      fireEvent.click(await screen.findByRole("checkbox", { name: new RegExp(`Danh mục ${String(i)} ·`) }));
    }
    expect(screen.getByText("Đã chọn 30/30 — bỏ bớt một mục để chọn thêm.")).toBeTruthy();
    const last = screen.getByRole("checkbox", { name: /Danh mục 30 ·/ });
    expect(last).toHaveProperty("disabled", true);
    fireEvent.click(screen.getByRole("button", { name: "Bỏ Danh mục 0" }));
    expect(screen.getByText("Đã chọn 29/30")).toBeTruthy();
    expect(screen.getByRole("checkbox", { name: /Danh mục 30 ·/ })).toHaveProperty("disabled", false);
  });

  it("danh sách chọn danh mục hiện mã kèm số mẫu (cả 0); ô phần trăm nêu trần của shop", async () => {
    await openNew();
    expect(await screen.findByText("dam-cong-so · 64 mẫu")).toBeTruthy();
    expect(screen.getByText("ao-dai-cach-tan · 0 mẫu")).toBeTruthy();
    expect(await screen.findByText("% · 1–99, trần của shop 50%")).toBeTruthy();
  });

  it("đọc trần lỗi → gợi ý chung, form vẫn dùng được", async () => {
    fake.maxPercent = null;
    await openNew();
    expect(await screen.findByText("% · 1–99, vượt trần của shop thì bị từ chối")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Lưu bản nháp" })).toHaveProperty("disabled", false);
  });

  it("tìm đích gửi q theo kind=category", async () => {
    await openNew();
    type(screen.getByLabelText("Tìm trong toàn bộ danh mục"), "đầm");
    await waitFor(() => {
      expect(fake.targetCalls.at(-1)?.get("q")).toBe("đầm");
    });
    expect(fake.targetCalls.at(-1)?.get("kind")).toBe("category");
    await waitFor(() => {
      expect(screen.queryByRole("checkbox", { name: /Áo dài cách tân/ })).toBeNull();
    });
    expect(screen.getByRole("checkbox", { name: /Đầm dự tiệc/ })).toBeTruthy();
  });
});

describe("Sửa chiến dịch", () => {
  it("mở chiến dịch tạm dừng: điền form, tên đích lấy từ targets của getWebCampaign (không đọc trang đích)", async () => {
    renderAt(`/khuyen-mai/chien-dich/${C2}`);
    expect(await screen.findByDisplayValue("Tri ân khách quen tháng 11")).toBeTruthy();
    expect(field("Phần trăm giảm *")).toHaveProperty("value", "15");
    expect(field("Bắt đầu (giờ Việt Nam) *")).toHaveProperty("value", "2026-11-15T00:00");
    expect(await screen.findByRole("button", { name: "Bỏ Đầm công sở" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Bỏ Áo dài cách tân" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Lưu" })).toBeTruthy();
    // Danh sách đích chỉ đọc cho ô tìm (trang 50), không đọc trang 100 để dò tên đích.
    expect(fake.targetCalls.every((q) => q.get("limit") === "50")).toBe(true);
  });

  it("đích không còn trong danh sách → hiện mục không còn kèm đuôi mã", async () => {
    fake.targets = fake.targets.filter((t) => t.id !== CAT2);
    renderAt(`/khuyen-mai/chien-dich/${C2}`);
    expect(await screen.findByText(`Mục không còn (…${CAT2.slice(-8)})`)).toBeTruthy();
  });

  it("lưu gửi revision đã đọc và nội dung; 409 version_conflict → giữ nguyên chữ, hộp báo, liên kết mở bản mới", async () => {
    renderAt(`/khuyen-mai/chien-dich/${C2}`);
    const name = await screen.findByDisplayValue("Tri ân khách quen tháng 11");
    await screen.findByRole("button", { name: "Bỏ Đầm công sở" });
    type(name, "Tri ân khách quen tháng 12");
    type(field("Phần trăm giảm *"), "20");
    // Người khác vừa lưu: máy chủ đã ở revision 6.
    fake.rows = fake.rows.map((r) => (r.id === C2 ? { ...r, revision: 6 } : r));
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText("Chiến dịch vừa được người khác đổi.")).toBeTruthy();
    expect(fake.writes[0]).toMatchObject({
      method: "PUT",
      path: `/api/v1/web-campaigns/${C2}`,
      body: { revision: 5, campaign: { name: "Tri ân khách quen tháng 12", percent: 20, targetIds: [CAT1, CAT2] } },
    });
    expect(screen.getByDisplayValue("Tri ân khách quen tháng 12")).toBeTruthy();
    expect(field("Phần trăm giảm *")).toHaveProperty("value", "20");
    const open = screen.getByRole("link", { name: "Mở bản mới" });
    expect(open.getAttribute("href")).toBe(`/khuyen-mai/chien-dich/${C2}`);
    expect(open.getAttribute("target")).toBe("_blank");
    expect(fake.writes).toHaveLength(1);
    // Lưu lại: đã đọc bản mới (revision 6), gửi đúng chữ đang gõ.
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Lưu" })).toHaveProperty("disabled", false);
    });
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(2);
    });
    expect(fake.writes[1]?.body).toMatchObject({ revision: 6, campaign: { name: "Tri ân khách quen tháng 12" } });
    expect(await screen.findByText("Đã lưu.")).toBeTruthy();
    expect(screen.queryByText("Chiến dịch vừa được người khác đổi.")).toBeNull();
  });

  it("vượt trần khi lưu → câu dưới ô phần trăm", async () => {
    renderAt(`/khuyen-mai/chien-dich/${C2}`);
    await screen.findByRole("button", { name: "Bỏ Đầm công sở" });
    type(field("Phần trăm giảm *"), "60");
    fake.replies.push({ status: 422, body: { error: "percent_too_high", request_id: "t" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText(/Chiến dịch giảm 60%, vượt trần của shop/)).toBeTruthy();
    type(field("Phần trăm giảm *"), "40");
    expect(screen.queryByText(/Chiến dịch giảm \d+%, vượt trần/)).toBeNull();
  });

  it("Xem trước giá khi form đã sửa: lưu trước rồi xem trước đúng revision mới, hiện giá mẫu", async () => {
    renderAt(`/khuyen-mai/chien-dich/${C2}`);
    await screen.findByRole("button", { name: "Bỏ Đầm công sở" });
    fireEvent.click(await screen.findByRole("checkbox", { name: /Đầm dự tiệc/ }));
    fireEvent.click(screen.getByRole("button", { name: "Xem trước giá" }));
    expect(await screen.findByRole("heading", { name: "Xem trước: 142 mẫu · 610 biến thể" })).toBeTruthy();
    expect(screen.getByText(/phiên bản 6/)).toBeTruthy();
    expect(fake.writes.map((w) => [w.method, w.path, w.body.revision])).toEqual([
      ["PUT", `/api/v1/web-campaigns/${C2}`, 5],
      ["POST", `/api/v1/web-campaigns/${C2}/preview`, 6],
    ]);
    expect(fake.writes[0]?.body).toMatchObject({ campaign: { targetIds: [CAT1, CAT2, CAT3] } });
    const sample = screen.getByRole("row", { name: /Chân váy bút chì/ });
    expect(within(sample).getByText("420.000 ₫")).toBeTruthy();
    expect(within(sample).getByText("357.000 ₫")).toBeTruthy();
    expect(within(sample).getByText("CV0420-DEN-M")).toBeTruthy();
  });

  it("Xem trước giá khi chưa sửa: chỉ xem trước revision đang có", async () => {
    renderAt(`/khuyen-mai/chien-dich/${C2}`);
    await screen.findByRole("button", { name: "Bỏ Đầm công sở" });
    fireEvent.click(screen.getByRole("button", { name: "Xem trước giá" }));
    await screen.findByRole("heading", { name: "Xem trước: 142 mẫu · 610 biến thể" });
    expect(fake.writes.map((w) => w.path)).toEqual([`/api/v1/web-campaigns/${C2}/preview`]);
  });

  it("chiến dịch đang chạy mở thẳng: ô khóa, nút Tạm dừng và sửa mở hộp lý do; tạm dừng xong sửa được", async () => {
    fake.rows = [campaign(), paused()];
    renderAt(`/khuyen-mai/chien-dich/${campaign().id}`);
    expect(await screen.findByText(/Chiến dịch đang chạy/)).toBeTruthy();
    expect(locked(await screen.findByDisplayValue("Xả hàng thu đông"))).toBe(true);
    expect(screen.queryByRole("button", { name: "Lưu" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Tạm dừng và sửa" }));
    const dlg = screen.getByRole("dialog");
    type(within(dlg).getByLabelText("Lý do *"), "Thêm mẫu");
    fireEvent.click(within(dlg).getByRole("button", { name: "Tạm dừng và sửa" }));
    await waitFor(() => {
      expect(locked(screen.getByDisplayValue("Xả hàng thu đông"))).toBe(false);
    });
    expect(screen.getByRole("button", { name: "Lưu" })).toBeTruthy();
  });

  it("không có promotion.manage: form chỉ đọc, không nút Lưu, Xem trước, có câu lý do", async () => {
    fake.grants = grantsOf();
    renderAt(`/khuyen-mai/chien-dich/${C2}`);
    expect(locked(await screen.findByDisplayValue("Tri ân khách quen tháng 11"))).toBe(true);
    expect(screen.queryByRole("button", { name: "Lưu" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Xem trước giá" })).toBeNull();
    expect(screen.getByText(/Bạn chỉ có quyền xem khuyến mãi/)).toBeTruthy();
  });

  it("không có chiến dịch → câu không tìm thấy", async () => {
    renderAt("/khuyen-mai/chien-dich/0195f3b2-0000-7000-8000-00000000ffff");
    expect(await screen.findByText(/Không tìm thấy/)).toBeTruthy();
  });
});
