// @vitest-environment jsdom
// Thẻ "Hãng trên website" ở cuối màn Kết nối vận chuyển (Task 208, M5-25; mockup docs/mockups/m5/web-carriers.html
// trạng thái A, B, C; HV09–HV12). Mục menu "Hãng trên web" (nhóm Website) mở chính màn đó và cuộn tới thẻ.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CarrierAccount, WebCarrierStat } from "../../api";
import { renderScreens } from "../../app/screenTestkit";
import {
  LONG_NAME,
  MANAGER,
  accId,
  account,
  grantsOf,
  installFake,
  stubDialog,
  type Fake,
} from "../carriers/carriersTestkit";
import { screens as carrierScreens } from "../carriers/route";
import { screens } from "./route";

const VTP = "Viettel Post — Kho chính";
const BEST = "Best Express — Kho chính";
const SPX = "SPX — Kho chính";
const BEST_HN = "Best Express — Kho Hà Nội";

/** Mockup A: VTP 1, Best 2 (chọn sẵn) đang hiện; SPX ẩn; Best Hà Nội tạm ngừng; tên 60 ký tự cần cấp quyền lại. */
function mockupAccounts(): CarrierAccount[] {
  return [
    account(1, { name: VTP, showOnWeb: true, webSort: 1, version: 4 }),
    account(2, { carrierKey: "best", name: BEST, showOnWeb: true, webSort: 2, version: 6 }),
    account(3, { carrierKey: "spx", name: SPX }),
    account(4, { carrierKey: "best", name: BEST_HN, status: "paused" }),
    account(5, { carrierKey: "best", name: LONG_NAME, status: "auth_failed" }),
    account(6, { carrierKey: "spx", name: "SPX cũ", status: "revoked", showOnWeb: true }),
  ];
}

const stat = (a: CarrierAccount, shipments30d: number, preferred = false): WebCarrierStat => ({
  accountId: a.id,
  carrierKey: a.carrierKey,
  name: a.name,
  status: a.status,
  webSort: a.webSort,
  preferred,
  shipments30d,
});

let fake: Fake;
beforeEach(() => {
  stubDialog();
  // jsdom không có scrollIntoView (mục menu Hãng trên web cuộn tới thẻ).
  Element.prototype.scrollIntoView = vi.fn();
  fake = installFake();
  fake.accounts = mockupAccounts();
  const [vtp, best, spx] = fake.accounts;
  if (vtp === undefined || best === undefined || spx === undefined) throw new Error("thiếu tài khoản");
  fake.webSettings = {
    version: 5,
    preferredAccountId: best.id,
    carriers: [stat(vtp, 412), stat(best, 37, true)],
    hiddenCarriers: [stat(spx, 9)],
  };
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const open = () => renderScreens([...carrierScreens, ...screens], "/website/hang-tren-web");
const card = async () => screen.findByRole("region", { name: "Hãng trên website" });
const item = (name: string) => {
  const el = [...document.querySelectorAll<HTMLElement>(".olist .oitem")].find(
    (x) => x.querySelector("b")?.textContent === name,
  );
  if (el === undefined) throw new Error(`không có dòng ${name}`);
  return el;
};
/** Chờ danh sách hãng của thẻ (ô bật của VTP) đã vẽ. */
const listed = (c: HTMLElement) => within(c).findByRole("checkbox", { name: `Hiện trên website: ${VTP}` });
const toggle = (name: string) => screen.getByRole<HTMLInputElement>("checkbox", { name: `Hiện trên website: ${name}` });
const webReads = () =>
  vi.mocked(fetch).mock.calls.filter(([r]) => r instanceof Request && r.url.endsWith("/api/v1/web-carrier-settings"))
    .length;

/** Giữ mọi lệnh ghi của API giả tới khi gọi hàm trả về (xem trạng thái "đang lưu"). */
function holdWrites(): () => void {
  let release: () => void = () => undefined;
  fake.hold = new Promise((r) => {
    release = r;
  });
  return () => {
    release();
  };
}

/** Đang giữ một lệnh ghi: bấm ô VTP không gửi thêm; thả ra thì ô mở lại. */
async function clickLockedThenRelease(release: () => void) {
  fireEvent.click(toggle(VTP));
  expect(fake.writes).toHaveLength(1);
  release();
  await waitFor(() => {
    expect(toggle(VTP).disabled).toBe(false);
  });
}

/** Mở thẻ, chờ danh sách, chọn sẵn VTP (chưa lưu). */
async function pickVtp() {
  const view = open();
  const c = await card();
  await listed(c);
  fireEvent.click(within(c).getByRole("radio", { name: VTP }));
  return { ...view, c };
}

/** Bấm Lưu của form chọn sẵn/phí và chờ lệnh ghi đầu tiên tới API giả. */
async function saveOnce(c: HTMLElement) {
  fireEvent.click(within(c).getByRole("button", { name: "Lưu" }));
  await waitFor(() => {
    expect(fake.writes).toHaveLength(1);
  });
}

describe("trạng thái A: danh sách, thứ tự, số vận đơn, chọn sẵn", () => {
  it("đang hiện theo thứ tự (số vận đơn 30 ngày, chọn sẵn); ẩn; không hoạt động không bật được; bỏ thu hồi", async () => {
    open();
    const c = await card();
    await listed(c);
    const names = within(c)
      .getAllByRole("checkbox")
      .map((x) => x.getAttribute("aria-label"));
    expect(names).toEqual([
      `Hiện trên website: ${VTP}`,
      `Hiện trên website: ${BEST}`,
      `Hiện trên website: ${SPX}`,
      `Hiện trên website: ${BEST_HN}`,
      `Hiện trên website: ${LONG_NAME}`,
      null, // ô "Dùng phí dự phòng…" lấy tên từ nhãn bọc ngoài
    ]);
    expect(within(c).getByText("2 hãng đang hiện")).toBeTruthy();
    expect(item(VTP).textContent).toContain("Viettel Post · Đang hoạt động · 412 vận đơn trong 30 ngày");
    expect(item(VTP).querySelector(".ord")?.textContent).toBe("1");
    expect(item(BEST).textContent).toContain("37 vận đơn trong 30 ngày");
    expect(item(BEST).textContent).toContain("Chọn sẵn");
    expect(toggle(VTP).checked).toBe(true);
    expect(toggle(SPX).checked).toBe(false);
    expect(toggle(SPX).disabled).toBe(false);
    expect(item(SPX).querySelector(".ord")?.textContent).toBe("–");
    expect(toggle(BEST_HN).disabled).toBe(true);
    expect(item(BEST_HN).textContent).toContain("Tạm ngừng: bật lại tài khoản");
    expect(toggle(LONG_NAME).disabled).toBe(true);
    expect(item(LONG_NAME).textContent).toContain("Cần cấp quyền lại");
    expect(within(c).queryByText("SPX cũ")).toBeNull();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: `Đưa ${VTP} lên` }).disabled).toBe(true);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: `Đưa ${BEST} xuống` }).disabled).toBe(true);
    const radios = within(c).getAllByRole<HTMLInputElement>("radio");
    expect(radios.map((r) => r.closest("label")?.textContent)).toEqual([
      "Không chọn sẵn (website chọn hãng rẻ nhất)",
      VTP,
      BEST,
    ]);
    expect(radios[2]?.checked).toBe(true);
    expect(screen.getByRole<HTMLInputElement>("textbox", { name: "Phí dự phòng" }).disabled).toBe(true);
    expect(within(c).getByText("Chưa có thay đổi.")).toBeTruthy();
  });

  it("mục menu Hãng trên web cuộn tới thẻ; mở Kết nối vận chuyển thì không cuộn", async () => {
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    const first = open();
    await card();
    await waitFor(() => {
      expect(scroll).toHaveBeenCalledTimes(1);
    });
    first.unmount();
    scroll.mockClear();
    renderScreens(carrierScreens, "/settings/carriers");
    await card();
    expect(scroll).not.toHaveBeenCalled();
  });

  it("nhắc việc trước khi website dùng báo phí của CRM: thiếu phí dự phòng → báo còn thiếu", async () => {
    open();
    const box = await screen.findByRole("note", { name: "Trước khi website dùng báo phí của CRM" });
    expect(box.textContent).toContain("bật “Hiện trên website” cho ít nhất một hãng");
    expect(box.textContent).toContain("nhập phí dự phòng");
    expect(box.textContent).toContain("Còn thiếu: phí dự phòng");
  });

  it("đủ hai việc → báo đã đủ", async () => {
    fake.webSettings = { ...fake.webSettings, fallbackFee: 30000 };
    open();
    const box = await screen.findByRole("note", { name: "Trước khi website dùng báo phí của CRM" });
    await waitFor(() => {
      expect(box.textContent).toContain("Đã đủ");
    });
  });
});

describe("bật, ẩn, đổi thứ tự (lưu ngay từng tài khoản)", () => {
  it("bật SPX → PATCH showOnWeb, webSort cuối, version đã đọc, có khóa chống trùng", async () => {
    open();
    await card();
    fireEvent.click(await screen.findByRole("checkbox", { name: `Hiện trên website: ${SPX}` }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({
      method: "PATCH",
      path: `/api/v1/carrier-accounts/${accId(3)}`,
      body: { version: 3, showOnWeb: true, webSort: 3 },
    });
    expect(fake.writes[0]?.key).toMatch(/^[A-Za-z0-9_-]{1,128}$/);
  });

  it("ẩn hãng đang chọn sẵn → PATCH showOnWeb false; báo đã bỏ chọn sẵn (máy chủ tự bỏ)", async () => {
    open();
    await card();
    fireEvent.click(await screen.findByRole("checkbox", { name: `Hiện trên website: ${BEST}` }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]?.body).toEqual({ version: 6, showOnWeb: false });
    const info = await screen.findByText(/đã bỏ chọn sẵn/);
    expect(info.closest(".box")?.textContent).toContain(`Đã ẩn ${BEST}`);
  });

  it("ẩn hãng đang hoạt động cuối cùng → không gửi, ô giữ tích, câu phải hiện ít nhất 1 hãng", async () => {
    fake.accounts = mockupAccounts().map((a) => (a.name === BEST ? { ...a, showOnWeb: false } : a));
    open();
    await card();
    const box = await screen.findByRole("checkbox", { name: `Hiện trên website: ${VTP}` });
    fireEvent.click(box);
    expect(await screen.findByText("Phải hiện ít nhất 1 hãng")).toBeTruthy();
    expect(toggle(VTP).checked).toBe(true);
    expect(fake.writes).toHaveLength(0);
  });

  it("máy chủ từ chối 422 last_web_carrier (người khác vừa ẩn hãng kia) → câu của mã lỗi", async () => {
    fake.replies.push({ status: 422, body: { error: "last_web_carrier", request_id: "t" } });
    open();
    await card();
    fireEvent.click(await screen.findByRole("checkbox", { name: `Hiện trên website: ${VTP}` }));
    expect(await screen.findByText(/Phải còn ít nhất một hãng hiện trên website/)).toBeTruthy();
  });

  it("↓ ở VTP → một lệnh PUT thứ tự cả danh sách (Best, VTP) kèm version cài đặt, có khóa chống trùng", async () => {
    open();
    await card();
    fireEvent.click(await screen.findByRole("button", { name: `Đưa ${VTP} xuống` }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({
      method: "PUT",
      path: "/api/v1/web-carrier-settings/order",
      body: { version: 5, accountIds: [accId(2), accId(1)] },
    });
    expect(fake.writes[0]?.key).toBeTruthy();
  });

  it("hãng chưa hiện cũng có số vận đơn 30 ngày (Task 488)", async () => {
    open();
    await listed(await card());
    expect(item(SPX).textContent).toContain("9 vận đơn trong 30 ngày");
  });

  it("đổi thứ tự 409 → hộp người khác vừa đổi nêu người, giờ lưu của bản tải lại", async () => {
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    open();
    await card();
    fake.webSettings = {
      ...fake.webSettings,
      version: 6,
      updatedByName: "Trần Minh Thư",
      updatedAt: "2026-11-21T04:20:00Z",
    };
    fireEvent.click(await screen.findByRole("button", { name: `Đưa ${VTP} xuống` }));
    expect(await screen.findByText("Người khác vừa đổi phần này.")).toBeTruthy();
    expect(await screen.findByText(/Đã tải lại \(Trần Minh Thư, 11:20 21\/11\)\. Xem lại rồi chọn lại\./)).toBeTruthy();
  });

  it("đổi thứ tự xong rồi lưu bản nháp chọn sẵn (đọc ở version 5) → gửi version mới của chính mình, không tự 409", async () => {
    const { c } = await pickVtp();
    fake.replies.push({ status: 200, body: { ...fake.webSettings, version: 6 } });
    fireEvent.click(screen.getByRole("button", { name: `Đưa ${VTP} xuống` }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    await waitFor(() => {
      expect(within(c).getByRole<HTMLButtonElement>("button", { name: "Lưu" }).disabled).toBe(false);
    });
    fireEvent.click(within(c).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(2);
    });
    expect(fake.writes[1]?.body).toEqual({ version: 6, preferredAccountId: accId(1) });
  });

  it("đang lưu → mọi ô bật/tắt, nút thứ tự và nút Lưu khóa; dòng đang lưu ghi Đang lưu…", async () => {
    const release = holdWrites();
    open();
    await card();
    fireEvent.click(await screen.findByRole("checkbox", { name: `Hiện trên website: ${SPX}` }));
    await waitFor(() => {
      expect(item(SPX).textContent).toContain("Đang lưu…");
    });
    expect(toggle(VTP).disabled).toBe(true);
    expect(toggle(SPX).disabled).toBe(true);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: `Đưa ${VTP} xuống` }).disabled).toBe(true);
    await clickLockedThenRelease(release);
  });

  it("409 version_conflict → hộp người khác vừa đổi, tải lại", async () => {
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    open();
    await card();
    const before = webReads();
    fireEvent.click(await screen.findByRole("checkbox", { name: `Hiện trên website: ${SPX}` }));
    expect(await screen.findByText("Người khác vừa đổi phần này.")).toBeTruthy();
    await waitFor(() => {
      expect(webReads()).toBeGreaterThan(before);
    });
  });
});

describe("hãng chọn sẵn, phí dự phòng (nút Lưu riêng)", () => {
  it("chọn rẻ nhất, bật phí 30.000 → PUT đúng thân; Bỏ thay đổi trả về như cũ", async () => {
    open();
    const c = await card();
    await listed(c);
    fireEvent.click(within(c).getByRole("radio", { name: "Không chọn sẵn (website chọn hãng rẻ nhất)" }));
    fireEvent.click(within(c).getByRole("checkbox", { name: "Dùng phí dự phòng khi không hãng nào báo được phí" }));
    fireEvent.change(within(c).getByRole("textbox", { name: "Phí dự phòng" }), { target: { value: "30.000" } });
    expect(within(c).getByText("Có thay đổi chưa lưu.")).toBeTruthy();
    fireEvent.click(within(c).getByRole("button", { name: "Bỏ thay đổi" }));
    expect(within(c).getByText("Chưa có thay đổi.")).toBeTruthy();
    expect(within(c).getByRole<HTMLInputElement>("radio", { name: BEST }).checked).toBe(true);

    fireEvent.click(within(c).getByRole("radio", { name: "Không chọn sẵn (website chọn hãng rẻ nhất)" }));
    fireEvent.click(within(c).getByRole("checkbox", { name: "Dùng phí dự phòng khi không hãng nào báo được phí" }));
    fireEvent.change(within(c).getByRole("textbox", { name: "Phí dự phòng" }), { target: { value: "30.000" } });
    await saveOnce(c);
    expect(fake.writes[0]).toMatchObject({
      method: "PUT",
      path: "/api/v1/web-carrier-settings",
      body: { version: 5, fallbackFee: 30000 },
    });
  });

  it("bản nháp giữ version đã đọc: cài đặt tải lại ngầm (version 6) rồi Lưu → gửi version 5 (máy chủ trả 409)", async () => {
    const { qc, c } = await pickVtp();
    const before = webReads();
    fake.webSettings = { ...fake.webSettings, version: 6 };
    await qc.invalidateQueries();
    await waitFor(() => {
      expect(webReads()).toBeGreaterThan(before);
    });
    expect(within(c).getByRole<HTMLInputElement>("radio", { name: VTP }).checked).toBe(true);
    await saveOnce(c);
    expect(fake.writes[0]?.body).toEqual({ version: 5, preferredAccountId: accId(1) });
  });

  it("đang lưu chọn sẵn → ô bật/tắt, nút thứ tự, ô chọn khóa; không gửi lệnh ghi thứ hai", async () => {
    const release = holdWrites();
    const { c } = await pickVtp();
    fireEvent.click(within(c).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(toggle(VTP).disabled).toBe(true);
    });
    expect(screen.getByRole<HTMLButtonElement>("button", { name: `Đưa ${VTP} xuống` }).disabled).toBe(true);
    expect(within(c).getByRole<HTMLInputElement>("radio", { name: BEST }).disabled).toBe(true);
    await clickLockedThenRelease(release);
  });

  it("phí 600.000 → câu lỗi dưới ô, nút Lưu khóa", async () => {
    open();
    const c = await card();
    await listed(c);
    fireEvent.click(within(c).getByRole("checkbox", { name: "Dùng phí dự phòng khi không hãng nào báo được phí" }));
    fireEvent.change(within(c).getByRole("textbox", { name: "Phí dự phòng" }), { target: { value: "600.000" } });
    expect(within(c).getByText("Phí dự phòng từ 0 đến 500.000 ₫")).toBeTruthy();
    expect(within(c).getByRole<HTMLButtonElement>("button", { name: "Lưu" }).disabled).toBe(true);
  });

  it("409 khi lưu → hộp người khác vừa đổi, bỏ bản nháp, tải lại", async () => {
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    const { c } = await pickVtp();
    const before = webReads();
    fireEvent.click(within(c).getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText("Người khác vừa đổi phần này.")).toBeTruthy();
    await waitFor(() => {
      expect(webReads()).toBeGreaterThan(before);
    });
    expect(within(c).getByText("Chưa có thay đổi.")).toBeTruthy();
    expect(within(c).getByRole<HTMLInputElement>("radio", { name: BEST }).checked).toBe(true);
  });

  it("422 carrier_not_on_web → câu của mã lỗi", async () => {
    fake.replies.push({ status: 422, body: { error: "carrier_not_on_web", request_id: "t" } });
    const { c } = await pickVtp();
    fireEvent.click(within(c).getByRole("button", { name: "Lưu" }));
    expect(await screen.findByText(/Hãng này chưa bật hiện trên website/)).toBeTruthy();
  });
});

describe("trạng thái C: chỉ xem, không tài khoản hoạt động, lỗi tải", () => {
  it("không có quyền manage → không ô, không nút; câu chỉ xem", async () => {
    fake.grants = grantsOf("fulfillment.carrier_accounts.read");
    open();
    const c = await card();
    await within(c).findByText(VTP);
    expect(within(c).queryAllByRole("checkbox")).toHaveLength(0);
    expect(within(c).queryAllByRole("radio")).toHaveLength(0);
    expect(within(c).queryAllByRole("button")).toHaveLength(0);
    expect(within(c).getByText(/Bạn chỉ có quyền xem/)).toBeTruthy();
    expect(c.textContent).toContain(`Hãng chọn sẵn: ${BEST}`);
    expect(c.textContent).toContain("Phí dự phòng: tắt");
  });

  it("không tài khoản nào hoạt động, không hãng hiện → câu chưa có hãng để khách chọn", async () => {
    fake.accounts = [account(4, { name: BEST_HN, status: "paused" })];
    fake.webSettings = { version: 1, carriers: [], hiddenCarriers: [] };
    open();
    const c = await card();
    expect(await within(c).findByText("Chưa có tài khoản hãng nào hoạt động")).toBeTruthy();
  });

  it("lỗi đọc cài đặt → hộp lỗi, không vẽ form như rỗng", async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      if (input instanceof Request && input.url.endsWith("/api/v1/web-carrier-settings")) {
        return Promise.resolve(Response.json({ error: "internal", request_id: "t" }, { status: 500 }));
      }
      if (input instanceof Request && input.url.endsWith("/api/v1/me")) {
        return Promise.resolve(Response.json({ userId: "u1", email: "lan@elimaz.vn", grants: grantsOf(...MANAGER) }));
      }
      if (input instanceof Request && input.url.includes("/api/v1/carrier-accounts")) {
        return Promise.resolve(Response.json({ items: fake.accounts }));
      }
      return Promise.resolve(Response.json([]));
    });
    open();
    const c = await card();
    expect(await within(c).findByRole("alert")).toBeTruthy();
    expect(within(c).queryAllByRole("radio")).toHaveLength(0);
  });
});
