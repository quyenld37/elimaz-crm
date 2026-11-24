// @vitest-environment jsdom
// Màn Khóa dịch vụ (Task 208, M5-25; mockup docs/mockups/m5/service-keys.html trạng thái A–D; HV13). Kế hoạch Step 1:
// tạo khóa → hộp hiện khóa; gửi lại cùng khóa chống trùng nhận 409 already_processed → hộp báo không hiện lại được;
// khóa không bao giờ vào localStorage, sessionStorage hay log của trình duyệt; thu hồi cần xác nhận; 409 → tải lại.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { reply } from "../../app/screenTestkit";
import { K2, K3, LONG_NAME, SECRET, installFake, renderAt, stubDialog, type Fake } from "./serviceKeysTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.clear();
  sessionStorage.clear();
});

const table = async () => screen.findByRole("table", { name: "Khóa dịch vụ" });
const rowOf = (name: string) => {
  const tr = screen.getByText(name).closest("tr");
  if (tr === null) throw new Error(`không có dòng ${name}`);
  return tr;
};
const dialog = (name: string) => screen.findByRole("dialog", { name });

async function openCreate() {
  renderAt();
  await table();
  fireEvent.click(screen.getByRole("button", { name: "+ Tạo khóa" }));
  return dialog("Tạo khóa dịch vụ");
}

describe("danh sách (trạng thái A)", () => {
  it("4 khóa: tiền tố (không khóa đầy đủ), quyền, IP, lần dùng cuối, trạng thái; thu hồi chỉ ở khóa đang dùng", async () => {
    renderAt();
    const t = await table();
    expect(within(t).getAllByRole("row")).toHaveLength(5);
    const main = rowOf("Website ELIMAZ — bản chính");
    expect(main.textContent).toContain("ek_aaaabbbb0001_…");
    expect(main.textContent).toContain("9 quyền website");
    expect(main.textContent).toContain("Mọi địa chỉ");
    expect(main.textContent).toContain("08:14 · 21/11");
    expect(main.textContent).toContain("Tạo 14/11/2026");
    expect(within(main).getByText("Đang dùng")).toBeTruthy();
    expect(within(main).getByRole("button", { name: "Thu hồi" })).toBeTruthy();
    expect(rowOf("Website ELIMAZ — bản thử (staging)").textContent).toContain("13.140.64.56/32");
    const long = rowOf(LONG_NAME);
    expect(long.textContent).toContain("Đọc danh mục");
    expect(long.textContent).toContain("Tạo đơn web");
    expect(long.textContent).toContain("Chưa dùng");
    expect(screen.getByText(LONG_NAME).className).toContain("wrap-any");
    const old = rowOf("Thử nối website ngày 10/11");
    expect(old.className).toContain("is-muted");
    expect(within(old).getByText("Đã thu hồi 11/11")).toBeTruthy();
    expect(within(old).queryByRole("button")).toBeNull();
    expect(t.textContent).not.toMatch(/ek_[a-z0-9]{12}_[A-Z0-9]/);
  });

  it("0 khóa → chưa có khóa nào, nút tạo khóa", async () => {
    fake.keys = [];
    renderAt();
    expect(await screen.findByText("Chưa có khóa nào")).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "+ Tạo khóa" }).length).toBeGreaterThan(0);
  });

  it("lỗi tải → hộp lỗi, không vẽ như 0 khóa", async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      if (input instanceof Request && input.url.endsWith("/api/v1/me")) {
        return Promise.resolve(reply({ userId: "me", email: "a@elimaz.vn", grants: fake.grants }));
      }
      return Promise.resolve(reply({ error: "forbidden", request_id: "t" }, 403));
    });
    renderAt();
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.queryByText("Chưa có khóa nào")).toBeNull();
  });
});

describe("tạo khóa (trạng thái B, C)", () => {
  it("mặc định tích 8 quyền (trừ Mua flash sale); gửi tên, quyền, IP; hộp hiện khóa một lần, Chép; đóng là mất", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const d = await openCreate();
    const boxes = within(d).getAllByRole<HTMLInputElement>("checkbox");
    expect(boxes).toHaveLength(9);
    expect(boxes.filter((b) => b.checked)).toHaveLength(8);
    expect(within(d).getByRole<HTMLInputElement>("checkbox", { name: /Mua flash sale/ }).checked).toBe(false);
    fireEvent.change(within(d).getByRole("textbox", { name: "Tên khóa" }), {
      target: { value: "  Website ELIMAZ — bản mới  " },
    });
    fireEvent.change(within(d).getByRole("textbox", { name: "IP được gọi" }), {
      target: { value: "13.140.64.56/32\n\n 10.0.0.0/8 " },
    });
    fireEvent.click(within(d).getByRole("button", { name: "Tạo khóa" }));
    const once = await dialog('Đã tạo khóa "Website ELIMAZ — bản mới"');
    expect(fake.writes).toHaveLength(1);
    expect(fake.writes[0]?.key).toMatch(/^[A-Za-z0-9_-]{1,128}$/);
    expect(fake.writes[0]?.body).toEqual({
      name: "Website ELIMAZ — bản mới",
      permissions: [
        "web.catalog.read",
        "web.orders.create",
        "web.orders.read",
        "web.orders.cancel",
        "web.requests.write",
        "web.coupons.issue",
        "web.birthdays.write",
        "web.shipping.quote",
      ],
      allowedCidrs: ["13.140.64.56/32", "10.0.0.0/8"],
    });
    const field = within(once).getByRole<HTMLInputElement>("textbox", { name: "Khóa dịch vụ" });
    expect(field.value).toBe(SECRET);
    expect(field.readOnly).toBe(true);
    expect(once.textContent).toContain("Khóa chỉ hiện một lần này.");
    fireEvent.click(within(once).getByRole("button", { name: "Chép" }));
    expect(await within(once).findByText("Đã chép vào bộ nhớ tạm.")).toBeTruthy();
    expect(writeText).toHaveBeenCalledWith(SECRET);
    fireEvent.click(within(once).getByRole("button", { name: "Tôi đã chép, đóng" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(document.body.innerHTML).not.toContain(SECRET);
    await waitFor(() => {
      expect(fake.listCalls).toBeGreaterThan(1);
    });
  });

  it("khóa không vào localStorage, sessionStorage, bộ đệm truy vấn, log của trình duyệt", async () => {
    const logs = (["log", "info", "warn", "error", "debug"] as const).map((m) =>
      vi.spyOn(console, m).mockImplementation(() => undefined),
    );
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    const d = await openCreate();
    fireEvent.change(within(d).getByRole("textbox", { name: "Tên khóa" }), { target: { value: "Khóa thử" } });
    fireEvent.click(within(d).getByRole("button", { name: "Tạo khóa" }));
    await dialog('Đã tạo khóa "Khóa thử"');
    const stored = [localStorage, sessionStorage].flatMap((s) =>
      Array.from({ length: s.length }, (_, i) => `${s.key(i) ?? ""}=${s.getItem(s.key(i) ?? "") ?? ""}`),
    );
    expect(stored.join("\n")).not.toContain(SECRET);
    for (const c of setItem.mock.calls) expect(c.join("=")).not.toContain(SECRET);
    for (const spy of logs) for (const c of spy.mock.calls) expect(JSON.stringify(c)).not.toContain(SECRET);
  });

  it("bộ đệm React Query không giữ khóa", async () => {
    fake.keys = [];
    const view = renderAt();
    await screen.findByText("Chưa có khóa nào");
    fireEvent.click(screen.getAllByRole("button", { name: "+ Tạo khóa" })[0] ?? document.body);
    const d = await dialog("Tạo khóa dịch vụ");
    fireEvent.change(within(d).getByRole("textbox", { name: "Tên khóa" }), { target: { value: "Khóa thử" } });
    fireEvent.click(within(d).getByRole("button", { name: "Tạo khóa" }));
    await dialog('Đã tạo khóa "Khóa thử"');
    await waitFor(() => {
      expect(fake.listCalls).toBeGreaterThan(1);
    });
    const cached = view.qc
      .getQueryCache()
      .getAll()
      .map((q) => JSON.stringify(q.state.data ?? null));
    expect(cached.join("\n")).not.toContain(SECRET);
    const mutations = view.qc
      .getMutationCache()
      .getAll()
      .map((m) => JSON.stringify(m.state.data ?? null));
    expect(mutations.join("\n")).not.toContain(SECRET);
  });

  it("mạng rớt, gửi lại cùng khóa chống trùng → 409 already_processed: hộp báo không hiện lại được, chỉ một khóa", async () => {
    const real = vi.mocked(fetch).getMockImplementation();
    if (real === undefined) throw new Error("thiếu API giả");
    let dropped = false;
    vi.mocked(fetch).mockImplementation(async (input) => {
      if (!dropped && input instanceof Request && input.method === "POST") {
        dropped = true;
        const path = new URL(input.url).pathname;
        fake.writes.push({ method: "POST", path, key: input.headers.get("Idempotency-Key"), body: {} });
        throw new TypeError("Failed to fetch");
      }
      return real(input);
    });
    const d = await openCreate();
    fake.replies.push({ status: 409, body: { error: "already_processed", request_id: "t" } });
    fireEvent.change(within(d).getByRole("textbox", { name: "Tên khóa" }), {
      target: { value: "Website ELIMAZ — bản chính" },
    });
    fireEvent.click(within(d).getByRole("button", { name: "Tạo khóa" }));
    const box = await screen.findByText(
      "Khóa đã được tạo ở lần bấm trước, không hiện lại được.",
      {},
      { timeout: 3000 },
    );
    expect(box.closest(".box")?.textContent).toContain(
      'thu hồi khóa "Website ELIMAZ — bản chính" trong danh sách rồi tạo khóa mới',
    );
    expect(fake.writes).toHaveLength(2);
    expect(fake.writes[0]?.key).toBe(fake.writes[1]?.key);
    expect(screen.queryByRole("textbox", { name: "Khóa dịch vụ" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Về danh sách" }));
    await waitFor(() => {
      expect(screen.queryByText("Khóa đã được tạo ở lần bấm trước, không hiện lại được.")).toBeNull();
    });
  });

  it("kiểm form: tên rỗng, quá 80 ký tự, không quyền nào, IP sai dạng → nút Tạo khóa khóa, câu dưới ô", async () => {
    const d = await openCreate();
    const submit = within(d).getByRole<HTMLButtonElement>("button", { name: "Tạo khóa" });
    expect(submit.disabled).toBe(true);
    fireEvent.change(within(d).getByRole("textbox", { name: "Tên khóa" }), { target: { value: "x".repeat(81) } });
    expect(within(d).getByText("Tên 1–80 ký tự")).toBeTruthy();
    fireEvent.change(within(d).getByRole("textbox", { name: "Tên khóa" }), { target: { value: "Khóa" } });
    expect(submit.disabled).toBe(false);
    for (const b of within(d).getAllByRole<HTMLInputElement>("checkbox")) if (b.checked) fireEvent.click(b);
    expect(within(d).getByText("Chọn ít nhất một quyền")).toBeTruthy();
    expect(submit.disabled).toBe(true);
    fireEvent.click(within(d).getByRole("checkbox", { name: /Đọc danh mục/ }));
    fireEvent.change(within(d).getByRole("textbox", { name: "IP được gọi" }), { target: { value: "13.140.64.56" } });
    expect(within(d).getByText(/Dòng 1 chưa đúng dạng/)).toBeTruthy();
    expect(submit.disabled).toBe(true);
    expect(fake.writes).toHaveLength(0);
  });

  it("máy chủ 422 permission_not_allowed_for_key → câu của mã lỗi trong hộp", async () => {
    fake.replies.push({ status: 422, body: { error: "permission_not_allowed_for_key", request_id: "t" } });
    const d = await openCreate();
    fireEvent.change(within(d).getByRole("textbox", { name: "Tên khóa" }), { target: { value: "Khóa" } });
    fireEvent.click(within(d).getByRole("button", { name: "Tạo khóa" }));
    expect(await within(d).findByText("Khóa cho website chỉ nhận các quyền web.*")).toBeTruthy();
  });
});

describe("thu hồi (trạng thái D)", () => {
  it("cần xác nhận; Đóng thì không gửi; xác nhận gửi version đã đọc, tải lại danh sách", async () => {
    renderAt();
    await table();
    fireEvent.click(within(rowOf("Website ELIMAZ — bản thử (staging)")).getByRole("button", { name: "Thu hồi" }));
    let d = await dialog("Thu hồi khóa này?");
    expect(d.textContent).toContain("ek_aaaabbbb0002_…");
    expect(d.textContent).toContain("Không mở lại được");
    fireEvent.click(within(d).getAllByRole("button", { name: "Đóng" }).at(-1) ?? d);
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(fake.writes).toHaveLength(0);
    fireEvent.click(within(rowOf("Website ELIMAZ — bản thử (staging)")).getByRole("button", { name: "Thu hồi" }));
    d = await dialog("Thu hồi khóa này?");
    fireEvent.click(within(d).getByRole("button", { name: "Thu hồi khóa" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({ path: `/api/v1/identity/service-keys/${K2}/revoke`, body: { version: 2 } });
    expect(await screen.findByText("Đã thu hồi 24/11")).toBeTruthy();
  });

  it("409 version_conflict → hộp khóa vừa được người khác đổi, tải lại danh sách", async () => {
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    renderAt();
    await table();
    const before = fake.listCalls;
    fireEvent.click(within(rowOf(LONG_NAME)).getByRole("button", { name: "Thu hồi" }));
    const d = await dialog("Thu hồi khóa này?");
    fireEvent.click(within(d).getByRole("button", { name: "Thu hồi khóa" }));
    expect(await screen.findByText("Khóa vừa được người khác đổi.")).toBeTruthy();
    await waitFor(() => {
      expect(fake.listCalls).toBeGreaterThan(before);
    });
    expect(fake.writes[0]?.path).toBe(`/api/v1/identity/service-keys/${K3}/revoke`);
  });
});
