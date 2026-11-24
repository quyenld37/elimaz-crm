// @vitest-environment jsdom
// Màn Chiến dịch giảm giá: danh sách và hộp thoại kích hoạt, tạm dừng, sửa khi đang chạy, xóa (mockup
// docs/mockups/m5/web-campaigns.html trạng thái A, C, D; HV16, HV17). Kế hoạch M5 Task 458 Step 1: bật khi chưa xem
// trước → nút khóa, máy chủ giả 422 hiện đúng câu; tên 120 ký tự; không promotion.manage → không nút ghi; liên kết
// chép đúng dạng.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  C1,
  C2,
  LONG_NAME,
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
  fake.rows = [
    campaign(),
    campaign({
      id: "0195f3b2-0000-7000-8000-00000000e010",
      name: LONG_NAME,
      percent: 30,
      status: "scheduled",
      allowCoupon: true,
      clearance: false,
    }),
    paused(),
    campaign({
      id: "0195f3b2-0000-7000-8000-00000000e011",
      name: "Bộ sưu tập Tết 2027",
      state: "draft",
      status: "draft",
    }),
    campaign({
      id: "0195f3b2-0000-7000-8000-00000000e012",
      name: "Sale 11.11",
      status: "ended",
      scope: "all",
      targetIds: [],
    }),
  ];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const row = (name: string | RegExp) => screen.findByRole("row", { name });
const type = (el: HTMLElement, v: string) => {
  fireEvent.change(el, { target: { value: v } });
};

describe("Danh sách", () => {
  it("đủ 5 trạng thái; mỗi dòng phạm vi, giảm, giờ VN; thao tác theo trạng thái", async () => {
    renderAt("/khuyen-mai/chien-dich");
    const a = await row(/Xả hàng thu đông/);
    expect(within(a).getByText("Đang áp dụng")).toBeTruthy();
    expect(within(a).getByText("50%")).toBeTruthy();
    expect(within(a).getByText("2 danh mục · Hàng cắt mã")).toBeTruthy();
    expect(within(a).getByText("00:00 15/11 → 23:59 25/11")).toBeTruthy();
    expect(
      within(a)
        .getAllByRole("button")
        .map((b) => b.textContent),
    ).toEqual(["Sửa", "Tạm dừng", "Chép liên kết"]);
    const p = await row(/Tri ân khách quen/);
    expect(within(p).getByText("Tạm dừng")).toBeTruthy();
    expect(within(p).getByRole("link", { name: "Sửa" }).getAttribute("href")).toBe(`/khuyen-mai/chien-dich/${C2}`);
    expect(
      within(p)
        .getAllByRole("button")
        .map((b) => b.textContent),
    ).toEqual(["Kích hoạt", "Xóa"]);
    expect(within(await row(/Bộ sưu tập Tết/)).getByText("Bản nháp")).toBeTruthy();
    const e = await row(/Sale 11.11/);
    expect(e.className).toContain("is-muted");
    expect(within(e).getByText("Toàn bộ sản phẩm · Hàng cắt mã")).toBeTruthy();
    expect(within(e).queryByRole("button", { name: "Chép liên kết" })).toBeNull();
    expect(screen.getByText("5 chiến dịch")).toBeTruthy();
  });

  it("tên 120 ký tự: cắt hai dòng (clamp2), tên đủ trong title; không tràn ngang", async () => {
    renderAt("/khuyen-mai/chien-dich");
    const r = await row(/Siêu sale cuối năm/);
    const name = within(r).getByText(LONG_NAME);
    expect(name.className).toContain("clamp2");
    expect(name.getAttribute("title")).toBe(LONG_NAME);
    expect(within(r).getByText("2 danh mục · dùng kèm mã giảm giá")).toBeTruthy();
  });

  it("tìm, lọc trạng thái, cỡ trang gửi q, status, limit; mặc định 20", async () => {
    renderAt("/khuyen-mai/chien-dich");
    await row(/Xả hàng thu đông/);
    expect(fake.listCalls[0]?.get("limit")).toBe("20");
    type(screen.getByRole("searchbox", { name: "Tìm chiến dịch" }), "tết");
    fireEvent.change(screen.getByRole("combobox", { name: "Trạng thái" }), { target: { value: "draft" } });
    fireEvent.change(screen.getByRole("combobox", { name: "Số dòng mỗi trang" }), { target: { value: "50" } });
    await waitFor(() => {
      const last = fake.listCalls.at(-1);
      expect(last?.get("q")).toBe("tết");
      expect(last?.get("status")).toBe("draft");
      expect(last?.get("limit")).toBe("50");
    });
  });

  it("cỡ trang 10: còn trang sau thì có Tải thêm, tải đủ 12 dòng", async () => {
    fake.rows = Array.from({ length: 12 }, (_, i) =>
      campaign({
        id: `0195f3b2-0000-7000-8000-0000000f${String(i).padStart(4, "0")}`,
        name: `Đợt ${String(i)} cuối tuần`,
      }),
    );
    renderAt("/khuyen-mai/chien-dich");
    await row(/^Đợt 0 cuối tuần/);
    fireEvent.change(screen.getByRole("combobox", { name: "Số dòng mỗi trang" }), { target: { value: "10" } });
    fireEvent.click(await screen.findByRole("button", { name: "Tải thêm" }));
    expect(await row(/^Đợt 11 cuối tuần/)).toBeTruthy();
    expect(screen.getByText("12 chiến dịch")).toBeTruthy();
  });

  it("chép liên kết: đúng https://elimaz.com.vn/uu-dai/<tên>-<khóa>, báo đã chép", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    renderAt("/khuyen-mai/chien-dich");
    fireEvent.click(within(await row(/Xả hàng thu đông/)).getByRole("button", { name: "Chép liên kết" }));
    expect(writeText).toHaveBeenCalledWith("https://elimaz.com.vn/uu-dai/xa-hang-thu-dong-0193a7c2");
    expect(await screen.findByText("Đã chép liên kết: elimaz.com.vn/uu-dai/xa-hang-thu-dong-0193a7c2")).toBeTruthy();
  });

  it("không có promotion.manage: không nút Tạo, Sửa, Kích hoạt, Tạm dừng, Xóa; vẫn chép liên kết; có câu lý do", async () => {
    fake.grants = grantsOf();
    renderAt("/khuyen-mai/chien-dich");
    const a = await row(/Xả hàng thu đông/);
    expect(
      within(a)
        .getAllByRole("button")
        .map((b) => b.textContent),
    ).toEqual(["Chép liên kết"]);
    expect(within(await row(/Tri ân khách quen/)).queryAllByRole("button")).toHaveLength(0);
    expect(within(await row(/Tri ân khách quen/)).queryByRole("link")).toBeNull();
    expect(screen.queryByRole("link", { name: "+ Tạo chiến dịch" })).toBeNull();
    expect(await screen.findByText(/Bạn chỉ có quyền xem khuyến mãi/)).toBeTruthy();
  });

  it("0 chiến dịch: hộp rỗng có nút tạo; lỗi tải hiện câu", async () => {
    fake.rows = [];
    renderAt("/khuyen-mai/chien-dich");
    expect(await screen.findByRole("heading", { name: "Chưa có chiến dịch" })).toBeTruthy();
    expect(screen.getAllByRole("link", { name: "+ Tạo chiến dịch" }).length).toBe(2);
    cleanup();
    vi.mocked(fetch).mockImplementation(() =>
      Promise.resolve(Response.json({ error: "internal", request_id: "t" }, { status: 500 })),
    );
    renderAt("/khuyen-mai/chien-dich");
    expect(await screen.findByText(/Hệ thống gặp lỗi/)).toBeTruthy();
  });
});

async function openEnable() {
  renderAt("/khuyen-mai/chien-dich");
  fireEvent.click(within(await row(/Tri ân khách quen/)).getByRole("button", { name: "Kích hoạt" }));
  return screen.getByRole("dialog", { name: "Kích hoạt: Tri ân khách quen tháng 11" });
}

describe("Kích hoạt", () => {
  it("chưa xem trước xong → nút khóa; xem trước xong + lý do → gửi số đã thấy", async () => {
    let release: () => void = vi.fn();
    fake.hold = new Promise((r) => {
      release = r;
    });
    const dlg = await openEnable();
    const go = within(dlg).getByRole("button", { name: "Kích hoạt" });
    type(within(dlg).getByLabelText("Lý do *"), "Gửi tin tri ân tối nay");
    expect(within(dlg).getByText("Đang xem trước giá…")).toBeTruthy();
    expect(go).toHaveProperty("disabled", true);
    release();
    fake.hold = null;
    expect(await within(dlg).findByText("142 mẫu · 610 biến thể")).toBeTruthy();
    expect(within(dlg).getByText(/Chân váy bút chì/)).toBeTruthy();
    expect(within(dlg).getByText("420.000 ₫")).toBeTruthy();
    expect(within(dlg).getByText("357.000 ₫")).toBeTruthy();
    expect(fake.writes[0]).toMatchObject({ path: `/api/v1/web-campaigns/${C2}/preview`, body: { revision: 5 } });
    expect(go).toHaveProperty("disabled", false);
    fireEvent.click(go);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(2);
    });
    expect(fake.writes[1]).toMatchObject({
      path: `/api/v1/web-campaigns/${C2}/state`,
      body: {
        revision: 5,
        state: "enabled",
        reason: "Gửi tin tri ân tối nay",
        preview: { revision: 5, modelCount: 142, variantCount: 610 },
      },
    });
    expect(fake.writes[1]?.key).toBeTruthy();
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });

  it("thiếu lý do → nút khóa", async () => {
    const dlg = await openEnable();
    await within(dlg).findByText("142 mẫu · 610 biến thể");
    expect(within(dlg).getByRole("button", { name: "Kích hoạt" })).toHaveProperty("disabled", true);
    type(within(dlg).getByLabelText("Lý do *"), "   ");
    expect(within(dlg).getByRole("button", { name: "Kích hoạt" })).toHaveProperty("disabled", true);
  });

  it("422 preview_required → câu phải xem trước lại, nút khóa; Xem trước lại → bật được", async () => {
    const dlg = await openEnable();
    await within(dlg).findByText("142 mẫu · 610 biến thể");
    type(within(dlg).getByLabelText("Lý do *"), "Mở bán");
    fake.replies.push({ status: 422, body: { error: "preview_required", request_id: "t" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Kích hoạt" }));
    expect(await within(dlg).findByText("Danh sách sản phẩm đã đổi kể từ lúc xem trước.")).toBeTruthy();
    expect(within(dlg).getByRole("button", { name: "Kích hoạt" })).toHaveProperty("disabled", true);
    fireEvent.click(within(dlg).getByRole("button", { name: "Xem trước lại" }));
    await waitFor(() => {
      expect(within(dlg).getByRole("button", { name: "Kích hoạt" })).toHaveProperty("disabled", false);
    });
    expect(within(dlg).queryByText("Danh sách sản phẩm đã đổi kể từ lúc xem trước.")).toBeNull();
    expect(fake.writes.map((w) => w.path.split("/").at(-1))).toEqual(["preview", "state", "preview"]);
  });

  it("422 percent_too_high → câu vượt trần nêu phần trăm, nhờ Chủ nâng trần", async () => {
    const dlg = await openEnable();
    await within(dlg).findByText("142 mẫu · 610 biến thể");
    type(within(dlg).getByLabelText("Lý do *"), "Mở bán");
    fake.replies.push({ status: 422, body: { error: "percent_too_high", request_id: "t" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Kích hoạt" }));
    expect(await within(dlg).findByText("Chiến dịch giảm 15%, vượt trần của shop.")).toBeTruthy();
    expect(within(dlg).getByText(/nhờ Chủ nâng trần trước; chiến dịch vẫn ở trạng thái cũ/)).toBeTruthy();
  });

  it("xem trước lỗi → hiện câu và nút Xem trước lại, nút Kích hoạt khóa", async () => {
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    const dlg = await openEnable();
    expect(await within(dlg).findByText(/Người khác vừa sửa mục này/)).toBeTruthy();
    expect(within(dlg).getByRole("button", { name: "Xem trước lại" })).toBeTruthy();
    expect(within(dlg).getByRole("button", { name: "Kích hoạt" })).toHaveProperty("disabled", true);
  });
});

describe("Tạm dừng, sửa khi đang chạy, xóa", () => {
  it("tạm dừng: lý do bắt buộc, gửi state paused", async () => {
    renderAt("/khuyen-mai/chien-dich");
    fireEvent.click(within(await row(/Xả hàng thu đông/)).getByRole("button", { name: "Tạm dừng" }));
    const dlg = screen.getByRole("dialog", { name: "Tạm dừng: Xả hàng thu đông" });
    expect(within(dlg).getByText(/Giá trên website trở về giá gốc ngay/)).toBeTruthy();
    const go = within(dlg).getByRole("button", { name: "Tạm dừng" });
    expect(go).toHaveProperty("disabled", true);
    type(within(dlg).getByLabelText("Lý do *"), "Đổi phạm vi");
    fireEvent.click(go);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({
      path: `/api/v1/web-campaigns/${C1}/state`,
      body: { revision: 3, state: "paused", reason: "Đổi phạm vi" },
    });
    expect(fake.writes[0]?.body).not.toHaveProperty("preview");
  });

  it("409 version_conflict trong hộp: hiện câu, tải lại danh sách để mở lại với bản mới", async () => {
    renderAt("/khuyen-mai/chien-dich");
    fireEvent.click(within(await row(/Xả hàng thu đông/)).getByRole("button", { name: "Tạm dừng" }));
    const dlg = screen.getByRole("dialog");
    type(within(dlg).getByLabelText("Lý do *"), "Đổi phạm vi");
    const before = fake.listCalls.length;
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Tạm dừng" }));
    expect(await within(dlg).findByText(/Người khác vừa sửa mục này/)).toBeTruthy();
    await waitFor(() => {
      expect(fake.listCalls.length).toBeGreaterThan(before);
    });
  });

  it("Sửa khi đang chạy: hộp Tạm dừng và sửa → tạm dừng rồi mở form", async () => {
    renderAt("/khuyen-mai/chien-dich");
    fireEvent.click(within(await row(/Xả hàng thu đông/)).getByRole("button", { name: "Sửa" }));
    const dlg = screen.getByRole("dialog", { name: "Sửa: Xả hàng thu đông" });
    expect(within(dlg).getByText(/Chiến dịch đang chạy/)).toBeTruthy();
    type(within(dlg).getByLabelText("Lý do *"), "Thêm 4 mẫu áo khoác");
    fireEvent.click(within(dlg).getByRole("button", { name: "Tạm dừng và sửa" }));
    expect(await screen.findByRole("heading", { name: "Sửa chiến dịch", level: 1 })).toBeTruthy();
    expect(fake.writes[0]?.body).toMatchObject({ state: "paused", reason: "Thêm 4 mẫu áo khoác" });
  });

  it("xóa: cảnh báo banner, lý do bắt buộc, gửi revision và lý do", async () => {
    renderAt("/khuyen-mai/chien-dich");
    fireEvent.click(within(await row(/Tri ân khách quen/)).getByRole("button", { name: "Xóa" }));
    const dlg = screen.getByRole("dialog", { name: 'Xóa chiến dịch "Tri ân khách quen tháng 11"?' });
    expect(within(dlg).getByText(/đang dùng chiến dịch này/)).toBeTruthy();
    const go = within(dlg).getByRole("button", { name: "Xóa chiến dịch" });
    expect(go).toHaveProperty("disabled", true);
    type(within(dlg).getByLabelText("Lý do *"), "Gộp vào Black Friday");
    fireEvent.click(go);
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({
      path: `/api/v1/web-campaigns/${C2}/delete`,
      body: { revision: 5, reason: "Gộp vào Black Friday" },
    });
  });

  it("xóa chiến dịch vừa bị bật ở nơi khác → 409 campaign_running hiện câu, hộp vẫn mở", async () => {
    renderAt("/khuyen-mai/chien-dich");
    fireEvent.click(within(await row(/Tri ân khách quen/)).getByRole("button", { name: "Xóa" }));
    const dlg = screen.getByRole("dialog");
    type(within(dlg).getByLabelText("Lý do *"), "Không chạy nữa");
    fake.replies.push({ status: 409, body: { error: "campaign_running", request_id: "t" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Xóa chiến dịch" }));
    expect(await within(dlg).findByText(/Chiến dịch đang bật/)).toBeTruthy();
  });
});
