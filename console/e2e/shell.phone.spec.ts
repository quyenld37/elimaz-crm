// Menu trượt trên điện thoại (WebKit, đuôi .phone.spec.ts; kế hoạch M1 Task 48): ☰ mở menu từ trái, nút chạm cao
// 44 px, Escape và nền mờ đóng menu. Thao tác bằng chạm (tap) như trên điện thoại thật: WebKit giả lập điện thoại
// (hasTouch) không nhận click chuột của Playwright vào nút trong thanh trên dính (sticky).
import { PHONE, expect, test } from "./fixtures";

test.use({ viewport: PHONE, hasTouch: true });

test("☰ mở menu; nền mờ đóng; nút chạm 44 px", async ({ page, api }) => {
  await api.live();
  await page.goto("/");
  const open = page.getByRole("button", { name: "Mở menu" });
  const box = await open.boundingBox();
  expect(box?.height).toBeGreaterThanOrEqual(44);
  expect(box?.width).toBeGreaterThanOrEqual(44);
  await open.tap();
  // Menu trượt (chưa có màn nào nên chỉ có chữ ELIMAZ và ô người dùng).
  const nav = page.locator("aside.side");
  await expect(nav).toBeVisible();
  await expect(nav.getByText("lan@elimaz.vn")).toBeVisible();
  expect((await nav.boundingBox())?.width).toBeCloseTo(300, 0);
  await expect(open).toHaveAttribute("aria-expanded", "true");
  await page.getByRole("button", { name: "Đóng menu" }).tap({ position: { x: 370, y: 400 } });
  await expect(nav).toBeHidden();
  await open.tap();
  await expect(nav).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(nav).toBeHidden();
  await expect(open).toBeFocused();
});
