// @vitest-environment jsdom
// Màn Kết nối vận chuyển /settings/carriers (Task 132; mockup docs/mockups/m3/ket-noi-van-chuyen.html trạng thái A, B,
// D, E; HV03, HV04): bảng tài khoản đủ trạng thái, thêm tài khoản (ô khóa xóa sau khi lưu), Thay khóa, chỉ quyền xem,
// 0 tài khoản, hãng chưa nối API → 422.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  LONG_NAME,
  MANAGER,
  accId,
  grantsOf,
  installFake,
  renderAt,
  stubDialog,
  type Fake,
  WH2,
} from "./carriersTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
});
afterEach(() => {
  cleanup();
});

const table = async () => screen.findByRole("table", { name: "Tài khoản hãng" });
const rowOf = (name: string) => {
  const cell = screen.getByText(name);
  const tr = cell.closest("tr");
  if (tr === null) throw new Error(`không có dòng ${name}`);
  return tr;
};

describe("danh sách tài khoản (trạng thái A)", () => {
  it("5 tài khoản (VTP 1, Best 3, SPX 1): trạng thái, nhãn tiếng Việt, kho mặc định, web, lần kiểm", async () => {
    renderAt("/settings/carriers");
    const t = await table();
    expect(within(t).getAllByRole("row")).toHaveLength(6);
    const vtp = rowOf("Viettel Post — Kho chính");
    expect(within(vtp).getByText("Đang hoạt động")).toBeTruthy();
    expect(within(vtp).getByText("Viettel Post · API", { exact: false })).toBeTruthy();
    expect(within(vtp).getByText("Thật")).toBeTruthy();
    expect(vtp.textContent).toContain("Kho chính · Mặc định");
    expect(vtp.textContent).toContain("Có · thứ tự 1");
    expect(vtp.textContent).toContain("09:12 · 21/11 · Đạt");
    const manual = rowOf("Best Express — Kho chính");
    expect(manual.textContent).toContain("Best Express · Nhập mã tay · không gọi API");
    expect(manual.textContent).toContain("Không kiểm (nhập tay)");
    expect(within(rowOf("Best Express — Kho Hà Nội")).getByText("Tạm ngừng")).toBeTruthy();
    expect(rowOf("Best Express — Kho Hà Nội").textContent).toContain("Kho Hà Nội");
    const spx = rowOf("SPX — Kho chính");
    expect(within(spx).getByText("Đã thu hồi")).toBeTruthy();
    expect(spx.className).toContain("is-muted");
    expect(within(spx).getByRole("link", { name: "Xem" })).toBeTruthy();
  });

  it("auth_failed: 'Cần cấp quyền lại', lần kiểm đỏ, nút 'Thay khóa'; tên 60 ký tự gói được (clamp2 wrap-any)", async () => {
    renderAt("/settings/carriers");
    await table();
    const r = rowOf(LONG_NAME);
    expect(r.className).toContain("is-err");
    expect(within(r).getByText("Cần cấp quyền lại").className).toContain("chip-err");
    expect(within(r).getByText("08:00 · 21/11 · Hãng từ chối khóa").className).toContain("err-text");
    expect(within(r).getByRole("button", { name: "Thay khóa" })).toBeTruthy();
    expect(within(r).getByText(LONG_NAME).className).toContain("clamp2 wrap-any");
  });

  it("lọc theo tên, hãng, kho trên trang", async () => {
    renderAt("/settings/carriers");
    const t = await table();
    fireEvent.change(screen.getByLabelText("Hãng"), { target: { value: "best" } });
    expect(within(t).getAllByRole("row")).toHaveLength(4);
    fireEvent.change(screen.getByLabelText("Kho"), { target: { value: WH2 } });
    expect(within(t).getAllByRole("row")).toHaveLength(2);
    fireEvent.change(screen.getByLabelText("Hãng"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Kho"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Tìm tài khoản"), { target: { value: "spx" } });
    expect(within(t).getAllByRole("row")).toHaveLength(2);
  });

  it("Thay khóa từ dòng: ô mật khẩu trống, gửi PUT credentials, đóng hộp, ô khóa không còn giá trị", async () => {
    renderAt("/settings/carriers");
    await table();
    fireEvent.click(within(rowOf(LONG_NAME)).getByRole("button", { name: "Thay khóa" }));
    const dlg = screen.getByRole("dialog", { name: "Thay khóa · Best Express" });
    const token = within(dlg).getByLabelText("Khóa API (token)");
    expect(token.getAttribute("type")).toBe("password");
    expect(token.getAttribute("autocomplete")).toBe("new-password");
    expect((token as HTMLInputElement).value).toBe("");
    fireEvent.change(token, { target: { value: "tk-moi-123" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu khóa mới" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(fake.writes).toEqual([
      {
        method: "PUT",
        path: `/api/v1/carrier-accounts/${accId(3)}/credentials`,
        key: expect.any(String) as string,
        body: { credentials: { token: "tk-moi-123" } },
      },
    ]);
    expect(document.querySelector('input[type="password"]')).toBeNull();
    expect(window.localStorage.length).toBe(0);
  });
});

/** Mở danh sách rồi hộp "Thêm tài khoản hãng". */
async function openAdd() {
  renderAt("/settings/carriers");
  await table();
  fireEvent.click(screen.getByRole("button", { name: "+ Thêm tài khoản" }));
  return screen.getByRole("dialog", { name: "Thêm tài khoản hãng" });
}

describe("thêm tài khoản (trạng thái B, VC01)", () => {
  it("mặc định Nhập mã vận đơn tay: gửi đúng thân, không có khóa", async () => {
    const dlg = await openAdd();
    fireEvent.click(within(dlg).getByLabelText("SPX"));
    expect(within(dlg).getByRole("button", { name: "Nhập mã vận đơn tay" }).getAttribute("aria-pressed")).toBe("true");
    expect(within(dlg).queryByLabelText("Khóa API (token)")).toBeNull();
    fireEvent.change(within(dlg).getByLabelText("Tên tài khoản *"), { target: { value: "SPX — nhập tay" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu tài khoản" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(fake.writes).toEqual([
      {
        method: "POST",
        path: "/api/v1/carrier-accounts",
        key: expect.any(String) as string,
        body: {
          carrierKey: "spx",
          mode: "manual",
          name: "SPX — nhập tay",
          warehouseId: "0195f3b2-0000-7000-8000-0000000000a1",
          environment: "production",
          isDefaultForWarehouse: false,
          showOnWeb: false,
          webSort: 0,
        },
      },
    ]);
  });

  it("Nối API Viettel Post: hai ô mật khẩu; Lưu và kiểm tra → tạo kèm khóa, rồi kiểm; ô khóa trống sau khi lưu", async () => {
    const dlg = await openAdd();
    fireEvent.click(within(dlg).getByRole("button", { name: "Nối API của hãng" }));
    fireEvent.change(within(dlg).getByLabelText("Tên tài khoản *"), { target: { value: "Viettel Post — Kho Hà Nội" } });
    fireEvent.change(within(dlg).getByLabelText("Kho gửi hàng *"), { target: { value: WH2 } });
    fireEvent.change(within(dlg).getByLabelText("Mã khách hàng ở hãng"), { target: { value: "VTP-1029384" } });
    const user = within(dlg).getByLabelText("Tên đăng nhập API");
    const pass = within(dlg).getByLabelText("Mật khẩu API");
    expect([user.getAttribute("type"), pass.getAttribute("type")]).toEqual(["password", "password"]);
    fireEvent.change(user, { target: { value: "shop-api" } });
    fireEvent.change(pass, { target: { value: "bi-mat-1" } });
    fireEvent.click(within(dlg).getByLabelText("Mặc định cho kho này (chọn sẵn khi gửi đơn của kho)"));
    fireEvent.click(within(dlg).getByLabelText("Hiện trên website"));
    fireEvent.change(within(dlg).getByLabelText("Thứ tự trên web"), { target: { value: "4" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu và kiểm tra kết nối" }));
    await screen.findByText(/Đang kiểm tra kết nối…/);
    expect(fake.writes.map((w) => [w.method, w.path])).toEqual([
      ["POST", "/api/v1/carrier-accounts"],
      ["POST", `/api/v1/carrier-accounts/${accId(99)}/check`],
    ]);
    expect(fake.writes[0]?.body).toEqual({
      carrierKey: "vtp",
      mode: "api",
      name: "Viettel Post — Kho Hà Nội",
      warehouseId: WH2,
      environment: "production",
      externalAccountRef: "VTP-1029384",
      isDefaultForWarehouse: true,
      showOnWeb: true,
      webSort: 4,
      credentials: { username: "shop-api", password: "bi-mat-1" },
    });
    expect(fake.writes[0]?.key).not.toBe(fake.writes[1]?.key);
    const left = within(screen.getByRole("dialog", { name: "Thêm tài khoản hãng" }));
    expect(left.queryByLabelText("Tên đăng nhập API")).toBeNull();
    expect(left.queryByLabelText("Mật khẩu API")).toBeNull();
    expect(window.localStorage.length).toBe(0);
  });

  it("hãng chưa nối API → 422 capability_not_supported: câu rõ, khóa vẫn trong ô để chọn nhập tay", async () => {
    fake.replies.push({ status: 422, body: { error: "capability_not_supported", request_id: "t" } });
    const dlg = await openAdd();
    fireEvent.click(within(dlg).getByLabelText("Best Express"));
    fireEvent.click(within(dlg).getByRole("button", { name: "Nối API của hãng" }));
    fireEvent.change(within(dlg).getByLabelText("Tên tài khoản *"), { target: { value: "Best API" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu nháp" }));
    const alert = await within(dlg).findByRole("alert");
    expect(alert.textContent).toContain("Hãng này chưa nối API trong CRM");
    expect(alert.textContent).toContain("Nhập mã vận đơn tay");
    expect(fake.writes).toHaveLength(1);
  });

  it("thiếu tên → báo tại ô, không gửi", async () => {
    const dlg = await openAdd();
    fireEvent.click(within(dlg).getByRole("button", { name: "Lưu tài khoản" }));
    expect(within(dlg).getByText("Nhập tên tài khoản")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
  });
});

describe("quyền và trạng thái rỗng (trạng thái E)", () => {
  it("chỉ quyền xem: không nút Thêm, Sửa, Thay khóa; mỗi dòng có Xem", async () => {
    fake.grants = grantsOf("fulfillment.carrier_accounts.read");
    renderAt("/settings/carriers");
    const t = await table();
    expect(screen.queryByRole("button", { name: "+ Thêm tài khoản" })).toBeNull();
    expect(within(t).queryByRole("link", { name: "Sửa" })).toBeNull();
    expect(within(t).queryByRole("button", { name: "Thay khóa" })).toBeNull();
    expect(within(t).getAllByRole("link", { name: "Xem" })).toHaveLength(5);
  });

  it("0 tài khoản: 'Chưa kết nối hãng nào' và nút thêm (người có quyền manage)", async () => {
    fake.accounts = [];
    renderAt("/settings/carriers");
    expect(await screen.findByRole("heading", { name: "Chưa kết nối hãng nào" })).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "+ Thêm tài khoản" })).toHaveLength(2);
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("đọc danh sách lỗi → hộp lỗi, không vẽ như rỗng", async () => {
    fake.grants = grantsOf(...MANAGER);
    fake.accounts = [];
    renderAt("/settings/carriers");
    await screen.findByRole("heading", { name: "Chưa kết nối hãng nào" });
    cleanup();
    fake.listCalls = [];
    installFakeError();
    renderAt("/settings/carriers");
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Chưa kết nối hãng nào" })).toBeNull();
  });
});

function installFakeError() {
  const base = globalThis.fetch;
  globalThis.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = input instanceof Request ? input : new Request(input, init);
    if (new URL(req.url).pathname === "/api/v1/carrier-accounts") {
      return Response.json({ error: "forbidden", request_id: "t" }, { status: 403 });
    }
    return base(req);
  };
}
