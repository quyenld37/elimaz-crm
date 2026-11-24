// Sinh web-look-links.json (Task 454) bằng đúng các hàm của website (apps/storefront-bct): slugify (src/features/text.ts),
// lookKey/lookSlug/lookPath (src/features/looks/look-link.ts). Hàm chép nguyên văn; website kiểm cùng tệp ở
// M5-52. Chạy: node testdata/contracts/web-look.gen.mjs
import { writeFileSync } from "node:fs";

const stripAccents = (s) => s.normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/[đĐ]/g, "d").toLowerCase();
const slugify = (s, max = 80) => stripAccents(s).replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, max);
const LOOK_KEY_LENGTH = 8;
const lookKey = (id) => id.replace(/-/g, "").slice(0, LOOK_KEY_LENGTH).toLowerCase();
const lookSlug = (l) => [slugify(l.name, 60), lookKey(l.id)].filter(Boolean).join("-");
const lookPath = (l) => "/bo-phoi/" + lookSlug(l);

const names = [
	"Đi làm ngày thường",
	"Họp quan trọng",
	"Đứng lớp cả ngày",
	"Hẹn sau giờ làm",
	"Cuối tuần ở nhà",
	"  Dạo phố!!!  ",
	"ĐI TIỆC CƯỚI",
	"!!!",
	"Sáng thứ Hai — họp",
	"x".repeat(27) + "y",
	"Ngày của mẹ ❤️",
	"Áo & quần",
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
	"01936b2a-0000-7000-8000-000000000001",
];
const links = names.map((name, i) => {
	const l = { id: ids[i], name };
	return { id: ids[i], name, slug: slugify(name, 60), linkKey: lookKey(ids[i]), path: lookPath(l)};
});
writeFileSync(new URL("web-look-links.json", new URL(".", import.meta.url)), JSON.stringify(links, null, 1) + "\n");
