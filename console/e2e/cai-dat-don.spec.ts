// Màn Cài đặt đơn (kế hoạch M2 Task M2-30; mockup docs/mockups/m2/cai-dat-don.html) ở 1512 × 767 và 390 × 844, API
// giả. Hành vi (đổi đơn vị, lỗi theo tên trường, nguồn đơn) kiểm ở test vitest của src/screens/cai-dat-don; ở đây kiểm
// bố cục không tràn và chụp test-results/cai-dat-don-*.png để so với docs/mockups/m2/png/cai-dat-don-*.png.
import type { OrderSettings, TrafficSource } from "../src/api";
import { OWNER_GRANTS, expect, forEachViewport, json, noHorizontalScroll, test } from "./fixtures";

const id = (n: number) => `0195f3b2-0000-7000-8000-0000000e${String(n).padStart(4, "0")}`;
const GRANTS = [
  ...OWNER_GRANTS,
  ...["order.read", "order.settings.manage"].map((permission) => ({
    permission,
    scope: { type: "all" as const, id: "" },
  })),
];
const SETTINGS: OrderSettings = {
  draftTtlMinutes: 1440,
  duplicateWindowMinutes: 1440,
  discountApprovalThresholdBp: 1000,
  returnWarningDays: 7,
  version: 5,
};
const SOURCES: TrafficSource[] = [
  { id: id(1), name: "Live tối", active: true, position: 0, version: 1 },
  { id: id(2), name: "Messenger", active: true, position: 1, version: 1 },
  { id: id(3), name: "Live trưa thứ bảy và chủ nhật cho khách sỉ miền Bắc", active: true, position: 2, version: 1 },
  { id: id(4), key: "web", name: "Website", active: true, position: 3, version: 1 },
  { id: id(5), name: "Zalo", active: false, position: 4, version: 1 },
];

forEachViewport((vp) => {
  test("bốn cài đặt, nguồn đơn, lỗi ngoài giới hạn: không tràn ngang", async ({ page, api }) => {
    await api.live({ grants: GRANTS });
    await api.on("**/api/v1/order-settings", (r) =>
      r.request().method() === "PATCH"
        ? json(r, 422, { error: "setting_out_of_range", request_id: "e2e", field: "draftTtlMinutes" })
        : json(r, 200, SETTINGS),
    );
    await api.on("**/api/v1/traffic-sources", (r) => json(r, 200, { items: SOURCES }));
    await page.goto("/cai-dat/don");
    await expect(page.getByLabel("Giữ đơn nháp (giờ)")).toHaveValue("24");
    await expect(page.getByText("Nguồn hệ thống")).toBeVisible();
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/cai-dat-don-${String(vp.width)}.png`, fullPage: true });
    await page.getByLabel("Giữ đơn nháp (giờ)").fill("200");
    await page.getByRole("button", { name: "Lưu cài đặt" }).click();
    await expect(page.getByText("Giữ đơn nháp chỉ từ 10 phút đến 7 ngày")).toBeVisible();
    await noHorizontalScroll(page);
    await page.screenshot({ path: `test-results/cai-dat-don-loi-${String(vp.width)}.png`, fullPage: true });
  });
});
