// Tự soát và chụp ảnh mockup M7 (Task M7-02, số toàn cục 247): màn 10 (Kiểm kho mở sổ) và 13 (Hàng hoàn của đơn cũ).
// Chép từ docs/mockups/m5/soat.mjs (Task 205, 514): đổi danh sách trang, khóa CASES, soát thêm m7.css; bỏ kiểm ô khóa dịch
// vụ (10), nội dung <script> và thứ tự menu (12) của M5 (hai màn M7 không có khóa, không có nội dung khách gửi, không đổi
// thứ tự nhóm menu); thêm (13) màn mở sổ: mọi nút "Đăng" trong trạng thái còn dòng chưa đếm ([data-ca~="dang-khoa"]) có
// thuộc tính disabled.
// Không phải mã của CRM: chỉ đọc các trang HTML trong thư mục này.
// Chạy: PW_DIR=<thư mục node_modules/playwright> node docs/mockups/m7/soat.mjs [--no-shot] [tên-trang ...]
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

// Trang và cỡ chụp. Kế hoạch M7 Task 247 Step 2: 1512 × 767; màn 10 thêm 360 và 390 (nhân viên kho đếm bằng điện thoại);
// màn 13 cũng chụp 390, 360 (tên sản phẩm dài; nhân viên kho nhập hàng hoàn ngay ở kệ).
const ALL = [1512, 390, 360];
const PAGES = {
	"kiem-kho-mo-so": ALL,
	"hang-hoan-don-cu": ALL,
	// Trang so sánh chụp sau cùng vì nó nhúng ảnh của các trang trên.
	"index": [1512, 390],
};
// (8) Khung kế hoạch đòi (Task 247 bảng màn 10, 13; Interfaces và Step 1 của Task 262, 263, 264, 273): mỗi khóa phải có
// một phần tử [data-ca~="khóa"]. "mo02", "mo08" là ca của spec 11.
const CASES = {
	"kiem-kho-mo-so": ["mo02", "loi-vao", "tao-phien", "chon-kho", "so-dong-can-dem", "kho-khong-trong", "da-co-phien-mo-so",
		"dang-co-phien-thuong", "quet-ma", "cong-1", "bot-1", "vua-quet", "nhieu-nguoi", "cho-gui", "gui-lai-cung-khoa", "ma-la",
		"ma-trung-nguoi-ban", "so-am", "ten-dai", "con-n-dong", "tien-do", "ds-chua-dem", "nhap-0", "nhap-excel", "chon-che-do",
		"dang-xu-ly", "ket-qua-nhap", "loi-tung-dong", "da-nhap-409", "tep-sai", "nhap-that-bai", "dang-khoa", "xac-nhan-dang",
		"uncounted-422", "da-dang-409", "kho-co-hang-luc-dang", "ket-qua-kiem", "kiem-khop", "kiem-lech", "da-co-chung-tu-khac",
		"sua-bang-phieu-dieu-chinh", "khong-quyen-403", "loi-tai"],
	"hang-hoan-don-cu": ["mo08", "form", "chon-kho", "ma-tham-chieu", "chuan-hoa", "them-dong", "quet-ma", "tinh-trang-tot",
		"tinh-trang-hong", "so-luong", "ghi-chu", "nhap-kho", "dang-gui", "da-nhap-409", "lien-ket-phieu-cu", "so-luong-0",
		"tham-chieu-ngan", "ma-la", "chua-co-dong", "ket-qua-tao", "phieu-hong", "ds-theo-ngay", "khoang-ngay", "nhieu-ngay",
		"0-dong", "khong-quyen-403", "ten-dai", "loi-tai"],
	"index": [],
};
const HEIGHT = { 1512: 767, 390: 844, 360: 800 };

// Tập có sẵn của DESIGN.md (§1 màu kèm ba sắc chữ C+, §2 cỡ chữ, §3 bo góc). Không thêm giá trị ở đây khi chưa có OK.
const HEX = ["#ffffff", "#f5f2ef", "#e8e2dc", "#2b2624", "#6e6560", "#58504d", "#c0392b", "#f9ebe7", "#fcf2ef",
	"#8f857f", "#3e6252", "#d8cfc8", "#1a1614", "#4a423e"];
const COLORS = new Set(HEX.map((h) => `rgb(${parseInt(h.slice(1, 3), 16)}, ${parseInt(h.slice(3, 5), 16)}, ${parseInt(h.slice(5, 7), 16)})`));
const FONT_SIZES = new Set(["12px", "13px", "14px", "15px", "16px", "18px", "26px", "34px"]);
const RADII = new Set(["0px", "2px", "8px", "14px", "999px", "50%"]);

function staticChecks() {
	const errs = [];
	for (const f of ["../m1/m1.css", "../m2/m2.css", "../m3/m3.css", "m7.css"]) {
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
	return page.evaluate(({ name, width, colors, sizes, radii, cases }) => {
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
		// (11) Trang không có thẻ script nào ngoài khung.js.
		for (const sc of document.querySelectorAll("script")) {
			if (!/(^|\/)khung\.js$/.test(sc.getAttribute("src") || "")) errs.push(`thẻ script ngoài khung.js: ${(sc.getAttribute("src") || sc.textContent).slice(0, 40)}`);
		}
		// (13) Màn mở sổ: còn dòng chưa đếm thì nút "Đăng" khóa (Task 273 Interfaces).
		for (const box of document.querySelectorAll('[data-ca~="dang-khoa"]')) {
			const btns = [...box.querySelectorAll("button")].filter((b) => /^Đăng/.test(b.textContent.trim()));
			if (!btns.length) errs.push(`[dang-khoa] không có nút "Đăng": ${where(box)}`);
			for (const b of btns) if (!b.disabled) errs.push(`nút "Đăng" không khóa khi còn dòng chưa đếm: ${where(b)}`);
		}
		return errs.map((e) => `${name} @${width}: ${e}`);
	}, { name, width, colors: [...COLORS], sizes: [...FONT_SIZES], radii: [...RADII], cases: CASES[name] || [] });
}

const args = process.argv.slice(2);
const shoot = !args.includes("--no-shot");
const only = args.filter((a) => !a.startsWith("--"));
const names = only.length ? only : Object.keys(PAGES);
const errors = staticChecks();
// PW_CHANNEL=chrome dùng Google Chrome đã cài trên máy khi bộ đệm Playwright chưa có trình duyệt riêng (28/11: máy chủ
// shop không có ~/Library/Caches/ms-playwright).
const browser = await chromium.launch(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {});
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
