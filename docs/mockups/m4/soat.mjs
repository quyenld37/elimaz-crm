// Tự soát và chụp ảnh mockup M4 phần hộp chat (Task M4-01, số toàn cục 141). Chép từ docs/mockups/m2/soat.mjs (Task 83),
// soát thêm m4.css và thêm kiểm (9) bong bóng, dòng hội thoại không tràn khung; (10) ảnh dọc rất cao bị giới hạn chiều cao.
// Không phải mã của CRM: chỉ đọc các trang HTML trong thư mục này.
// Chạy: PW_DIR=<thư mục node_modules/playwright> node docs/mockups/m4/soat.mjs [--no-shot] [tên-trang ...]
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

// Trang và cỡ chụp. Kế hoạch: mỗi màn 1512 và 390, thêm 360 cho dòng hội thoại và bong bóng (chữ có thể tràn).
// Mockup vẽ 360 cho cả ba màn vì khung bên phải có tên khách 60 ký tự và khung lên đơn hẹp.
const PAGES = {
	"inbox": [1512, 390, 360],
	"hoi-thoai": [1512, 390, 360],
	"khung-ben-phai": [1512, 390, 360],
	// Trang so sánh chụp sau cùng vì nó nhúng ảnh của các trang trên.
	"index": [1512, 390],
};
// (8) Khung kế hoạch đòi (Task M4-01, Interfaces và tiêu chí CH01–CH19, LV04, DH03): mỗi khóa phải có một phần tử
// [data-ca~="khóa"]. Khóa "chNN", "lv04", "dh03" là ca kiểm của spec 11; các khóa khác là phần Interfaces liệt kê.
const CASES = {
	"inbox": ["ch01", "ch02", "ch06", "ch07", "ch08", "ch16", "cot-page", "loc", "tim", "dong", "ten-60", "0-hoi-thoai",
		"1-hoi-thoai", "200-hoi-thoai", "0-chua-doc", "9-chua-doc", "120-chua-doc", "0-nhan", "14-nhan", "bang-mat-ket-noi",
		"bang-chi-xem", "bang-page-mat-ket-noi"],
	"hoi-thoai": ["ch01", "ch02", "ch03", "ch04", "ch05", "ch07", "ch08", "ch09", "ch10", "ch11", "ch12", "ch13", "ch14",
		"ch18", "ch19", "tin-vao", "tin-ra-nv", "tin-tu-dong", "tin-ngoai-crm", "ghi-chu", "tin-he-thong", "comment-bai",
		"comment-an", "comment-sua-xoa", "anh-cao", "tep", "tt-dang-gui", "tt-da-gui", "tt-dang-kiem-tra", "tt-khong-ro",
		"tt-chua-gui", "tt-loi", "o-go-duoc", "o-go-het-24h", "o-go-human-agent", "o-go-chi-xem", "menu-mau",
		"nhan-rieng-khoa-mot-tin", "nhan-rieng-khoa-7-ngay", "dang-xem", "dang-tra-loi", "tin-lich-su", "comment-2000",
		"ten-60", "ch06", "nut-nhan"],
	"khung-ben-phai": ["dh03", "lv04", "ch06", "ch07", "ch08", "ch15", "ch17", "khach-moi", "khach-cu", "don-gan-day",
		"khong-lien-he", "cung-khach", "don-nhap-live", "len-don", "len-don-khong-quyen", "goi-y-nhan", "nhan", "phu-trach",
		"nhac-viec", "goi-dien", "ket-qua-goi", "ten-60"],
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
	for (const f of ["../m1/m1.css", "../m2/m2.css", "m4.css"]) {
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
		// (9) Bong bóng tin và dòng hội thoại nằm trọn trong khung của chúng (tên 60 ký tự, comment 2.000 ký tự, tên tệp dài).
		for (const el of document.querySelectorAll(".bub, .cv")) {
			if (!vis(el)) continue;
			const frame = el.closest(".thread, .cvlist");
			if (!frame) { errs.push(`bong bóng/dòng ngoài khung .thread/.cvlist: ${where(el)}`); continue; }
			const r = el.getBoundingClientRect();
			const f = frame.getBoundingClientRect();
			if (r.right > f.right + 1 || r.left < f.left - 1) errs.push(`tràn khỏi khung ${Math.round(Math.max(f.left - r.left, r.right - f.right))}px: ${where(el)}`);
			for (const c of el.querySelectorAll("*")) {
				if (!vis(c) || c.closest(".scroll-x")) continue;
				const cr = c.getBoundingClientRect();
				if (cr.right > r.right + 1) errs.push(`chữ tràn khỏi bong bóng/dòng ${Math.round(cr.right - r.right)}px: ${where(c)}`);
			}
		}
		// (10) Ảnh trong bong bóng (kể cả ảnh dọc rất cao) không cao quá 360px.
		for (const el of document.querySelectorAll(".att-img")) {
			if (vis(el) && el.getBoundingClientRect().height > 361) errs.push(`ảnh cao ${Math.round(el.getBoundingClientRect().height)}px > 360px: ${where(el)}`);
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
