// Khung điều hướng chung của mockup M3 (Task M3-40, số toàn cục 131): cùng cấu trúc với docs/mockups/m2/khung.js và
// docs/mockups/m4/khung.js (menu trái từ 1000px, thanh trên 56px dưới 1000px). M3 thêm hai nhóm "Vận chuyển" (bốn danh sách
// vận hành, kết nối và cài đặt vận chuyển) và "Tiền" (đối soát COD, lệch, quá hạn, nghĩa vụ hoàn) ngay sau "Đơn hàng", và
// hai mục kho "Đóng gói", "Nhận hàng hoàn" (đề xuất HV01, HV02). Mục của M2, M1 trỏ sang mockup của mốc đó.
// Mục có số đếm (Cần gọi lại, Cần xem) lấy từ counts của listShipmentsForReview (cập nhật theo báo hiệu, HV02).
// Trang đặt <body data-page="..." data-title="..."> và có <div class="app"><main class="main">…</main></div>.
// Menu chỉ hiện mục người dùng có quyền; mockup vẽ với tài khoản Chủ.
(function () {
	// [đường dẫn, tên, trang mockup làm sáng mục này, số đếm (tùy chọn), số quá hạn (tùy chọn)]
	const groups = [
		["Đơn hàng", [["../m2/len-don.html", "Lên đơn", ["the-khach"]], ["../m2/don-danh-sach.html", "Danh sách đơn", ["don-van-chuyen", "tra-doi", "tien-tren-don"]], ["../m2/don-trung.html", "Đơn trùng"], ["../m2/cai-dat-don.html", "Cài đặt đơn"]]],
		["Vận chuyển", [["van-hanh.html#goi-lai", "Cần gọi lại", ["van-hanh"], 12, 3], ["van-hanh.html#don-ket", "Đơn kẹt", [], 4], ["van-hanh.html#can-xem", "Cần xem", [], 6], ["van-hanh.html#giu-hang", "Giữ hàng quá hạn", []], ["ket-noi-van-chuyen.html", "Kết nối vận chuyển", ["ket-noi-van-chuyen"]], ["ket-noi-van-chuyen.html#cai-dat", "Cài đặt vận chuyển", []]]],
		["Tiền", [["doi-soat-cod.html", "Đối soát COD", ["doi-soat-cod"]], ["doi-soat-cod.html#lech", "Lệch đối soát", [], 3], ["doi-soat-cod.html#qua-han", "Quá hạn chưa có tiền", [], 2], ["tien-tren-don.html#nghia-vu", "Nghĩa vụ hoàn", [], 2]]],
		["Khách hàng", [["../m2/khach-danh-sach.html", "Khách hàng"]]],
		["Khuyến mãi", [["../m2/ma-giam-gia.html", "Mã giảm giá"]]],
		["Sản phẩm", [["../m1/san-pham-danh-sach.html", "Danh sách sản phẩm"], ["../m1/nhap-excel.html", "Nhập Excel"]]],
		["Kho", [["../m1/kho-ton.html", "Tồn kho"], ["kho.html", "Đóng gói", ["kho"]], ["kho.html#nhan-hoan", "Nhận hàng hoàn", []], ["../m1/kho-phieu.html", "Phiếu kho"], ["../m1/kho-kiem-kho.html", "Kiểm kho"], ["../m1/kho-nha-cung-cap.html", "Nhà cung cấp"]]],
		["Hệ thống", [["../m1/suc-khoe.html", "Sức khỏe hệ thống"], ["../m1/su-kien-loi.html", "Sự kiện lỗi"], ["../m1/tep-bi-mat.html", "Tệp bị mất"]]],
		["Quản trị", [["../m1/nhan-vien.html", "Nhân viên"], ["../m1/vai-tro.html", "Vai trò"]]],
	];
	const page = document.body.dataset.page;
	const app = document.querySelector(".app");
	const side = document.createElement("aside");
	side.className = "side";
	let html = '<div class="brand"><span class="wordmark">ELIMAZ</span><span class="xs">CRM</span></div><nav aria-label="Menu chính" data-ca="so-dem-menu">';
	for (const [g, items] of groups) {
		html += `<div class="nav-g"><p>${g}</p>`;
		for (const [href, label, pages = [], n, late] of items) {
			const cur = pages.includes(page) ? ' aria-current="page"' : "";
			let badge = "";
			if (n !== undefined) {
				const lateTxt = late ? `, ${late} quá hạn` : "";
				badge = `<span class="nav-n${late ? " is-late" : ""}" aria-label="${n} việc${lateTxt}">${n}</span>`;
			}
			html += `<a class="nav-a" href="${href}"${cur}><span class="grow">${label}</span>${badge}</a>`;
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
