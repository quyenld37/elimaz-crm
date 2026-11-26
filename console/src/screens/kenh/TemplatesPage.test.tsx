// @vitest-environment jsdom
// Mẫu trả lời và Nhãn (kế hoạch M4 Task 178 Step 1; mockup cai-dat.html trạng thái C; HV55): lỗi 422 của API hiện tại
// đúng ô, bảng biến dưới ô nội dung, tên trùng báo tại ô, sửa chỉ gửi trường đã đổi.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderScreens } from "../../app/screenTestkit";
import { P1, T1, grant, setupKenh } from "./kenhTestkit";
import { screens } from "./route";

const t = setupKenh();

const editor = async (title: string) => screen.findByRole("region", { name: new RegExp(`^${title}`) });
const type = (el: HTMLElement, value: string) => {
  fireEvent.change(el, { target: { value } });
};

describe("Mẫu trả lời", () => {
  async function newTemplate() {
    renderScreens(screens, "/settings/templates");
    fireEvent.click(await screen.findByRole("button", { name: "+ Mẫu" }));
    const e = await editor("Mẫu mới");
    type(within(e).getByLabelText(/^Phím tắt \*/), "xin-dia-chi-2");
    type(within(e).getByLabelText(/^Tên mẫu \*/), "Xin địa chỉ");
    return e;
  }

  it("danh sách: phím tắt, tên, phạm vi", async () => {
    renderScreens(screens, "/settings/templates");
    const item = await screen.findByRole("button", { name: "/xin-dia-chi · Xin địa chỉ live" });
    expect(within(item.closest("li") ?? document.body).getByText("Mọi Page")).toBeTruthy();
  });

  it("mẫu có {abc} → API 422 → lỗi hiện dưới ô nội dung", async () => {
    t.fake.replies.push({ status: 422, body: { error: "invalid_input", request_id: "r" } });
    const e = await newTemplate();
    const body = within(e).getByRole("textbox", { name: /^Nội dung \*/ });
    type(body, "Chào {ten_khach}, shop giữ đơn {ma_don} cho bạn. {abc}");
    fireEvent.click(within(e).getByRole("button", { name: "Lưu mẫu" }));
    const err = await within(e).findByText("Biến {abc} không có. Dùng các biến ở bảng dưới.");
    expect(err.previousElementSibling).toBe(body);
    expect(body.getAttribute("aria-invalid")).toBe("true");
    expect(t.fake.writes[0]?.body).toEqual({
      shortcut: "xin-dia-chi-2",
      title: "Xin địa chỉ",
      body: "Chào {ten_khach}, shop giữ đơn {ma_don} cho bạn. {abc}",
    });
    expect(within(e).queryByRole("alert")).toBeNull();
  });

  it("bảng biến ngay dưới ô nội dung có đủ năm biến", async () => {
    const e = await newTemplate();
    for (const v of ["{ten_khach}", "{ten_page}", "{ma_don}", "{nhan_vien}", "{ma_van_don}"]) {
      expect(within(e).getByText(v)).toBeTruthy();
    }
  });

  it("422 không do biến lạ → hộp lỗi chung của khối", async () => {
    t.fake.replies.push({ status: 422, body: { error: "invalid_input", request_id: "r" } });
    const e = await newTemplate();
    type(within(e).getByRole("textbox", { name: /^Nội dung \*/ }), "Chào {ten_khach}");
    fireEvent.click(within(e).getByRole("button", { name: "Lưu mẫu" }));
    expect(await within(e).findByRole("alert")).toBeTruthy();
  });

  it("phím tắt trùng (409 already_exists) → báo tại ô Phím tắt", async () => {
    t.fake.replies.push({ status: 409, body: { error: "already_exists", request_id: "r" } });
    const e = await newTemplate();
    type(within(e).getByRole("textbox", { name: /^Nội dung \*/ }), "Chào");
    fireEvent.click(within(e).getByRole("button", { name: "Lưu mẫu" }));
    expect(await within(e).findByText("Phím tắt /xin-dia-chi-2 đã có.")).toBeTruthy();
  });

  it("phím tắt sai dạng, nội dung rỗng → báo tại ô, không gửi", async () => {
    const e = await newTemplate();
    type(within(e).getByLabelText(/^Phím tắt \*/), "Xin Dia");
    fireEvent.click(within(e).getByRole("button", { name: "Lưu mẫu" }));
    expect(within(e).getByText("Chỉ a–z, 0–9, - và _; tối đa 32 ký tự.")).toBeTruthy();
    expect(within(e).getByText(/Nhập nội dung/)).toBeTruthy();
    expect(t.fake.writes).toHaveLength(0);
  });

  it("sửa chỉ gửi trường đã đổi kèm version; 409 version_conflict → 'Có người vừa sửa, tải lại trang'", async () => {
    t.fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "r" } });
    renderScreens(screens, "/settings/templates");
    fireEvent.click(await screen.findByRole("button", { name: "/xin-dia-chi · Xin địa chỉ live" }));
    const e = await editor("Sửa mẫu /xin-dia-chi");
    type(within(e).getByRole("textbox", { name: /^Nội dung \*/ }), "Chào {ten_khach} nhé");
    fireEvent.click(within(e).getByRole("button", { name: "Lưu mẫu" }));
    expect(await within(e).findByText("Có người vừa sửa, tải lại trang.")).toBeTruthy();
    expect(t.fake.writes[0]).toMatchObject({
      method: "PATCH",
      path: `/api/v1/reply-templates/${T1}`,
      body: { body: "Chào {ten_khach} nhé", expectedVersion: 1 },
    });
    expect(Object.keys(t.fake.writes[0]?.body ?? {}).sort()).toEqual(["body", "expectedVersion"]);
  });

  it("chỉ có quyền trên một Page → mẫu mới mặc định cho Page đó, không có 'Mọi Page'", async () => {
    t.fake.grants = [grant("conversation.read"), grant("conversation.settings", { type: "page", id: P1 })];
    renderScreens(screens, "/settings/templates");
    fireEvent.click(await screen.findByRole("button", { name: "+ Mẫu" }));
    const e = await editor("Mẫu mới");
    const scope = within(e).getByLabelText<HTMLSelectElement>("Áp cho Page");
    expect(scope.value).toBe(P1);
    expect([...scope.options].map((o) => o.text)).toEqual(["ELIMAZ"]);
  });
});

describe("Nhãn", () => {
  it("tên trùng (không phân biệt hoa thường) → báo tại ô trước khi gửi", async () => {
    renderScreens(screens, "/settings/tags");
    fireEvent.click(await screen.findByRole("button", { name: "+ Nhãn" }));
    const e = await editor("Nhãn mới");
    type(within(e).getByLabelText(/^Tên nhãn/), "đã lên đơn");
    fireEvent.click(within(e).getByRole("button", { name: "Lưu nhãn" }));
    expect(within(e).getByText('Nhãn "Đã lên đơn" đã có.')).toBeTruthy();
    expect(t.fake.writes).toHaveLength(0);
  });

  it("tạo nhãn: tên và màu từ bốn sắc có sẵn", async () => {
    renderScreens(screens, "/settings/tags");
    fireEvent.click(await screen.findByRole("button", { name: "+ Nhãn" }));
    const e = await editor("Nhãn mới");
    expect(
      within(e)
        .getAllByRole("radio")
        .map((r) => r.closest("label")?.textContent.trim()),
    ).toEqual(["Hồng", "Xám", "Xanh", "Viền"]);
    type(within(e).getByLabelText(/^Tên nhãn/), "VIP");
    fireEvent.click(within(e).getByRole("radio", { name: "Xanh" }));
    fireEvent.click(within(e).getByRole("button", { name: "Lưu nhãn" }));
    await waitFor(() => {
      expect(t.fake.writes[0]).toMatchObject({
        method: "POST",
        path: "/api/v1/conversation-tags",
        body: { name: "VIP", color: "success" },
      });
    });
  });

  it("409 already_exists từ máy chủ → báo tại ô Tên nhãn", async () => {
    t.fake.replies.push({ status: 409, body: { error: "already_exists", request_id: "r" } });
    renderScreens(screens, "/settings/tags");
    fireEvent.click(await screen.findByRole("button", { name: "+ Nhãn" }));
    const e = await editor("Nhãn mới");
    type(within(e).getByLabelText(/^Tên nhãn/), "Khách sỉ");
    fireEvent.click(within(e).getByRole("button", { name: "Lưu nhãn" }));
    expect(await within(e).findByText('Nhãn "Khách sỉ" đã có.')).toBeTruthy();
    expect(within(e).queryByRole("alert")).toBeNull();
  });

  it("sửa màu chỉ gửi color kèm version; xóa hỏi lại rồi gọi DELETE", async () => {
    renderScreens(screens, "/settings/tags");
    fireEvent.click(await screen.findByRole("button", { name: "Sửa nhãn Hỏi size" }));
    const e = await editor("Sửa nhãn Hỏi size");
    fireEvent.click(within(e).getByRole("radio", { name: "Viền" }));
    fireEvent.click(within(e).getByRole("button", { name: "Lưu nhãn" }));
    await waitFor(() => {
      expect(t.fake.writes[0]).toMatchObject({ method: "PATCH", body: { color: "edge", expectedVersion: 1 } });
    });
    expect(Object.keys(t.fake.writes[0]?.body ?? {}).sort()).toEqual(["color", "expectedVersion"]);
    fireEvent.click(await screen.findByRole("button", { name: "Sửa nhãn Hỏi size" }));
    const e2 = await editor("Sửa nhãn Hỏi size");
    fireEvent.click(within(e2).getByRole("button", { name: "Xóa nhãn" }));
    const dlg = screen.getByRole("dialog", { name: "Xóa nhãn Hỏi size?" });
    fireEvent.click(within(dlg).getByRole("button", { name: "Xóa nhãn" }));
    await waitFor(() => {
      expect(t.fake.writes[1]).toMatchObject({ method: "DELETE", path: "/api/v1/conversation-tags/tag2" });
    });
  });

  it("không có quyền cài đặt cả shop → chỉ xem, không có nút + Nhãn", async () => {
    t.fake.grants = [grant("conversation.read"), grant("conversation.settings", { type: "page", id: P1 })];
    renderScreens(screens, "/settings/tags");
    expect(await screen.findByText("Bạn chỉ xem được nhãn.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "+ Nhãn" })).toBeNull();
    expect(screen.getByText("Hỏi size")).toBeTruthy();
  });
});
