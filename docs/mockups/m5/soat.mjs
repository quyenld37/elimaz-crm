// Tự soát và chụp ảnh mockup M5 đợt 1 (Task M5-22, số toàn cục 205). Chép từ docs/mockups/m3/soat.mjs (Task 131): đổi danh
// sách trang, cỡ chụp (1512 × 767 máy chủ shop, 390 × 844 và 360 × 800 điện thoại), thư mục ảnh shots/, soát thêm m5.css;
// bỏ kiểm ô khóa API của M3; thêm (10) khóa dịch vụ hiện một lần chỉ nằm trong ô chỉ đọc .once có nút chép và lời nhắc,
// không chỗ nào khác chứa khóa đầy đủ; (11) nội dung khách gửi có <script> hiện nguyên chữ, trang không có thẻ script nào
// ngoài khung.js; (12) menu.html: thứ tự nhóm đúng mục "Xếp module" của kế hoạch M5, không có mục đã bỏ hay của đợt 2.
// Không phải mã của CRM: chỉ đọc các trang HTML trong thư mục này.
// Chạy: PW_DIR=<thư mục node_modules/playwright> node docs/mockups/m5/soat.mjs [--no-shot] [tên-trang ...]
// Playwright không thêm vào repo; trên máy chủ shop có sẵn trong bộ đệm npx (~/.npm/_npx/*/node_modules/playwright).
// Thoát 0 khi mọi trang ở mọi cỡ đạt; 1 khi có lỗi (in từng lỗi).
import { createRequire } from "node:module";
import { existsSync, mkdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pwDir = process.env.PW_DIR;
if (!pwDir) {
	console.error("Đặt PW_DIR là thư mục gói playwright, ví dụ ~/.npm/_npx/<id>/node_modules/playwright");
	process.exit(2);
}
const { chromium } = createRequire(join(pwDir, "package.json"))("./index.js");

// Trang và cỡ chụp. Kế hoạch M5 Task 205: 1512 × 767 (máy chủ shop) và 390; luật mockup thêm 360 khi chữ có thể tràn
// (mọi màn M5 đều có tên dài: khách 60 ký tự, chiến dịch 120 ký tự, khóa 80 ký tự).
const ALL = [1512, 390, 360];
const PAGES = {
	"order-requests": ALL,
	"service-keys": ALL,
	"web-carriers": ALL,
	"birthday-gifts": ALL,
	"web-campaigns": ALL,
	"web-banners": ALL,
	"web-looks": ALL,
	"feedback": ALL,
	"web-intake": ALL,
	"audit": ALL,
	"menu": ALL,
	// Trang so sánh chụp sau cùng vì nó nhúng ảnh của các trang trên.
	"index": [1512, 390],
};
// (8) Khung kế hoạch đòi (Task 205 Interfaces và các dòng "(Task 450)"; Interfaces của Task 207–209, 458–462 và task nền):
// mỗi khóa phải có một phần tử [data-ca~="khóa"]. "wb04", "wb05" là ca V của spec 11; các khóa khác là phần Interfaces liệt
// kê và dữ liệu khó (tên dài, 0 / 1 / nhiều dòng, trạng thái rỗng, lỗi, 409, không quyền).
const CASES = {
	"order-requests": ["wb04", "ds-yeu-cau", "loc-trang-thai", "loc-loai", "tim-khong-dau", "loai-doi", "loai-tra", "loai-huy",
		"loai-hoi", "tt-da-tiep-nhan", "tt-dang-kiem-tra", "tt-can-bo-sung", "tt-da-xac-nhan-doi", "tt-da-nhan-hang", "tt-hoan-tat",
		"tt-tu-choi", "tt-khach-huy", "chi-tiet", "dong-hang", "muon-doi-sang", "anh-khach", "anh-dang-xu-ly", "anh-bi-tu-choi",
		"tin-hai-phia", "khach-thay", "noi-bo", "xss-text", "doi-trang-thai", "gui-tra", "ma-goi", "han-gui", "ma-van-don",
		"xac-nhan-dong", "ly-do-tu-choi", "can-bo-sung-bat-buoc", "da-dong", "xung-dot-409", "chi-doc", "0-dong", "200-dong",
		"ten-60", "loi-tai"],
	"service-keys": ["ds-khoa", "tien-to", "quyen", "ip", "lan-dung-cuoi", "da-thu-hoi", "tao-khoa", "khoa-mot-lan", "chep-khoa",
		"da-xu-ly-409", "thu-hoi", "xac-nhan-thu-hoi", "xung-dot-409", "0-khoa", "ten-80"],
	"web-carriers": ["hien-tren-web", "thu-tu", "chon-san", "khong-chon-re-nhat", "phi-du-phong", "it-nhat-mot-hang",
		"chon-san-phai-bat", "tk-khong-hoat-dong", "dang-luu", "xung-dot-409", "chi-doc", "0-tai-khoan"],
	"birthday-gifts": ["wb05", "tab-can-nhan", "tab-tat-ca", "tab-cai-dat", "tab-dot-ma", "so-tong", "tao-ma-ngay", "lan-chay-cuoi",
		"dang-chay-409", "chep-tin", "mo-zalo", "da-nhan", "chon-nhieu", "8-trang-thai", "loc-thang", "xuat-csv", "cai-dat-bat",
		"muc-giam", "don-toi-thieu", "gui-truoc", "dung-trong", "dang-ky-truoc", "loi-boi-so", "xem-truoc-tin", "dot-ma",
		"chi-tiet-dot", "ma-chua-tang", "ma-da-tang", "ma-da-dung", "ma-het-han", "dot-rong", "khong-quyen-403", "0-can-nhan",
		"ten-60", "xem-them"],
	"web-campaigns": ["ds-chien-dich", "loc-trang-thai", "tim", "co-trang", "tt-nhap", "tt-tam-dung", "tt-hen-gio",
		"tt-dang-ap-dung", "tt-het-han", "ten-120", "form", "phan-tram", "pham-vi", "chon-dich", "toi-da-30", "gio-vn",
		"cho-dung-ma", "hang-cat-ma", "xem-truoc-gia", "kich-hoat", "ly-do", "preview-required", "percent-too-high", "tam-dung",
		"sua-khi-chay", "xoa", "canh-bao-banner", "chep-lien-ket", "xung-dot-409", "chi-doc", "0-chien-dich"],
	"web-banners": ["ds-banner", "toi-da-8", "bat-tat", "len-xuong", "nhan-ban", "xoa", "loai-giam-gia", "loai-xu-huong",
		"loai-hang-moi", "loai-danh-muc", "loai-anh", "mau-nen", "chu-tren-banner", "ghim-3", "theo-tg-chien-dich", "tai-anh",
		"anh-dang-xu-ly", "anh-bi-tu-choi", "lech-ti-le", "anh-dien-thoai", "mo-ta-anh", "dich-den", "xem-truoc-may-tinh",
		"xem-truoc-dien-thoai", "ly-do-an", "9-ly-do", "luu-khoa-anh", "co-thay-doi", "xung-dot-409", "dang-luu-khoa", "chi-doc",
		"0-banner"],
	"web-looks": ["ds-bo-phoi", "dem-trang-chu", "toi-da-6", "ten-dip", "ten-goi-y", "san-pham-1-3", "mau", "anh-nguoi-mau",
		"cham-san-pham", "keo-kep", "tieu-de-tu-dong", "cau-mo-dau", "cau-chuyen", "danh-muc-them", "chieu-cao-co",
		"thoi-gian-hien", "ly-do-an", "6-ly-do", "nhan-ban", "xoa-xac-nhan", "xep-thu-tu", "chep-lien-ket", "xem-truoc",
		"xung-dot-409", "0-bo-phoi", "toi-da-500"],
	"feedback": ["ds-gop-y", "loc-trang-thai", "loc-chu-de", "diem", "noi-dung", "xss-text", "lien-he", "don", "trang",
		"thiet-bi", "khach-gan", "khong-gan", "doi-trang-thai", "ghi-chu", "xem-them", "khoi-ho-so", "xung-dot-409",
		"khong-quyen-403", "0-dong"],
	"web-intake": ["dang-nhan", "tam-dung", "ly-do-bat-buoc", "xac-nhan", "dang-tam-dung", "ai-luc-nao", "mo-lai", "dang-luu",
		"xung-dot-409", "chi-doc", "hang-doi-van-vao"],
	"audit": ["ds-nhat-ky", "loc-hanh-dong", "loc-nguoi", "khoang-ngay", "nguoi-nhan-vien", "nguoi-khoa", "nguoi-he-thong",
		"ten-truong", "khong-gia-tri", "xem-them", "0-dong", "khong-quyen-403"],
	"menu": ["nhom-website", "yeu-cau", "gop-y", "chien-dich", "qua-sinh-nhat", "banner", "bo-phoi", "hang-tren-web",
		"nhan-don-web", "khoa-dich-vu", "nhat-ky", "so-dem-menu", "dien-thoai"],
	"index": [],
};
// (12) Thứ tự nhóm menu (kế hoạch M5 mục "Xếp module trên CRM"; "Cài đặt" là hai nhóm "Hệ thống", "Quản trị", QĐ39) và mục
// không được có (Tổng quan bỏ theo QĐ37; Đơn cần kiểm tra, Kết nối Nhanh bỏ; Flash sale là đợt 2).
const NAV_ORDER = ["Đơn hàng", "Vận chuyển", "Tiền", "Khách hàng", "Khuyến mãi", "Website", "Sản phẩm", "Kho", "Hệ thống", "Quản trị"];
const NAV_FORBIDDEN = ["Tổng quan", "Đơn cần kiểm tra", "Kết nối Nhanh", "Flash sale"];
const HEIGHT = { 1512: 767, 390: 844, 360: 800 };

// Tập có sẵn của DESIGN.md (§1 màu kèm ba sắc chữ C+, §2 cỡ chữ, §3 bo góc). Không thêm giá trị ở đây khi chưa có OK.
const HEX = ["#ffffff", "#f5f2ef", "#e8e2dc", "#2b2624", "#6e6560", "#58504d", "#c0392b", "#f9ebe7", "#fcf2ef",
	"#8f857f", "#3e6252", "#d8cfc8", "#1a1614", "#4a423e"];
const COLORS = new Set(HEX.map((h) => `rgb(${parseInt(h.slice(1, 3), 16)}, ${parseInt(h.slice(3, 5), 16)}, ${parseInt(h.slice(5, 7), 16)})`));
const FONT_SIZES = new Set(["12px", "13px", "14px", "15px", "16px", "18px", "26px", "34px"]);
const RADII = new Set(["0px", "2px", "8px", "14px", "999px", "50%"]);

function staticChecks() {
	const errs = [];
	for (const f of ["../m1/m1.css", "../m2/m2.css", "../m3/m3.css", "m5.css"]) {
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
		// (10) Khóa dịch vụ đầy đủ (ek_<tiền tố 12>_<bí mật>) chỉ nằm trong ô .once: ô chỉ đọc, không tự điền, có nút chép
		// và lời nhắc chỉ hiện lần này; mọi chỗ khác (danh sách, nhật ký) chỉ có tiền tố.
		const full = /ek_[a-z2-7]{12}_[A-Za-z2-7]{20,}/;
		for (const el of document.querySelectorAll(".once")) {
			const inp = el.querySelector("input");
			if (!inp || !inp.readOnly) errs.push(`ô khóa hiện một lần không phải ô chỉ đọc: ${where(el)}`);
			else if (!full.test(inp.value)) errs.push(`ô khóa hiện một lần không chứa khóa đầy đủ: ${where(el)}`);
			if (inp && inp.getAttribute("autocomplete") !== "off") errs.push(`ô khóa hiện một lần thiếu autocomplete="off": ${where(el)}`);
			if (![...el.querySelectorAll("button")].some((b) => /Chép/.test(b.textContent))) errs.push(`ô khóa hiện một lần thiếu nút Chép: ${where(el)}`);
			if (!/chỉ hiện (một )?lần/i.test(el.textContent)) errs.push(`ô khóa hiện một lần thiếu lời nhắc "chỉ hiện một lần": ${where(el)}`);
		}
		for (const el of document.querySelectorAll("body *")) {
			if (el.closest(".once") || el.closest("script")) continue;
			const own = [...el.childNodes].filter((n) => n.nodeType === 3).map((n) => n.textContent).join("") + (el.value || "");
			if (full.test(own)) errs.push(`khóa dịch vụ đầy đủ nằm ngoài ô hiện một lần: ${where(el)}`);
		}
		if (name === "service-keys" && !document.querySelector(".once")) errs.push("thiếu ô khóa hiện một lần (.once)");
		// (11) Nội dung khách gửi hiện dạng văn bản thuần: phần tử [data-ca~=xss-text] chứa nguyên chữ "<script>"; trang
		// không có thẻ script nào ngoài khung.js.
		for (const sc of document.querySelectorAll("script")) {
			if (!/(^|\/)khung\.js$/.test(sc.getAttribute("src") || "")) errs.push(`thẻ script ngoài khung.js: ${(sc.getAttribute("src") || sc.textContent).slice(0, 40)}`);
		}
		for (const el of document.querySelectorAll('[data-ca~="xss-text"]')) {
			if (!el.textContent.includes("<script>")) errs.push(`[xss-text] không hiện nguyên chữ "<script>": ${where(el)}`);
		}
		// (12) Menu: thứ tự nhóm, mục đã bỏ hoặc của đợt 2 không có.
		if (name === "menu") {
			const groups = [...document.querySelectorAll(".side .nav-g > p")].map((p) => p.textContent.trim());
			if (JSON.stringify(groups) !== JSON.stringify(navOrder)) errs.push(`thứ tự nhóm menu sai: ${groups.join(" · ")}`);
		}
		for (const a of document.querySelectorAll(".side .nav-a")) {
			const t = a.textContent.replace(/\d+/g, "").trim();
			if (navForbidden.includes(t)) errs.push(`menu còn mục "${t}"`);
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
mkdirSync(join(here, "shots"), { recursive: true });
for (const name of names) {
	const file = join(here, `${name}.html`);
	if (!existsSync(file)) { errors.push(`${name}: chưa có ${name}.html`); continue; }
	for (const width of PAGES[name]) {
		const page = await browser.newPage({ viewport: { width, height: HEIGHT[width] }, deviceScaleFactor: 1 });
		await page.goto(pathToFileURL(file).href, { waitUntil: "networkidle" });
		await page.evaluate(() => document.fonts.ready);
		errors.push(...(await checkPage(page, name, width)));
		if (shoot) await page.screenshot({ path: join(here, "shots", `${name}-${width}.png`), fullPage: true });
		await page.close();
	}
}
await browser.close();
if (errors.length) {
	console.error(errors.join("\n"));
	console.error(`SOÁT MOCKUP: ${errors.length} lỗi`);
	process.exit(1);
}
console.log(`SOÁT MOCKUP OK: ${names.length} trang, ${names.reduce((n, k) => n + PAGES[k].length, 0)} ảnh`);
