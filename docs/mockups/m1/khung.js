// Khung điều hướng chung của mockup M1: menu trái (từ 1000px) và thanh trên 56px (điện thoại, máy tính bảng).
// Trang đặt <body data-page="..." data-title="..."> và có <div class="app"><main class="main">…</main></div>.
// Menu chỉ hiện mục người dùng có quyền (Task M1-53); mockup vẽ với tài khoản Chủ.
(function () {
	const groups = [
		["Sản phẩm", [["san-pham-danh-sach", "Danh sách sản phẩm", ["san-pham-sua"]], ["nhap-excel", "Nhập Excel"]]],
		["Kho", [["kho-ton", "Tồn kho"], ["kho-phieu", "Phiếu kho"], ["kho-kiem-kho", "Kiểm kho"], ["kho-nha-cung-cap", "Nhà cung cấp"]]],
		["Hệ thống", [["suc-khoe", "Sức khỏe hệ thống"], ["su-kien-loi", "Sự kiện lỗi"], ["tep-bi-mat", "Tệp bị mất"]]],
		["Quản trị", [["nhan-vien", "Nhân viên"], ["vai-tro", "Vai trò"]]],
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
