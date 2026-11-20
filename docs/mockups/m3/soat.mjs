// Tự soát và chụp ảnh mockup M3 (Task M3-40, số toàn cục 131). Chép từ docs/mockups/m4/soat.mjs (Task 141): đổi danh sách
// trang và cỡ chụp (kế hoạch M3: 1512 và 1280, màn kho thêm 390), soát m3.css; bỏ kiểm bong bóng, ảnh của hộp chat; thêm
// (9) ô tiền (kể cả 9 chữ số) nằm trọn trong ô, thẻ chứa nó; (10) ô khóa API là ô mật khẩu rỗng (không hiện lại khóa đã lưu).
// Không phải mã của CRM: chỉ đọc các trang HTML trong thư mục này.
// Chạy: PW_DIR=<thư mục node_modules/playwright> node docs/mockups/m3/soat.mjs [--no-shot] [tên-trang ...]
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

// Trang và cỡ chụp. Kế hoạch M3 Task 131: mỗi màn 1512 và 1280, màn kho thêm 390. Mockup chụp thêm 390 cho mọi màn
// (console chạy trên điện thoại; nhân viên gọi lại khách bằng điện thoại) và 360 cho màn kho (ô Tốt/Hỏng/Thiếu dễ tràn).
const PAGES = {
	"ket-noi-van-chuyen": [1512, 1280, 390],
	"don-van-chuyen": [1512, 1280, 390],
	"the-khach": [1512, 1280, 390],
	"van-hanh": [1512, 1280, 390],
	"kho": [1512, 1280, 390, 360],
	"tra-doi": [1512, 1280, 390],
	"doi-soat-cod": [1512, 1280, 390],
	"tien-tren-don": [1512, 1280, 390],
	// Trang so sánh chụp sau cùng vì nó nhúng ảnh của các trang trên.
	"index": [1512, 390],
};
// (8) Khung kế hoạch đòi (Task 131 Interfaces, ca V của Task 132–138 trong bảng ca M3): mỗi khóa phải có một phần tử
// [data-ca~="khóa"]. Khóa "vcNN", "tiNN", "tdNN", "kh09", "dh03", "dh15" là ca của spec 11; các khóa khác là phần Interfaces
// liệt kê và dữ liệu khó của luật chung (tên 60 ký tự, 0 / 1 / 200 dòng, tiền 0 và 9 chữ số).
const CASES = {
	"ket-noi-van-chuyen": ["vc01", "vc15", "ds-tai-khoan", "tt-hoat-dong", "tt-cap-quyen-lai", "tt-tam-ngung", "tt-thu-hoi",
		"tt-dang-kiem", "tt-nhap", "moi-truong", "kho", "mac-dinh", "hien-tren-web", "lan-kiem", "them-tai-khoan", "chon-hang",
		"o-khoa", "thay-khoa", "kiem-tra", "webhook", "chi-doc", "dia-gioi-dong-bo", "dia-gioi-chua-khop", "dia-chi-lay-hang",
		"cai-dat", "nguong-don-ket", "nguong-goi-lai", "nguong-hay-hoan", "nguong-qua-han-tien", "nguong-giu-hang", "nhap-tay",
		"ten-60", "0-tai-khoan"],
	"don-van-chuyen": ["vc02", "vc03", "vc05", "vc06", "vc11", "vc12", "vc14", "vc16", "vc18", "td03", "dh15", "ti01", "so-phi",
		"so-phi-loi", "so-phi-dang-hoi", "so-phi-het-han", "can-nang", "khai-gia", "cho-xem", "phi-tu-choi", "ghi-chu-hang",
		"gui-hang", "goi-y-hay-hoan", "dang-gui", "khong-ro", "chua-gui", "tu-choi", "da-gui", "ma-van-don", "nhan-trang-thai",
		"cap-nhat-cuoi", "dong-bo-lai", "in-nhan", "huy-doi-hang", "dang-yeu-cau-huy", "doi-dia-chi", "cap-nhat-tay",
		"dong-thoi-gian", "nguon", "su-kien-bo-qua", "cod-4-ngan", "van-don-ket", "ten-60", "tien-9-so", "tien-0"],
	"the-khach": ["dh03", "nhan-hoan", "muc-khong", "muc-co-hoan", "muc-hay-hoan", "goi-y", "len-don", "khoi-van-chuyen",
		"chua-co-don", "ten-60"],
	"van-hanh": ["vc07", "vc16", "vc18", "kh09", "goi-lai", "qua-han-dau", "ghi-ket-qua", "hen-goi-lai", "giao-lai",
		"doi-dia-chi", "nhan-viec", "hien-so", "don-ket", "can-xem", "theo-loai", "cau-huong-dan", "nut-xu-ly", "giu-hang",
		"khong-nha-hang", "so-dem-menu", "0-dong", "1-dong", "200-dong", "ten-60"],
	"kho": ["vc04", "td02", "td04", "chon-nhieu", "in-nhan-lo", "tu-giao-khong-nhan", "phieu-nhat", "trang-in", "dong-goi",
		"da-o-buoc-sau", "nhan-hoan", "tot-hong-thieu", "nut-khoa-chua-du", "nhan-khach-tra", "0-dong", "200-dong"],
	"tra-doi": ["td05", "td06", "td08", "td09", "phieu-tra", "so-con-tra", "duyet", "huy-phieu", "doi-hang", "mon-tra",
		"mon-moi", "can-tru", "cod-don-moi", "giao-mot-phan", "mon-nhan", "tien-thuc-thu", "ten-60"],
	"doi-soat-cod": ["ti02", "ti04", "ti05", "ti06", "ti07", "nhap-bang-ke", "chon-tai-khoan", "tai-tep", "dinh-dang",
		"tep-trung", "dang-phan-tich", "loi-cot", "nhom-khop", "nhom-lech", "nhom-ngoai", "nhom-trung", "nhom-chi-phi",
		"tong-tien", "chot", "nguoi-khac-chot", "lech-doi-soat", "qua-han", "tien-9-so", "0-dong", "200-dong"],
	"tien-tren-don": ["ti08", "ti09", "td10", "phieu-thu", "coc", "da-gui-hang-khong-doi", "nghia-vu-hoan", "chi-hoan",
		"mien-hoan", "dieu-chinh-cod", "xem-truoc", "canh-bao-am", "dao-thanh-cong", "bi-chan", "xem-truoc-cu", "tien-9-so"],
	"index": [],
};
const HEIGHT = { 1512: 767, 1280: 800, 390: 844, 360: 800 };

// Tập có sẵn của DESIGN.md (§1 màu kèm ba sắc chữ C+, §2 cỡ chữ, §3 bo góc). Không thêm giá trị ở đây khi chưa có OK.
const HEX = ["#ffffff", "#f5f2ef", "#e8e2dc", "#2b2624", "#6e6560", "#58504d", "#c0392b", "#f9ebe7", "#fcf2ef",
	"#8f857f", "#3e6252", "#d8cfc8", "#1a1614", "#4a423e"];
const COLORS = new Set(HEX.map((h) => `rgb(${parseInt(h.slice(1, 3), 16)}, ${parseInt(h.slice(3, 5), 16)}, ${parseInt(h.slice(5, 7), 16)})`));
const FONT_SIZES = new Set(["12px", "13px", "14px", "15px", "16px", "18px", "26px", "34px"]);
const RADII = new Set(["0px", "2px", "8px", "14px", "999px", "50%"]);

function staticChecks() {
	const errs = [];
	for (const f of ["../m1/m1.css", "../m2/m2.css", "m3.css"]) {
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
		// (10) Ô khóa API (.secret): ô mật khẩu, không bao giờ có giá trị (không hiện lại khóa đã lưu).
		for (const el of document.querySelectorAll("input.secret")) {
			if (el.type !== "password") errs.push(`ô khóa API không phải ô mật khẩu: ${where(el)}`);
			if (el.getAttribute("value") || el.value) errs.push(`ô khóa API có giá trị: ${where(el)}`);
			if (el.getAttribute("autocomplete") !== "new-password") errs.push(`ô khóa API thiếu autocomplete="new-password": ${where(el)}`);
		}
		if (name === "ket-noi-van-chuyen" && !document.querySelector("input.secret")) errs.push("thiếu ô khóa API (input.secret)");
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
