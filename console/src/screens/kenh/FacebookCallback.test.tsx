// @vitest-environment jsdom
// Quay về từ Facebook (kế hoạch M4 Task 178 Step 1; mockup ket-noi-page.html trạng thái B; HV35).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderScreens } from "../../app/screenTestkit";
import { newFake, stubDialog, stubKenh, type KenhFake } from "./kenhTestkit";
import { TEXT_NO_CODE } from "./FacebookCallback";
import { screens } from "./route";

const CALLBACK = "/settings/channels/facebook/callback";
const SESSION = "0195f3b2-0000-7000-8000-0000000a0099";
let fake: KenhFake;
let searchAtExchange: string | null;

beforeEach(() => {
  stubDialog();
  fake = stubKenh(newFake());
  searchAtExchange = null;
  const inner = globalThis.fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn((req: Request) => {
      if (new URL(req.url).pathname === "/api/v1/channels/facebook/oauth/exchange")
        searchAtExchange = window.location.search;
      return inner(req);
    }),
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  window.history.replaceState(null, "", "/");
});

const PAGES = {
  sessionId: SESSION,
  pages: [
    { pageId: "256660660862565", name: "ELIMAZ", canMessage: true, canModerate: true },
    { pageId: "100000000000003", name: "ELIMAZ Outlet", canMessage: true, canModerate: false },
    { pageId: "100000000000004", name: "Shop Thử Nghiệm", canMessage: true, canModerate: true },
  ],
};

function open(search: string) {
  window.history.pushState(null, "", `${CALLBACK}${search}`);
  return renderScreens(screens, `${CALLBACK}${search}`, [{ path: "/settings/channels", element: <p>Trang kênh</p> }]);
}

it("?code=abc&state=xyz → exchangeFacebookOAuth({code, state}); trước lời gọi đó location.search đã rỗng", async () => {
  fake.replies.push({ status: 200, body: PAGES });
  open("?code=abc&state=xyz");
  await screen.findByRole("dialog", { name: "Chọn Page để kết nối" });
  const ex = fake.writes.filter((w) => w.path === "/api/v1/channels/facebook/oauth/exchange");
  expect(ex).toHaveLength(1);
  expect(ex[0]?.body).toEqual({ code: "abc", state: "xyz" });
  expect(ex[0]?.key).not.toBeNull();
  expect(searchAtExchange).toBe("");
  expect(window.location.search).toBe("");
  expect(window.location.pathname).toBe(CALLBACK);
});

it("hộp chọn Page: Page thiếu quyền mờ kèm lý do; chọn → connectFacebookPage với sessionId; xong về Kênh kết nối", async () => {
  fake.replies.push({ status: 200, body: PAGES });
  const { router } = open("?code=abc&state=xyz");
  const dlg = await screen.findByRole("dialog", { name: "Chọn Page để kết nối" });
  expect(within(dlg).getByText(/Facebook cho CRM thấy 3 Page/)).toBeTruthy();
  const outlet = within(dlg).getByRole("radio", { name: /ELIMAZ Outlet · thiếu quyền kiểm duyệt comment/ });
  expect(outlet).toHaveProperty("disabled", true);
  expect(within(dlg).getByText(/Page mới kết nối ở chế độ chỉ xem/)).toBeTruthy();
  fireEvent.click(within(dlg).getByRole("radio", { name: /Shop Thử Nghiệm/ }));
  fireEvent.click(within(dlg).getByRole("button", { name: "Kết nối Shop Thử Nghiệm" }));
  await waitFor(() => {
    expect(router.state.location.pathname).toBe("/settings/channels");
  });
  const c = fake.writes.find((w) => w.path === "/api/v1/channels/facebook/pages");
  expect(c?.body).toEqual({ pageId: "100000000000004", sessionId: SESSION });
});

it.each([
  [409, "page_connected_elsewhere", 'Page "ELIMAZ" đang nối với một CRM khác.', /Gỡ ở CRM kia trước rồi thử lại/],
  [422, "page_not_accessible", 'Chưa đủ quyền trên Page "ELIMAZ".', /cần quyền nhắn tin và kiểm duyệt comment/],
])("kết nối bị từ chối %i %s → câu theo mockup kèm tên Page, ở lại hộp chọn", async (status, error, title, detail) => {
  fake.replies.push({ status: 200, body: PAGES }, { status, body: { error, request_id: "r" } });
  open("?code=abc&state=xyz");
  const dlg = await screen.findByRole("dialog", { name: "Chọn Page để kết nối" });
  fireEvent.click(within(dlg).getByRole("button", { name: "Kết nối ELIMAZ" }));
  expect(await within(dlg).findByText(title)).toBeTruthy();
  expect(within(dlg).getByText(detail)).toBeTruthy();
});

it("phiên hết hạn (400 oauth_state_invalid) → hộp lỗi, không có hộp chọn Page", async () => {
  fake.replies.push({ status: 400, body: { error: "oauth_state_invalid", request_id: "r" } });
  open("?code=abc&state=xyz");
  expect(await screen.findByText("Phiên kết nối đã hết hạn hoặc đã dùng.")).toBeTruthy();
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(window.location.search).toBe("");
});

it("Facebook không trả code (người dùng hủy) → báo, không gọi API; tham số lỗi cũng bị xóa khỏi thanh địa chỉ", async () => {
  open("?error=access_denied&state=xyz");
  expect(await screen.findByText(TEXT_NO_CODE)).toBeTruthy();
  expect(fake.writes).toHaveLength(0);
  expect(window.location.search).toBe("");
});
