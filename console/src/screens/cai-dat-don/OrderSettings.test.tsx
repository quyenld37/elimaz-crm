// @vitest-environment jsdom
// Màn Cài đặt đơn (mockup docs/mockups/m2/cai-dat-don.html trạng thái A–C). Kế hoạch M2 Task M2-30 Step 1: cài đặt
// ngoài giới hạn → báo theo tên trường từ setting_out_of_range.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Grant, OrderSettings, TrafficSource } from "../../api";
import { bodyOf, renderScreens, type Write } from "../../app/screenTestkit";
import { screens } from "./route";
const S1 = "0195f3b2-0000-7000-8000-0000000a0001";
const S2 = "0195f3b2-0000-7000-8000-0000000a0002";
const S3 = "0195f3b2-0000-7000-8000-0000000a0003";
const S4 = "0195f3b2-0000-7000-8000-0000000a0004";

let settings: OrderSettings;
let sources: TrafficSource[];
let writes: Write[];
let replies: { status: number; body: unknown }[];
let grants: Grant[];

beforeEach(() => {
  settings = {
    draftTtlMinutes: 1440,
    duplicateWindowMinutes: 1440,
    discountApprovalThresholdBp: 1000,
    returnWarningDays: 7,
    version: 5,
  };
  sources = [
    { id: S1, name: "Live tối", active: true, position: 0, version: 1 },
    { id: S2, name: "Messenger", active: true, position: 1, version: 2 },
    { id: S3, key: "web", name: "Website", active: true, position: 2, version: 1 },
    { id: S4, name: "Zalo", active: false, position: 3, version: 4 },
  ];
  writes = [];
  replies = [];
  grants = ["order.read", "order.settings.manage"].map((permission) => ({
    permission,
    scope: { type: "all", id: "" },
  }));
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const p = new URL(req.url).pathname;
      if (req.method === "GET") {
        if (p === "/api/v1/me") return Response.json({ userId: "me", email: "a@elimaz.vn", grants });
        if (p === "/api/v1/order-settings") return Response.json(settings);
        if (p === "/api/v1/traffic-sources") return Response.json({ items: sources });
        return Response.json({ error: "not_found", request_id: "t" }, { status: 404 });
      }
      writes.push({ method: req.method, path: p, key: req.headers.get("Idempotency-Key"), body: await bodyOf(req) });
      const q = replies.shift();
      if (q !== undefined) return Response.json(q.body, { status: q.status });
      if (p === "/api/v1/order-settings") return Response.json({ ...settings, version: settings.version + 1 });
      if (p === "/api/v1/traffic-sources") {
        return Response.json({ id: "new", name: "TikTok", active: true, position: 4, version: 1 }, { status: 201 });
      }
      const s = sources.find((x) => p.endsWith(x.id));
      return Response.json({ ...s, version: (s?.version ?? 0) + 1 });
    }),
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const renderPage = () => renderScreens(screens, "/cai-dat/don");

const input = (name: string) => screen.getByLabelText<HTMLInputElement>(name);
/** Gõ v vào ô el (ô tên theo nhãn: fill). */
function type(el: HTMLElement, v: string) {
  fireEvent.change(el, { target: { value: v } });
}
const fill = (name: string, v: string) => {
  type(input(name), v);
};

describe("Bốn cài đặt", () => {
  it("hiện giá trị hiện hành theo giờ, %, ngày và giới hạn cho phép", async () => {
    renderPage();
    expect(await screen.findByDisplayValue("7")).toBeTruthy();
    expect(input("Giữ đơn nháp (giờ)").value).toBe("24");
    expect(input("Khoảng tìm đơn trùng (giờ)").value).toBe("24");
    expect(input("Mức giảm tay cần duyệt (%)").value).toBe("10");
    expect(input("Cảnh báo đổi trả muộn (ngày)").value).toBe("7");
    expect(screen.getByText("10 phút (0,17 giờ) đến 7 ngày (168 giờ); nhập số lẻ được")).toBeTruthy();
    expect(screen.getByText("0 đến 365")).toBeTruthy();
  });

  it("lưu đổi đơn vị: giờ → phút, % → điểm cơ bản, kèm version", async () => {
    renderPage();
    await screen.findByDisplayValue("7");
    fill("Giữ đơn nháp (giờ)", "0,5");
    fill("Mức giảm tay cần duyệt (%)", "12,5");
    fireEvent.click(screen.getByRole("button", { name: "Lưu cài đặt" }));
    await waitFor(() => {
      expect(writes).toHaveLength(1);
    });
    expect(writes[0]?.key).toBeTruthy();
    expect(writes[0]?.body).toEqual({
      version: 5,
      draftTtlMinutes: 30,
      duplicateWindowMinutes: 1440,
      discountApprovalThresholdBp: 1250,
      returnWarningDays: 7,
    });
    expect(await screen.findByText("Đã lưu cài đặt.")).toBeTruthy();
  });

  it("ngoài giới hạn → 422 setting_out_of_range: báo dưới đúng ô theo tên trường", async () => {
    replies.push({ status: 422, body: { error: "setting_out_of_range", request_id: "t", field: "draftTtlMinutes" } });
    renderPage();
    await screen.findByDisplayValue("7");
    fill("Giữ đơn nháp (giờ)", "200");
    fireEvent.click(screen.getByRole("button", { name: "Lưu cài đặt" }));
    const err = await screen.findByText("Giữ đơn nháp chỉ từ 10 phút đến 7 ngày");
    expect(err.closest(".set")?.textContent).toContain("Giữ đơn nháp");
    expect(input("Giữ đơn nháp (giờ)").getAttribute("aria-invalid")).toBe("true");
    expect(input("Cảnh báo đổi trả muộn (ngày)").getAttribute("aria-invalid")).toBeNull();
  });

  it("người khác vừa lưu (409 version_conflict): báo, giữ giá trị đã nhập, có nút Tải lại", async () => {
    replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    renderPage();
    await screen.findByDisplayValue("7");
    fill("Cảnh báo đổi trả muộn (ngày)", "10");
    fireEvent.click(screen.getByRole("button", { name: "Lưu cài đặt" }));
    expect(await screen.findByText("Cài đặt đã thay đổi, tải lại.")).toBeTruthy();
    expect(screen.getByText(/Giá trị bạn nhập chưa được lưu/)).toBeTruthy();
    settings = { ...settings, returnWarningDays: 14, version: 6 };
    fireEvent.click(screen.getByRole("button", { name: "Tải lại" }));
    await waitFor(() => {
      expect(input("Cảnh báo đổi trả muộn (ngày)").value).toBe("14");
    });
  });

  it("ô không phải số: báo dưới ô, không gửi", async () => {
    renderPage();
    await screen.findByDisplayValue("7");
    fill("Cảnh báo đổi trả muộn (ngày)", "bảy");
    fireEvent.click(screen.getByRole("button", { name: "Lưu cài đặt" }));
    expect(screen.getByText("Nhập số ngày nguyên")).toBeTruthy();
    expect(writes).toHaveLength(0);
  });
});

describe("Nguồn đơn", () => {
  const item = (name: string) => {
    const el = screen.getAllByRole("listitem").find((li) => li.textContent.includes(name));
    if (el === undefined) throw new Error(`thiếu nguồn ${name}`);
    return el;
  };

  it("theo vị trí; nguồn hệ thống không tắt được; nguồn đã tắt chỉ có Bật lại", async () => {
    renderPage();
    await screen.findByText("Live tối");
    expect(screen.getAllByRole("listitem").map((li) => li.querySelector("b")?.textContent)).toEqual([
      "Live tối",
      "Messenger",
      "Website",
      "Zalo",
    ]);
    expect(within(item("Live tối")).getByRole("button", { name: "Đưa Live tối lên" }).hasAttribute("disabled")).toBe(
      true,
    );
    expect(within(item("Website")).queryByRole("button", { name: "Tắt" })).toBeNull();
    expect(within(item("Website")).getByText("Nguồn hệ thống")).toBeTruthy();
    expect(within(item("Zalo")).getByText(/không chọn được cho đơn mới/)).toBeTruthy();
    expect(within(item("Zalo")).queryByRole("button", { name: "Đổi tên" })).toBeNull();
  });

  it("thêm nguồn: gửi tên; tên trùng → báo theo tên", async () => {
    replies.push({ status: 409, body: { error: "already_exists", request_id: "t" } });
    renderPage();
    await screen.findByText("Live tối");
    type(screen.getByRole("textbox", { name: "Tên nguồn mới" }), "Messenger");
    fireEvent.click(screen.getByRole("button", { name: "Thêm nguồn" }));
    expect(await screen.findByText('Đã có nguồn tên "Messenger".')).toBeTruthy();
    type(screen.getByRole("textbox", { name: "Tên nguồn mới" }), " TikTok ");
    fireEvent.click(screen.getByRole("button", { name: "Thêm nguồn" }));
    await waitFor(() => {
      expect(writes.at(-1)?.body).toEqual({ name: "TikTok" });
    });
  });

  it("đổi tên tại chỗ: Lưu gửi tên mới kèm version", async () => {
    renderPage();
    await screen.findByText("Messenger");
    fireEvent.click(within(item("Messenger")).getByRole("button", { name: "Đổi tên" }));
    type(screen.getByRole("textbox", { name: "Tên nguồn" }), "Messenger shop");
    fireEvent.click(screen.getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(writes[0]).toMatchObject({ method: "PATCH", path: `/api/v1/traffic-sources/${S2}` });
    });
    expect(writes[0]?.body).toEqual({ version: 2, name: "Messenger shop" });
  });

  it("đưa xuống: đổi vị trí hai nguồn, mỗi nguồn một PATCH có version", async () => {
    renderPage();
    await screen.findByText("Live tối");
    fireEvent.click(within(item("Live tối")).getByRole("button", { name: "Đưa Live tối xuống" }));
    await waitFor(() => {
      expect(writes).toHaveLength(2);
    });
    expect(writes.map((w) => [w.path, w.body])).toEqual([
      [`/api/v1/traffic-sources/${S2}`, { version: 2, position: 0 }],
      [`/api/v1/traffic-sources/${S1}`, { version: 1, position: 1 }],
    ]);
    expect(new Set(writes.map((w) => w.key)).size).toBe(2);
  });

  it("tắt và bật lại gửi active kèm version", async () => {
    renderPage();
    await screen.findByText("Live tối");
    fireEvent.click(within(item("Live tối")).getByRole("button", { name: "Tắt" }));
    await waitFor(() => {
      expect(writes[0]?.body).toEqual({ version: 1, active: false });
    });
    fireEvent.click(within(item("Zalo")).getByRole("button", { name: "Bật lại" }));
    await waitFor(() => {
      expect(writes[1]?.body).toEqual({ version: 4, active: true });
    });
  });
});
