// @vitest-environment jsdom
// Kết nối bằng mã system user (kế hoạch M4 Task 178 Step 1; mockup ket-noi-page.html trạng thái C; HV35): sau khi gửi,
// ô rỗng; mã không nằm trong localStorage, sessionStorage, bộ đệm truy vấn của ứng dụng.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderScreens } from "../../app/screenTestkit";
import { newFake, stubDialog, stubKenh, type KenhFake } from "./kenhTestkit";
import { screens } from "./route";

const SECRET = "EAAG-system-user-secret-0123456789";
let fake: KenhFake;

beforeEach(() => {
  stubDialog();
  fake = stubKenh(newFake());
  localStorage.clear();
  sessionStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function storageText(s: Storage): string {
  let out = "";
  for (let i = 0; i < s.length; i++) {
    const k = s.key(i) ?? "";
    out += `${k}=${s.getItem(k) ?? ""};`;
  }
  return out;
}

async function openForm() {
  const view = renderScreens(screens, "/settings/channels");
  fireEvent.click(await screen.findByRole("button", { name: "Kết nối bằng mã system user" }));
  const dlg = screen.getByRole("dialog", { name: "Kết nối bằng mã system user" });
  const token = within(dlg).getByLabelText<HTMLInputElement>("Mã truy cập system user *");
  return { ...view, dlg, token };
}

it("ô mã là ô mật khẩu, không tự điền", async () => {
  const { token } = await openForm();
  expect(token.type).toBe("password");
  expect(token.getAttribute("autocomplete")).toBe("off");
});

it("gửi: thân có systemUserToken; ô rỗng ngay khi gửi (cả lúc đang chờ) và mã không nằm ở kho nào của ứng dụng", async () => {
  let release: () => void = () => undefined;
  fake.hold = new Promise((r) => {
    release = r;
  });
  const { dlg, token, qc } = await openForm();
  fireEvent.change(token, { target: { value: SECRET } });
  fireEvent.change(within(dlg).getByLabelText("Page ID *"), { target: { value: "256660660862565" } });
  fireEvent.click(within(dlg).getByRole("button", { name: "Kết nối" }));
  await waitFor(() => {
    expect(fake.writes).toHaveLength(1);
  });
  expect(token.value).toBe("");
  release();
  await waitFor(() => {
    expect(screen.queryByRole("dialog")).toBeNull();
  });
  const w = fake.writes[0];
  expect(w?.path).toBe("/api/v1/channels/facebook/pages");
  expect(w?.body).toEqual({ pageId: "256660660862565", systemUserToken: SECRET });
  expect(w?.key).not.toBeNull();
  expect(storageText(localStorage)).not.toContain(SECRET);
  expect(storageText(sessionStorage)).not.toContain(SECRET);
  const cache = JSON.stringify(
    qc
      .getQueryCache()
      .getAll()
      .map((q) => q.state.data),
  );
  expect(cache).not.toContain(SECRET);
  expect(
    JSON.stringify(
      qc
        .getMutationCache()
        .getAll()
        .map((m) => m.state),
    ),
  ).not.toContain(SECRET);
  expect(document.body.innerHTML).not.toContain(SECRET);
});

it("lỗi (422 page_not_accessible): ô mã vẫn rỗng, báo lỗi, phải nhập lại mã", async () => {
  fake.replies.push({ status: 422, body: { error: "page_not_accessible", request_id: "r" } });
  const { dlg, token } = await openForm();
  fireEvent.change(token, { target: { value: SECRET } });
  fireEvent.change(within(dlg).getByLabelText("Page ID *"), { target: { value: "256660660862565" } });
  fireEvent.click(within(dlg).getByRole("button", { name: "Kết nối" }));
  expect(await within(dlg).findByText("Chưa đủ quyền trên Page ID 256660660862565.")).toBeTruthy();
  expect(token.value).toBe("");
  expect(document.body.innerHTML).not.toContain(SECRET);
});

it("thiếu mã hay Page ID sai → báo tại ô, không gửi", async () => {
  const { dlg } = await openForm();
  fireEvent.change(within(dlg).getByLabelText("Page ID *"), { target: { value: "abc" } });
  fireEvent.click(within(dlg).getByRole("button", { name: "Kết nối" }));
  expect(within(dlg).getByText("Nhập mã truy cập system user.")).toBeTruthy();
  expect(within(dlg).getByText("Page ID chỉ gồm chữ số.")).toBeTruthy();
  expect(fake.writes).toHaveLength(0);
});
