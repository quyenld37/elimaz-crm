// @vitest-environment jsdom
// Trang sửa tài khoản /settings/carriers/:id (Task 132; mockup docs/mockups/m3/ket-noi-van-chuyen.html trạng thái C, D,
// E): sửa chỉ gửi trường đổi kèm version, 409 → tải lại; Kiểm tra kết nối → "Đang kiểm tra" rồi cập nhật theo báo hiệu;
// URL webhook chỉ cho quyền manage; địa giới chưa khớp, đồng bộ; địa chỉ lấy hàng của kho; tạm ngừng, thu hồi.
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import { MANAGER, WH, account, accId, grantsOf, installFake, renderAt, stubDialog, type Fake } from "./carriersTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
});
afterEach(() => {
  cleanup();
});

const open = async (n: number) => {
  const view = renderAt(`/settings/carriers/${accId(n)}`);
  await screen.findByRole("region", { name: "Thông tin tài khoản" });
  return view;
};

describe("sửa tài khoản (trạng thái C)", () => {
  it("chỉ gửi trường đã đổi kèm version đang xem", async () => {
    await open(1);
    const name = screen.getByLabelText("Tên tài khoản *");
    expect((name as HTMLInputElement).value).toBe("Viettel Post — Kho chính");
    fireEvent.change(name, { target: { value: "Viettel Post — Kho chính 2" } });
    fireEvent.change(screen.getByLabelText("Thứ tự trên web"), { target: { value: "2" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu thay đổi" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toEqual({
      method: "PATCH",
      path: `/api/v1/carrier-accounts/${accId(1)}`,
      key: expect.any(String) as string,
      body: { version: 3, name: "Viettel Post — Kho chính 2", webSort: 2 },
    });
  });

  it("không đổi gì → nút Lưu thay đổi khóa", async () => {
    await open(1);
    expect(screen.getByRole("button", { name: "Lưu thay đổi" }).hasAttribute("disabled")).toBe(true);
  });

  it("409 version_conflict → câu người khác vừa sửa, nút Tải lại", async () => {
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    await open(1);
    fireEvent.change(screen.getByLabelText("Tên tài khoản *"), { target: { value: "Khác" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu thay đổi" }));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Tài khoản vừa được người khác sửa");
    expect(within(alert).getByRole("button", { name: "Tải lại" })).toBeTruthy();
  });

  it("khóa đã lưu chỉ có 'Thay khóa' (ô trống); URL webhook có nút sao chép; việc hãng làm được", async () => {
    await open(1);
    expect(screen.getByText("Đã lưu khóa")).toBeTruthy();
    expect(document.querySelector('input[type="password"]')).toBeNull();
    const url = screen.getByLabelText("URL webhook");
    expect((url as HTMLInputElement).value).toBe(
      "https://crm.elimaz.com.vn/hooks/vtp/q7Hk2mP0sVx9LrT4bNc8WdYe1Za6Uf3G",
    );
    expect(url.hasAttribute("readonly")).toBe(true);
    expect(screen.getByRole("button", { name: "Sao chép" })).toBeTruthy();
    expect(screen.getByText(/Sửa địa chỉ sau khi gửi:/).textContent).toContain("có");
    fireEvent.click(screen.getByRole("button", { name: "Thay khóa" }));
    const dlg = screen.getByRole("dialog", { name: "Thay khóa · Viettel Post" });
    expect(
      within(dlg)
        .getAllByLabelText(/API$/)
        .map((i) => (i as HTMLInputElement).value),
    ).toEqual(["", ""]);
  });

  it("khóa đang mã hóa bằng khóa cũ (needsRewrap) → hộp nhắc nhập lại", async () => {
    fake.accounts = [account(1, { needsRewrap: true })];
    await open(1);
    expect(screen.getByText(/Khóa đang mã hóa bằng khóa cũ của hệ thống/)).toBeTruthy();
  });

  it("Kiểm tra kết nối → 'Đang kiểm tra' rồi cập nhật khi nhận báo hiệu fulfillment.carrier_account", async () => {
    const { qc } = await open(1);
    const card = screen.getByRole("region", { name: "Kiểm tra kết nối" });
    expect(card.textContent).toContain("09:12 · 21/11 · Đạt");
    fireEvent.click(within(card).getByRole("button", { name: "Kiểm tra kết nối" }));
    await within(card).findByText("Đang kiểm tra…");
    expect(fake.writes.map((w) => [w.method, w.path])).toEqual([
      ["POST", `/api/v1/carrier-accounts/${accId(1)}/check`],
    ]);
    expect(within(card).getByRole("button", { name: "Kiểm tra kết nối" }).hasAttribute("disabled")).toBe(true);
    const first = fake.accounts[0];
    if (first === undefined) throw new Error("thiếu tài khoản");
    fake.accounts = [
      { ...first, status: "auth_failed", lastCheckAt: "2026-11-21T03:30:00Z", lastCheckResult: "unauthorized" },
    ];
    act(() => {
      onRealtimeChanged(qc, {
        topic: "fulfillment.carrier_account",
        entity: { type: "carrier_account", id: accId(1) },
      });
    });
    await within(card).findByText("10:30 · 21/11 · Hãng từ chối khóa");
    expect(within(card).queryByText("Đang kiểm tra…")).toBeNull();
    expect(screen.getAllByText("Cần cấp quyền lại").length).toBeGreaterThan(0);
  });

  it("Tạm ngừng gửi trạng thái paused kèm version; Thu hồi hỏi lại rồi gửi revoked", async () => {
    await open(1);
    fireEvent.click(screen.getByRole("button", { name: "Tạm ngừng" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({ status: "paused", version: 3 });
    expect(fake.writes[0]?.path).toBe(`/api/v1/carrier-accounts/${accId(1)}/status`);
    fireEvent.click(screen.getByRole("button", { name: "Thu hồi" }));
    const dlg = screen.getByRole("dialog", { name: "Thu hồi tài khoản" });
    expect(fake.writes).toHaveLength(1);
    fireEvent.click(within(dlg).getByRole("button", { name: "Thu hồi hẳn" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(2);
    });
    expect(fake.writes[1]?.body).toEqual({ status: "revoked", version: 3 });
  });

  it("tài khoản tạm ngừng có 'Bật lại' (active)", async () => {
    fake.accounts = [account(1, { status: "paused" })];
    await open(1);
    fireEvent.click(screen.getByRole("button", { name: "Bật lại" }));
    await waitFor(() => {
      expect(fake.writes[0]?.body).toEqual({ status: "active", version: 3 });
    });
  });

  it("không có tài khoản → câu không tìm thấy", async () => {
    renderAt(`/settings/carriers/${accId(77)}`);
    expect(await screen.findByText("Không có tài khoản hãng này, hoặc bạn không được xem.")).toBeTruthy();
  });
});

describe("cần cấp quyền lại, đã thu hồi (trạng thái D, VC15)", () => {
  it("auth_failed: hộp đỏ 'không tự chuyển sang tài khoản khác'", async () => {
    await open(3);
    const box = screen.getByText(/Cần cấp quyền lại\./).closest(".box");
    expect(box?.textContent).toContain("CRM không tự chuyển sang tài khoản khác");
  });

  it("revoked: chỉ xem, không nút sửa, không đổi trạng thái", async () => {
    await open(5);
    expect(screen.getByText(/Đã thu hồi/, { selector: "b" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Lưu thay đổi" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Tạm ngừng" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Thay khóa" })).toBeNull();
  });
});

describe("chỉ quyền xem (trạng thái E)", () => {
  it("không nút sửa, thay khóa, kiểm tra; không URL webhook; câu quyền", async () => {
    fake.grants = grantsOf("fulfillment.carrier_accounts.read");
    const first = fake.accounts[0];
    if (first === undefined) throw new Error("thiếu tài khoản");
    fake.accounts = [{ ...first, webhookUrl: undefined }];
    await open(1);
    expect(screen.queryByRole("button", { name: "Lưu thay đổi" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Thay khóa" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Kiểm tra kết nối" })).toBeNull();
    expect(screen.queryByLabelText("URL webhook")).toBeNull();
    expect(screen.queryByLabelText("Tên tài khoản *")).toBeNull();
    expect(screen.getByText(/Bạn chỉ có quyền xem kết nối vận chuyển/)).toBeTruthy();
    expect(screen.getByText("Kho chính · Mặc định")).toBeTruthy();
  });
});

describe("địa giới và địa chỉ lấy hàng", () => {
  it("3 địa giới chưa khớp theo hãng; Đồng bộ địa giới gửi lệnh nền", async () => {
    fake.unmapped = [
      { level: "ward", ourCode: "26734", name: "Phường Vĩnh Hưng", parentCode: "01" },
      { level: "ward", ourCode: "31120", name: "Xã Đảo Thạnh An", parentCode: "79" },
      { level: "province", ourCode: "31", name: "Hải Phòng" },
    ];
    await open(1);
    const card = screen.getByRole("region", { name: "Địa giới của hãng" });
    expect(await within(card).findByText("3 địa giới chưa khớp")).toBeTruthy();
    expect(within(card).getAllByRole("row")).toHaveLength(4);
    expect(within(card).getByText("Phường Vĩnh Hưng").closest("tr")?.textContent).toContain("Hà Nội");
    fireEvent.click(within(card).getByRole("button", { name: "Đồng bộ địa giới" }));
    await within(card).findByText(/Đã gửi việc đồng bộ/);
    expect(fake.writes.map((w) => [w.method, w.path])).toEqual([
      ["POST", `/api/v1/carrier-accounts/${accId(1)}/locations/sync`],
    ]);
  });

  it("địa chỉ lấy hàng đang lưu hiện trong ô; lưu gửi đủ trường kèm version", async () => {
    fake.grants = grantsOf(...MANAGER, "fulfillment.settings.manage");
    fake.pickup = {
      warehouseId: WH,
      version: 5,
      name: "Kho ELIMAZ",
      phone: "+84987654321",
      line: "18 ngõ 120 Trường Chinh",
      provinceCode: "01",
      wardCode: "00070",
    };
    fake.replies.push({ status: 200, body: { ...fake.pickup, line: "20 Trường Chinh", version: 6 } });
    await open(1);
    const card = screen.getByRole("region", { name: "Địa chỉ lấy hàng · Kho chính" });
    const who = await within(card).findByDisplayValue("Kho ELIMAZ");
    expect(who).toBeTruthy();
    fireEvent.change(within(card).getByLabelText("Địa chỉ *"), { target: { value: "20 Trường Chinh" } });
    fireEvent.click(within(card).getByRole("button", { name: "Lưu địa chỉ lấy hàng" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toEqual({
      method: "PUT",
      path: `/api/v1/inventory/warehouses/${WH}/pickup`,
      key: expect.any(String) as string,
      body: {
        version: 5,
        name: "Kho ELIMAZ",
        phone: "+84987654321",
        line: "20 Trường Chinh",
        provinceCode: "01",
        wardCode: "00070",
      },
    });
    expect(await within(card).findByText("Đã lưu địa chỉ lấy hàng.")).toBeTruthy();
  });

  it("không có quyền cài đặt vận chuyển → không có thẻ địa chỉ lấy hàng", async () => {
    await open(1);
    expect(screen.queryByRole("region", { name: /Địa chỉ lấy hàng/ })).toBeNull();
  });
});
