// Khung màn hình trên trình duyệt thật (kế hoạch M1 Task 48): tải ở 1512 × 767 và 390 × 844; API tắt → băng báo của
// M0 hiện và khung vẫn đứng. Chạy trên Chromium (mọi spec); API giả qua fixtures (xem e2e/fixtures.ts).
import { TEXT_EMPTY_HOME } from "../src/app/router";
import { TEXT_API_DOWN } from "../src/shell/Banner";
import { expect, forEachViewport, test } from "./fixtures";

forEachViewport((vp) => {
  test("khung tải: chữ ELIMAZ, không băng báo khi API sống, không tràn ngang", async ({ page, api }) => {
    await api.live();
    await page.goto("/");
    await expect(page.locator(".wordmark:visible")).toBeVisible();
    await expect(page.getByText(TEXT_API_DOWN)).toHaveCount(0);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(0);
    if (vp.width >= 1000) {
      // Cột trái 240 px (chưa có màn nào nên menu chưa có mục; ô người dùng ở cuối cột).
      const side = page.locator("aside.side");
      await expect(side).toBeVisible();
      expect((await side.boundingBox())?.width).toBeCloseTo(240, 0);
      await expect(side.getByText("lan@elimaz.vn")).toBeVisible();
      await expect(page.getByRole("button", { name: "Mở menu" })).toBeHidden();
    } else {
      const top = page.getByRole("banner");
      await expect(top).toBeVisible();
      expect((await top.boundingBox())?.height).toBeCloseTo(56, 0);
      // Nội dung bắt đầu ngay dưới thanh trên (không bị đẩy xuống giữa màn khi trang ngắn).
      expect((await page.getByRole("main").boundingBox())?.y).toBeCloseTo(56, 0);
      await expect(page.locator("aside.side")).toBeHidden();
    }
  });

  test("API tắt → băng Hệ thống tạm ngừng hiện, khung vẫn đứng", async ({ page, api }) => {
    await api.down();
    await page.goto("/");
    await expect(page.getByText(TEXT_API_DOWN)).toBeVisible();
    await expect(page.locator(".wordmark:visible")).toBeVisible();
    await expect(page.getByText(TEXT_EMPTY_HOME)).toHaveCount(0);
  });
});
