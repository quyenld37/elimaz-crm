// Khung điều hướng chung của mockup M2: cùng cấu trúc với docs/mockups/m1/khung.js (menu trái từ 1000px, thanh trên
// 56px dưới 1000px), thêm ba nhóm của M2 đứng đầu menu: Đơn hàng, Khách hàng, Khuyến mãi. Mục của M1 trỏ sang mockup M1.
// Trang đặt <body data-page="..." data-title="..."> và có <div class="app"><main class="main">…</main></div>.
// Menu chỉ hiện mục người dùng có quyền; mockup vẽ với tài khoản Chủ.
(function () {
	const groups = [
		["Đơn hàng", [["len-don", "Lên đơn"], ["don-danh-sach", "Danh sách đơn", ["don-chi-tiet"]], ["don-trung", "Đơn trùng"], ["cai-dat-don", "Cài đặt đơn"]]],
		["Khách hàng", [["khach-danh-sach", "Khách hàng", ["khach-ho-so", "khach-gop"]]]],
		["Khuyến mãi", [["ma-giam-gia", "Mã giảm giá"]]],
		["Sản phẩm", [["../m1/san-pham-danh-sach", "Danh sách sản phẩm"], ["../m1/nhap-excel", "Nhập Excel"]]],
		["Kho", [["../m1/kho-ton", "Tồn kho"], ["../m1/kho-phieu", "Phiếu kho"], ["../m1/kho-kiem-kho", "Kiểm kho"], ["../m1/kho-nha-cung-cap", "Nhà cung cấp"]]],
		["Hệ thống", [["../m1/suc-khoe", "Sức khỏe hệ thống"], ["../m1/su-kien-loi", "Sự kiện lỗi"], ["../m1/tep-bi-mat", "Tệp bị mất"]]],
		["Quản trị", [["../m1/nhan-vien", "Nhân viên"], ["../m1/vai-tro", "Vai trò"]]],
	];
	const page = document.body.dataset.page;
	const app = document.querySelector(".app");
	const side = document.createElement("aside");
	side.className = "side";
	let html = '<div class="brand"><span class="wordmark">ELIMAZ</span><span class="xs">CRM</span></div><nav aria-label="Menu chính">';
	for (const [g, items] of groups) {
		html += `<div class="nav-g"><p>${g}</p>`;
		for (const [key, label, also = []] of items) {
			const cur = key === page || also.includes(page) ? ' aria-current="page"' : "";
			html += `<a class="nav-a" href="${key}.html"${cur}>${label}</a>`;
		}
		html += "</div>";
	}
	html += '</nav><div class="side-user"><b>Nguyễn Thị Lan</b><br><span class="muted">Chủ · Toàn shop</span></div>';
	side.innerHTML = html;
	const top = document.createElement("header");
	top.className = "top";
	top.innerHTML = `<button class="ibtn" aria-label="Mở menu">☰</button><span class="t ellip">${document.body.dataset.title || ""}</span><span class="wordmark xs">ELIMAZ</span><span class="ibtn" aria-label="Tài khoản" role="button" tabindex="0">L</span>`;
	app.prepend(side);
	app.querySelector(".main").before(top);
})();
