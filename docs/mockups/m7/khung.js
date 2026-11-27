// Khung điều hướng chung của mockup M7 (Task M7-02, số toàn cục 247): chép từ docs/mockups/m5/khung.js (menu trái từ
// 1000px, thanh trên 56px dưới 1000px). Nhóm menu theo console/src/app/nav.ts trên main 28/11: Hộp chat (M4) đứng đầu,
// Báo cáo (M6) giữa Kho và Hệ thống. M7 chỉ thêm một mục: "Hàng hoàn đơn cũ" ở nhóm Kho, ngay sau "Nhận hàng hoàn" (HV02 của
// README). Kiểm kho mở sổ không có mục riêng (việc làm một lần mỗi kho): vào từ màn Kiểm kho, làm sáng mục "Kiểm kho" (HV01).
// Mục của M1–M5 trỏ sang mockup của mốc đó. Menu chỉ hiện mục người dùng có quyền; mockup vẽ với tài khoản nhân viên kho
// (vai trò mẫu warehouse, giao Kho chính).
// Trang đặt <body data-page="..." data-title="..."> và có <div class="app"><main class="main">…</main></div>.
(function () {
	// [đường dẫn, tên, trang mockup làm sáng mục này, khóa data-ca (tùy chọn)]
	const groups = [
		["Hộp chat", [["../m4/inbox.html", "Hộp chat"], ["../m4/live.html", "Live"]]],
		["Đơn hàng", [["../m2/len-don.html", "Lên đơn"], ["../m2/don-danh-sach.html", "Danh sách đơn"], ["../m5/order-requests.html", "Yêu cầu về đơn hàng"]]],
		["Vận chuyển", [["../m3/van-hanh.html", "Vận hành giao hàng"], ["../m3/ket-noi-van-chuyen.html", "Kết nối vận chuyển"]]],
		["Tiền", [["../m3/doi-soat-cod.html", "Đối soát COD"]]],
		["Khách hàng", [["../m2/khach-danh-sach.html", "Khách hàng"], ["../m5/feedback.html", "Góp ý"]]],
		["Khuyến mãi", [["../m2/ma-giam-gia.html", "Mã giảm giá"], ["../m5/flash-sales.html", "Flash sale"]]],
		["Website", [["../m5/web-banners.html", "Banner trang chủ"], ["../m5/web-intake.html", "Nhận đơn web"]]],
		["Sản phẩm", [["../m1/san-pham-danh-sach.html", "Danh sách sản phẩm"], ["../m1/nhap-excel.html", "Nhập Excel"]]],
		["Kho", [["../m1/kho-ton.html", "Tồn kho"], ["../m3/kho.html", "Đóng gói"], ["../m3/kho.html#nhan-hoan", "Nhận hàng hoàn"],
			["hang-hoan-don-cu.html", "Hàng hoàn đơn cũ", ["hang-hoan-don-cu"], "muc-hang-hoan-cu"], ["../m1/kho-phieu.html", "Phiếu kho"],
			["../m1/kho-kiem-kho.html", "Kiểm kho", ["kiem-kho-mo-so"]], ["../m1/kho-nha-cung-cap.html", "Nhà cung cấp"]]],
		["Báo cáo", [["../m1/kho-ton.html", "Báo cáo"]]],
		["Hệ thống", [["../m1/suc-khoe.html", "Sức khỏe hệ thống"]]],
		["Quản trị", [["../m1/nhan-vien.html", "Nhân viên"], ["../m1/vai-tro.html", "Vai trò"]]],
	];
	const page = document.body.dataset.page;
	const app = document.querySelector(".app");
	let nav = "";
	for (const [g, items] of groups) {
		nav += `<div class="nav-g"><p>${g}</p>`;
		for (const [href, label, pages = [], ca] of items) {
			const cur = pages.includes(page) ? ' aria-current="page"' : "";
			const dca = ca ? ` data-ca="${ca}"` : "";
			nav += `<a class="nav-a" href="${href}"${cur}${dca}><span class="grow">${label}</span></a>`;
		}
		nav += "</div>";
	}
	const side = document.createElement("aside");
	side.className = "side";
	side.innerHTML = `<div class="brand"><span class="wordmark">ELIMAZ</span><span class="xs">CRM</span></div><nav aria-label="Menu chính">${nav}</nav><div class="side-user"><b>Trần Văn Hùng</b><br><span class="muted">Kho · Kho chính</span></div>`;
	const top = document.createElement("header");
	top.className = "top";
	top.innerHTML = `<button class="ibtn" aria-label="Mở menu">☰</button><span class="t ellip">${document.body.dataset.title || ""}</span><span class="wordmark xs">ELIMAZ</span><span class="ibtn" aria-label="Tài khoản" role="button" tabindex="0">H</span>`;
	app.prepend(side);
	app.querySelector(".main").before(top);
})();
