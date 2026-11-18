// Tự soát và chụp ảnh mockup M2 (Task M2-26). Chép từ docs/mockups/m1/soat.mjs (Task M1-49), thêm kiểm (7) dạng tiền
// "690.000 ₫" và (8) đủ trạng thái kế hoạch đòi. Không phải mã của CRM: chỉ đọc các trang HTML trong thư mục này.
// Chạy: PW_DIR=<thư mục node_modules/playwright> node docs/mockups/m2/soat.mjs [--no-shot] [tên-trang ...]
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

// Trang và cỡ chụp. 360 cho màn có chữ dễ tràn (tên sản phẩm 120 ký tự, tên khách dài, bảng dòng đơn).
const PAGES = {
	"len-don": [1512, 390, 360],
	"don-danh-sach": [1512, 390, 360],
	"don-chi-tiet": [1512, 390, 360],
	"don-trung": [1512, 390, 360],
	"khach-danh-sach": [1512, 390, 360],
	"khach-ho-so": [1512, 390, 360],
	"khach-gop": [1512, 390, 360],
	"ma-giam-gia": [1512, 390, 360],
	"cai-dat-don": [1512, 390],
	// Trang so sánh chụp sau cùng vì nó nhúng ảnh của các trang trên.
	"index": [1512, 390],
};
// (8) Trạng thái kế hoạch đòi cho từng màn (Task M2-26, Interfaces): mỗi khóa phải có một phần tử [data-ca~="khóa"].
const CASES = {
	"len-don": ["0-dong", "20-dong", "ten-120", "het-hang", "cho-duyet", "ma-het-luot", "mat-mang", "canh-bao-trung", "cung-khach", "phim-tat"],
	"don-danh-sach": ["thanh-nhan", "loc", "nghi-trung", "tai-them", "tu-lam-moi", "trong"],
	"don-chi-tiet": ["dong-tong", "khach-chi-so", "nut-theo-scope", "lich-su", "ghi-chu", "phieu-tra", "hop-huy", "hop-tra-doi", "hop-nhan-tra", "da-thay-doi"],
	"don-trung": ["cap", "khong-trung", "huy-don", "trong"],
	"khach-danh-sach": ["danh-sach", "tim", "trong", "nhap-excel", "nhan-quy-tac"],
	"khach-ho-so": ["ho-so-gop", "danh-tinh", "dia-chi", "nhan", "chi-so", "dong-thoi-gian", "ghi-chu"],
	"khach-gop": ["hai-ho-so", "chon-giu", "xac-nhan", "lan-gop", "hoan-tac-loi"],
	"ma-giam-gia": ["danh-sach", "tao-ma", "tao-dot", "chi-tiet", "tat-ma", "truong-khoa"],
	"cai-dat-don": ["bon-cai-dat", "nguon-don", "ngoai-gioi-han"],
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
	for (const f of ["../m1/m1.css", "m2.css"]) {
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
		return errs.map((e) => `${name} @${width}: ${e}`);
	}, { name, width, colors: [...COLORS], sizes: [...FONT_SIZES], radii: [...RADII], cases: CASES[name] || [] });
}

const args = process.argv.slice(2);
const shoot = !args.includes("--no-shot");
const only = args.filter((a) => !a.startsWith("--"));
const names = only.length ? only : Object.keys(PAGES);
const errors = staticChecks();
const browser = await chromium.launch();
mkdirSync(join(here, "png"), { recursive: true });
for (const name of names) {
	const file = join(here, `${name}.html`);
	if (!existsSync(file)) { errors.push(`${name}: chưa có ${name}.html`); continue; }
	for (const width of PAGES[name]) {
		const page = await browser.newPage({ viewport: { width, height: HEIGHT[width] }, deviceScaleFactor: 1 });
		await page.goto(pathToFileURL(file).href, { waitUntil: "networkidle" });
		await page.evaluate(() => document.fonts.ready);
		errors.push(...(await checkPage(page, name, width)));
		if (shoot) await page.screenshot({ path: join(here, "png", `${name}-${width}.png`), fullPage: true });
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
