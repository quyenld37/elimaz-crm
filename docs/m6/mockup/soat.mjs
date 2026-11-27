// Tự soát và chụp ảnh mockup M6 (Task M6-01, số toàn cục 224): sáu màn báo cáo và hai nút xuất trên màn có sẵn. Chép từ
// docs/mockups/m5/soat.mjs (Task 205/514), giữ kiểm (1)–(9), (11); đổi danh sách trang, khóa CASES, thứ tự menu (thêm
// nhóm "Hộp chat" của M4 và "Báo cáo" của M6); ảnh ghi thẳng vào thư mục này (kế hoạch: docs/m6/mockup/*.png); bỏ kiểm khóa
// dịch vụ (10) của M5. Thêm: (13) bảng số liệu .rt luôn nằm trong khung .scroll-x và không đổi thành thẻ ở điện thoại (đầu
// bảng còn hiện) — bảng rộng chỉ cuộn ngang trong khung, không cuộn cả trang; (14) vùng [data-ca~=khong-nut-xuat] (người
// chỉ có quyền một Page / một kho) không có nút hay liên kết nào chứa chữ "Xuất"; (15) mục "Tổng quan" chỉ nằm trong
// nhóm "Báo cáo" (M5 bỏ màn Tổng quan của admin, QĐ37); (16) ô tỷ lệ mẫu số 0 [data-ca~=ty-le-trong] hiện đúng "—".
// Chụp thêm ảnh "Hiện tại" của hai màn có sẵn (mockup đã duyệt M2 khach-danh-sach, M1 san-pham-danh-sach), không soát.
// Không phải mã của CRM: chỉ đọc các trang HTML cục bộ.
// Chạy: PW_DIR=<thư mục node_modules/playwright> node docs/m6/mockup/soat.mjs [--no-shot] [tên-trang ...]
// Playwright không thêm vào repo; trên máy chủ shop có sẵn trong bộ đệm npx (~/.npm/_npx/*/node_modules/playwright).
// Thoát 0 khi mọi trang ở mọi cỡ đạt; 1 khi có lỗi (in từng lỗi).
import { createRequire } from "node:module";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pwDir = process.env.PW_DIR;
if (!pwDir) {
	console.error("Đặt PW_DIR là thư mục gói playwright, ví dụ ~/.npm/_npx/<id>/node_modules/playwright");
	process.exit(2);
}
const { chromium } = createRequire(join(pwDir, "package.json"))("./index.js");

// Trang và cỡ chụp. Kế hoạch M6 Task 224 Step 1: 1512 × 767 (máy chủ shop) và 390; thêm 360 cho bảng có thể tràn chữ
// (mọi màn M6 có bảng số rộng, tên sản phẩm 120 ký tự, tên nhân viên dài, tiền 9 chữ số).
const ALL = [1512, 390, 360];
const PAGES = {
	"tong-quan": ALL,
	"don": ALL,
	"van-chuyen": ALL,
	"ban-chay": ALL,
	"ton-kho": ALL,
	"tep-xuat": ALL,
	"nut-xuat": ALL,
	// Trang so sánh chụp sau cùng vì nó nhúng ảnh của các trang trên.
	"index": [1512, 390],
};
// Ảnh "Hiện tại" của màn có sẵn bị đổi hành vi (c), (d): chụp mockup đã duyệt, không soát.
const CURRENT = { "hien-tai-khach": "../../mockups/m2/khach-danh-sach.html", "hien-tai-san-pham": "../../mockups/m1/san-pham-danh-sach.html" };
// (8) Khung kế hoạch đòi (Task 224 Interfaces và dòng "Dữ liệu cứng"; Interfaces, Step 1 của Task 239–241; Q8, Q11, Q12,
// Q21–Q23; lỗi của Task 225): mỗi khóa phải có một phần tử [data-ca~="khóa"]. "bc01"… "pq06" là ca của docs/m6/tieu-chi.md.
const CASES = {
	"tong-quan": ["bc01", "chon-ky", "hom-nay", "hom-qua", "7-ngay", "thang-nay", "tuy-chon", "qua-366", "ngay-tuong-lai",
		"o-don-len", "o-doanh-so", "o-gia-tri-tb", "o-huy", "o-thanh-cong", "o-hoan", "o-khach-tra", "cho-xu-ly", "cho-xac-nhan",
		"cho-gui-hang", "dang-yeu-cau-huy", "theo-kenh", "khong-xac-dinh", "ty-le-trong", "theo-ngay", "thanh-ngang",
		"cap-nhat-luc", "tai-lai", "chu-thich-doanh-so", "loai-ngay", "xuat-excel", "mo-chi-tiet", "ngan-chi-tiet",
		"trang-chi-tiet-dt", "tong-dau-bang", "xem-them-chi-tiet", "tien-9-so", "0-don", "1-don", "dang-tai", "loi-tai", "ban-503",
		"chay-nen", "chay-nen-xong", "qua-nang", "mot-page", "khong-nut-xuat", "khong-quyen-403", "ten-nv-dai"],
	"don": ["bc02", "co-so-ngay", "ngay-len-don", "ngay-hoan-tat", "nhom-theo", "8-chieu", "loc-nhieu", "loc-page", "bo-loc",
		"cot-day-du", "dong-tong", "o-bam", "ngan-chi-tiet", "tong-bang-o", "an-cot-don-len", "ty-le-trong", "khong-xac-dinh",
		"chua-phan-cong", "ten-nv-dai", "theo-ngay", "tien-9-so", "0-don", "1-don", "dang-tai", "loi-tai", "chay-nen",
		"xuat-excel", "mot-page", "khong-nut-xuat", "trang-chi-tiet-dt", "khong-quyen-403", "chu-thich-doanh-so", "loai-ngay"],
	"van-chuyen": ["theo-hang", "theo-tai-khoan", "tu-giao", "cot-van-don", "giao-that-bai", "ty-le-thanh-cong", "ty-le-trong",
		"tong-phi", "phi-tb", "tong-cod", "tien-9-so", "loai-ngay", "mo-bao-cao-don", "dong-tong", "0-van-don", "dang-tai",
		"loi-tai", "xuat-excel", "ten-tk-dai"],
	"ban-chay": ["theo-sku", "theo-mau", "co-so-chua-huy", "co-so-thanh-cong", "so-luong", "so-don", "doanh-so", "khach-tra",
		"ten-120", "50-dong", "xem-them-500", "toi-da-500", "1200-sku", "0-dong", "dang-tai", "loi-tai", "xuat-excel", "thu-tu-mau",
		"loai-ngay", "chu-thich-doanh-so"],
	"ton-kho": ["bc03", "tab-ton", "tab-xnt", "hom-nay", "ngay-da-qua", "ban-chup-24h", "chua-co-ban-chup", "chon-kho", "tim-sku",
		"cot-ton", "co-the-ban", "1200-sku", "danh-sach-ao", "ten-120", "het-hang", "xnt-cot", "dau-ky", "cuoi-ky", "dieu-chinh-am",
		"xnt-0", "pham-vi-kho", "khong-nut-xuat", "xuat-excel", "xuat-san-pham", "dang-tai", "loi-tai", "khong-quyen-403"],
	"tep-xuat": ["bc04", "ds-lan-chay", "cua-toi", "moi-nguoi", "nhom-ban-hang", "nhom-ton", "nhom-khach", "tt-dang-cho",
		"tt-dang-chay", "tt-xong", "tt-loi-qua-nang", "tt-loi-ban", "tt-loi-quyen", "tt-loi", "tt-het-han", "so-dong", "da-cat",
		"han-tai", "tai-ve", "xem", "thong-bao-chay-nen", "thong-bao-xong", "dang-tao-tep", "qua-5-lan", "het-han-410",
		"duong-dan-5-phut", "0-lan-chay", "dang-tai", "loi-tai", "tu-tai-lai", "khong-o-cong-thuc"],
	"nut-xuat": ["pq06", "nut-xuat-khach", "hop-xuat-khach", "khong-o-chu", "cot-tep-khach", "han-24h", "ghi-nhat-ky",
		"khong-quyen-khach", "khong-nut-xuat", "nut-xuat-san-pham", "hop-xuat-san-pham", "cot-mau-nhap", "khong-gia-von",
		"khong-quyen-san-pham", "dang-gui"],
	"index": [],
};
// (12) Thứ tự nhóm menu: console/src/app/nav.ts (NAV_GROUPS trên main: Hộp chat của M4 đứng đầu … Quản trị) cộng nhóm
// "Báo cáo" của M6 giữa "Kho" và "Hệ thống" (quyết định thay chủ shop, tệp task). Mục không được có (M5: Đơn cần kiểm
// tra, Kết nối Nhanh bỏ).
const NAV_ORDER = ["Hộp chat", "Đơn hàng", "Vận chuyển", "Tiền", "Khách hàng", "Khuyến mãi", "Website", "Sản phẩm", "Kho", "Báo cáo", "Hệ thống", "Quản trị"];
const NAV_FORBIDDEN = ["Đơn cần kiểm tra", "Kết nối Nhanh"];
const HEIGHT = { 1512: 767, 390: 844, 360: 800 };

// Tập có sẵn của DESIGN.md (§1 màu kèm ba sắc chữ C+, §2 cỡ chữ, §3 bo góc). Không thêm giá trị ở đây khi chưa có OK.
const HEX = ["#ffffff", "#f5f2ef", "#e8e2dc", "#2b2624", "#6e6560", "#58504d", "#c0392b", "#f9ebe7", "#fcf2ef",
	"#8f857f", "#3e6252", "#d8cfc8", "#1a1614", "#4a423e"];
const COLORS = new Set(HEX.map((h) => `rgb(${parseInt(h.slice(1, 3), 16)}, ${parseInt(h.slice(3, 5), 16)}, ${parseInt(h.slice(5, 7), 16)})`));
const FONT_SIZES = new Set(["12px", "13px", "14px", "15px", "16px", "18px", "26px", "34px"]);
const RADII = new Set(["0px", "2px", "8px", "14px", "999px", "50%"]);

function staticChecks() {
	const errs = [];
	for (const f of ["../../mockups/m1/m1.css", "../../mockups/m2/m2.css", "../../mockups/m3/m3.css", "m6.css"]) {
		if (!existsSync(join(here, f))) { errs.push(`${f}: chưa có`); continue; }
		const css = readFileSync(join(here, f), "utf8");
		// Không có gì chỉ hiện khi rê chuột: luật :hover chỉ được đổi màu, nền, viền, gạch chân.
		for (const m of css.matchAll(/([^{}]*:hover[^{}]*)\{([^}]*)\}/g)) {
			const body = m[2];
			if (/\b(display|visibility|opacity|height|max-height|width|transform)\s*:/.test(body)) {
				errs.push(`${f}: luật "${m[1].trim()}" làm hiện/ẩn khi rê chuột`);
			}
		}
	}
	return errs;
}

async function checkPage(page, name, width) {
	return page.evaluate(({ name, width, colors, sizes, radii, cases, navOrder, navForbidden }) => {
		const errs = [];
		const vis = (el) => {
			const r = el.getBoundingClientRect();
			const s = getComputedStyle(el);
			return r.width > 0 && r.height > 0 && s.visibility !== "hidden" && s.display !== "none";
		};
		const where = (el) => {
			const t = (el.innerText || el.value || el.getAttribute("aria-label") || "").trim().replace(/\s+/g, " ").slice(0, 40);
			return `${el.tagName.toLowerCase()}${el.id ? "#" + el.id : ""}${el.className && typeof el.className === "string" ? "." + el.className.trim().split(/\s+/).join(".") : ""} "${t}"`;
		};
		// (1) Không cuộn ngang.
		const sw = document.documentElement.scrollWidth;
		if (sw > innerWidth + 1) errs.push(`cuộn ngang: trang rộng ${sw}px > ${innerWidth}px`);
		for (const el of document.body.querySelectorAll("*")) {
			if (!vis(el)) continue;
			const r = el.getBoundingClientRect();
			const s = getComputedStyle(el);
			if (el.closest(".scroll-x") === null && r.right > innerWidth + 1 && s.position !== "fixed") {
				errs.push(`tràn phải ${Math.round(r.right - innerWidth)}px: ${where(el)}`);
			}
			// Tràn trái (M2): phần tử bị đẩy ra ngoài mép trái hoặc đè lên ô bên cạnh trong thẻ dòng.
			if (el.closest(".scroll-x") === null && r.left < -1 && s.position !== "fixed") {
				errs.push(`tràn trái ${Math.round(-r.left)}px: ${where(el)}`);
			}
			const box = el.parentElement && el.parentElement.closest("td");
			if (box && el.closest(".scroll-x") === null && s.position !== "absolute") {
				const b = box.getBoundingClientRect();
				if (r.left < b.left - 1 || r.right > b.right + 1) errs.push(`tràn khỏi ô bảng ${Math.round(Math.max(b.left - r.left, r.right - b.right))}px: ${where(el)}`);
			}
			// (2) Chữ bị cắt: phần tử tự cắt mà không có dấu … (ellipsis hoặc line-clamp).
			const clips = s.overflowX === "hidden" || s.overflow === "hidden";
			const marked = s.textOverflow === "ellipsis" || s.webkitLineClamp !== "none";
			if (clips && !marked && !el.classList.contains("sr") && el.children.length === 0 && el.scrollWidth > el.clientWidth + 1) {
				errs.push(`chữ bị cắt: ${where(el)}`);
			}
			// (4) Token: màu chữ, nền, viền; cỡ chữ; bo góc.
			for (const p of ["color", "backgroundColor", "borderTopColor", "borderBottomColor", "borderLeftColor", "borderRightColor"]) {
				const v = s[p];
				if (v === "rgba(0, 0, 0, 0)" || v === "transparent") continue;
				const side = p.startsWith("border") ? p.replace("border", "").replace("Color", "").toLowerCase() : "";
				if (side && (s[`border${side[0].toUpperCase()}${side.slice(1)}Width`] === "0px" || s[`border${side[0].toUpperCase()}${side.slice(1)}Style`] === "none")) continue;
				if (!colors.includes(v)) errs.push(`màu ngoài token ${p}=${v}: ${where(el)}`);
			}
			if (el.childNodes.length && [...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim())) {
				if (!sizes.includes(s.fontSize)) errs.push(`cỡ chữ ngoài bộ ${s.fontSize}: ${where(el)}`);
			}
			for (const c of ["borderTopLeftRadius", "borderTopRightRadius", "borderBottomLeftRadius", "borderBottomRightRadius"]) {
				const v = s[c];
				const ok = radii.includes(v) || (v.endsWith("px") && parseFloat(v) >= 999);
				if (!ok) errs.push(`bo góc ngoài bộ ${c}=${v}: ${where(el)}`);
			}
		}
		// (3) Mục chạm.
		const min = width < 1000 ? 44 : 32;
		for (const el of document.querySelectorAll("a, button, input, select, textarea, summary, [role=button], [tabindex]")) {
			if (!vis(el) || el.closest(".mk")) continue;
			let box = el.getBoundingClientRect();
			if (el.type === "checkbox" || el.type === "radio") {
				const lab = el.closest("label");
				if (!lab) { errs.push(`ô chọn không có nhãn bọc: ${where(el)}`); continue; }
				box = lab.getBoundingClientRect();
				if (box.height < min) errs.push(`mục chạm thấp ${Math.round(box.height)}px < ${min}px: ${where(lab)}`);
				continue;
			}
			if (el.type === "hidden") continue;
			if (box.height < min - 0.5) errs.push(`mục chạm thấp ${Math.round(box.height)}px < ${min}px: ${where(el)}`);
			if (box.width < min - 0.5) errs.push(`mục chạm hẹp ${Math.round(box.width)}px < ${min}px: ${where(el)}`);
		}
		// (5) Mọi select có mũi tên nằm trong ô.
		for (const el of document.querySelectorAll("select")) {
			if (!el.parentElement.classList.contains("select-wrap")) errs.push(`select không nằm trong .select-wrap: ${where(el)}`);
			const cs = getComputedStyle(el);
			if (cs.appearance !== "none") errs.push(`select còn mũi tên của trình duyệt: ${where(el)}`);
			// Chữ của lựa chọn đang chọn phải vừa ô (trình duyệt cắt mà không có dấu …).
			const ctx = document.createElement("canvas").getContext("2d");
			ctx.font = `${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
			const text = el.options[el.selectedIndex]?.text || "";
			const room = el.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
			if (vis(el) && ctx.measureText(text).width > room + 1) errs.push(`chữ trong ô chọn bị cắt: ${where(el)} "${text}"`);
		}
		// (7) Tiền dạng "690.000 ₫" (âm dùng dấu −).
		for (const el of document.querySelectorAll(".money")) {
			const t = el.textContent.trim();
			if (!/^[−+]?\d{1,3}(\.\d{3})* ₫$/.test(t)) errs.push(`tiền sai dạng "${t}": ${where(el)}`);
		}
		// (8) Đủ trạng thái kế hoạch đòi.
		for (const c of cases) {
			if (!document.querySelector(`[data-ca~="${c}"]`)) errs.push(`thiếu trạng thái "${c}"`);
		}
		// (9) Ô tiền (kể cả 9 chữ số) nằm trọn trong ô bảng, ô số, thẻ hay hộp chứa nó.
		for (const el of document.querySelectorAll(".money")) {
			if (!vis(el) || el.closest(".scroll-x")) continue;
			const box = el.closest("td, .cod-c, .kpi, .stat, .sum-r, .grp, .dlg, .card, .side-c");
			if (!box) continue;
			// Đo phần chữ thật (range), không đo khung phần tử: chữ có thể tràn khỏi chính phần tử của nó.
			const rg = document.createRange();
			rg.selectNodeContents(el);
			const r = rg.getBoundingClientRect();
			const b = box.getBoundingClientRect();
			if (r.right > b.right + 1 || r.left < b.left - 1) errs.push(`tiền tràn khỏi ô ${Math.round(Math.max(b.left - r.left, r.right - b.right))}px: ${where(el)}`);
		}
		// (11) Nội dung khách gửi hiện dạng văn bản thuần: phần tử [data-ca~=xss-text] chứa nguyên chữ "<script>"; trang
		// không có thẻ script nào ngoài khung.js.
		for (const sc of document.querySelectorAll("script")) {
			if (!/(^|\/)khung\.js$/.test(sc.getAttribute("src") || "")) errs.push(`thẻ script ngoài khung.js: ${(sc.getAttribute("src") || sc.textContent).slice(0, 40)}`);
		}
		for (const el of document.querySelectorAll('[data-ca~="xss-text"]')) {
			if (!el.textContent.includes("<script>")) errs.push(`[xss-text] không hiện nguyên chữ "<script>": ${where(el)}`);
		}
		// (12) Menu: thứ tự nhóm, mục đã bỏ không có.
		const groups = [...document.querySelectorAll(".side .nav-g > p")].map((p) => p.textContent.trim());
		if (groups.length && JSON.stringify(groups) !== JSON.stringify(navOrder)) errs.push(`thứ tự nhóm menu sai: ${groups.join(" · ")}`);
		for (const a of document.querySelectorAll(".side .nav-a")) {
			const t = a.textContent.replace(/\d+/g, "").trim();
			if (navForbidden.includes(t)) errs.push(`menu còn mục "${t}"`);
			// (15) "Tổng quan" chỉ trong nhóm Báo cáo.
			if (t === "Tổng quan" && a.closest(".nav-g").querySelector("p").textContent.trim() !== "Báo cáo") errs.push("mục Tổng quan nằm ngoài nhóm Báo cáo");
		}
		// (13) Bảng số liệu: trong .scroll-x, đầu bảng còn hiện ở mọi cỡ.
		for (const t of document.querySelectorAll("table.rt")) {
			if (!t.closest(".scroll-x")) errs.push(`bảng .rt không nằm trong .scroll-x: ${where(t)}`);
			const th = t.querySelector("thead");
			if (vis(t) && (!th || !vis(th))) errs.push(`bảng .rt mất đầu bảng: ${where(t)}`);
		}
		// (14) Người chỉ có quyền một Page / một kho: không có nút xuất.
		for (const z of document.querySelectorAll('[data-ca~="khong-nut-xuat"]')) {
			for (const b of z.querySelectorAll("a, button")) if (/Xuất/.test(b.textContent)) errs.push(`vùng không quyền xuất còn nút "${b.textContent.trim()}"`);
		}
		// (16) Tỷ lệ mẫu số 0 hiện "—".
		for (const el of document.querySelectorAll('[data-ca~="ty-le-trong"]')) {
			if (el.textContent.trim() !== "—") errs.push(`ô tỷ lệ mẫu số 0 không hiện "—": "${el.textContent.trim()}"`);
		}
		return errs.map((e) => `${name} @${width}: ${e}`);
	}, { name, width, colors: [...COLORS], sizes: [...FONT_SIZES], radii: [...RADII], cases: CASES[name] || [], navOrder: NAV_ORDER, navForbidden: NAV_FORBIDDEN });
}

const args = process.argv.slice(2);
const shoot = !args.includes("--no-shot");
const only = args.filter((a) => !a.startsWith("--"));
const names = only.length ? only : Object.keys(PAGES);
const errors = staticChecks();
const browser = await chromium.launch();
for (const name of names) {
	const file = join(here, `${name}.html`);
	if (!existsSync(file)) { errors.push(`${name}: chưa có ${name}.html`); continue; }
	for (const width of PAGES[name]) {
		const page = await browser.newPage({ viewport: { width, height: HEIGHT[width] }, deviceScaleFactor: 1 });
		await page.goto(pathToFileURL(file).href, { waitUntil: "networkidle" });
		await page.evaluate(() => document.fonts.ready);
		errors.push(...(await checkPage(page, name, width)));
		if (shoot) await page.screenshot({ path: join(here, `${name}-${width}.png`), fullPage: true });
		await page.close();
	}
}
if (shoot && !only.length) {
	for (const [name, rel] of Object.entries(CURRENT)) {
		for (const width of [1512, 390]) {
			const page = await browser.newPage({ viewport: { width, height: HEIGHT[width] }, deviceScaleFactor: 1 });
			await page.goto(pathToFileURL(join(here, rel)).href, { waitUntil: "networkidle" });
			await page.evaluate(() => document.fonts.ready);
			await page.screenshot({ path: join(here, `${name}-${width}.png`), fullPage: true });
			await page.close();
		}
	}
}
await browser.close();
if (errors.length) {
	console.error(errors.join("\n"));
	console.error(`SOÁT MOCKUP: ${errors.length} lỗi`);
	process.exit(1);
}
console.log(`SOÁT MOCKUP OK: ${names.length} trang, ${names.reduce((n, k) => n + PAGES[k].length, 0)} ảnh`);
