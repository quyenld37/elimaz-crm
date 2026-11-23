// Sinh web-campaign-rounding.json và web-campaign-links.json (Task 452) bằng đúng các hàm của website
// (apps/storefront origin/main 90627d0): campaignPrice (src/server/promotions/engine.ts), slugify
// (src/features/text.ts), campaignKey/campaignSlug (src/features/promotions/campaign-link.ts). Hàm chép nguyên văn;
// website kiểm cùng tệp ở M5-29. Chạy: node testdata/contracts/web-campaign.gen.mjs
import { writeFileSync } from "node:fs";

const campaignPrice = (listPrice, percent) => Math.round((listPrice * (100 - percent)) / 100);
const stripAccents = (s) => s.normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/[đĐ]/g, "d").toLowerCase();
const slugify = (s, max = 80) => stripAccents(s).replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, max);
const campaignKey = (id) => id.replace(/-/g, "").slice(0, 8).toLowerCase();
const campaignSlug = (c) => [slugify(c.name, 60), campaignKey(c.id)].filter(Boolean).join("-");

const prices = [1, 2, 3, 49, 50, 51, 99, 101, 149, 150, 151, 1005, 9999, 33333, 99999, 149500, 199000, 249999, 1234567, 9007199254];
const percents = [1, 5, 9, 15, 33, 49, 50, 51, 66, 99];
const rounding = [];
for (const listPrice of prices)
	for (const percent of percents) {
		const salePrice = campaignPrice(listPrice, percent);
		rounding.push({ listPrice, percent, salePrice, applies: salePrice > 0 && salePrice < listPrice });
	}
const dir = new URL(".", import.meta.url);
writeFileSync(new URL("web-campaign-rounding.json", dir), JSON.stringify(rounding, null, 1) + "\n");

const names = [
	"Sale cuối năm",
	"Đầm Lụa Xòe — giảm 30%",
	"  Siêu ưu đãi!!!  ",
	"ĐỒ BỘ MẶC NHÀ đẹp",
	"Ưu đãi tháng 12/2026 (chỉ online)",
	"Áo sơ mi cổ đức & quần tây",
	"Khuyến mãi dài: " + "Thời trang công sở nữ thanh lịch ".repeat(4),
	"!!!",
	"Black Friday 2026",
	"Hè rực rỡ – mua 2 tặng 1",
	"Giảm giá 50% toàn bộ cửa hàng cho khách hàng thân thiết vào dịp lễ",
	"Ngày của mẹ ❤️",
	"x".repeat(59) + " yz", // cắt ở 60 ký tự rơi đúng dấu "-": website giữ "--" trước mã
];
const ids = [
	"0f3a9c2e-7b1d-4e55-9a10-2c3d4e5f6a7b",
	"A1B2C3D4-0000-4000-8000-000000000001",
	"ffffffff-1111-4222-8333-444455556666",
	"12345678-9abc-4def-8123-456789abcdef",
	"deadbeef-cafe-4bab-8eed-0123456789ab",
	"00000000-0000-4000-8000-000000000000",
	"9e107d9d-372b-4b6a-8d1b-6b1f0a3c2e4d",
	"5f4dcc3b-5aa7-465d-8a8f-2f3e4d5c6b7a",
	"c0ffee00-1234-4567-89ab-cdef01234567",
	"7d793037-a076-4d2c-9a1b-0c3e5f7a9b1d",
	"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
	"31415926-5358-4979-8323-846264338327",
	"01936b2a-0000-7000-8000-000000000001",
];
const links = names.map((name, i) => ({ id: ids[i], name, slug: slugify(name, 60), linkKey: campaignKey(ids[i]), path: "/uu-dai/" + campaignSlug({ id: ids[i], name }) }));
writeFileSync(new URL("web-campaign-links.json", dir), JSON.stringify(links, null, 1) + "\n");
