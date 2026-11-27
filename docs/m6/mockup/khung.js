// Khung điều hướng chung của mockup M6 (Task M6-01, số toàn cục 224). Chép từ docs/mockups/m5/khung.js (Task 205/514):
// cùng cấu trúc (menu trái từ 1000px, thanh trên 56px dưới 1000px). Menu theo console/src/app/nav.ts trên main (nhóm
// "Hộp chat" của M4 đứng đầu) cộng nhóm mới "Báo cáo" của M6 giữa "Kho" và "Hệ thống": Tổng quan, Báo cáo đơn, Hãng vận
// chuyển, Bán chạy, Tồn và xuất nhập tồn, Tệp xuất. Mục của M1–M5 trỏ sang mockup của mốc đó.
// Trang đặt <body data-page="..." data-title="..."> và có <div class="app"><main class="main">…</main></div>.
// Menu chỉ hiện mục người dùng có quyền; mockup vẽ với tài khoản Chủ (người chỉ có quyền một Page vẽ trong trang, ở
// trạng thái riêng).
(function () {
	const m = "../../mockups/";
	// [đường dẫn, tên, trang mockup làm sáng mục này, số đếm (tùy chọn), số quá hạn (tùy chọn)]
	const groups = [
		["Hộp chat", [[m + "m4/inbox.html", "Hộp chat"], [m + "m4/live.html", "Live"], [m + "m4/thieu-dia-chi.html", "Đã chốt, thiếu địa chỉ"], [m + "m4/ket-noi-page.html", "Kênh kết nối"], [m + "m4/cai-dat.html", "Cài đặt hộp chat"]]],
		["Đơn hàng", [[m + "m2/len-don.html", "Lên đơn"], [m + "m2/don-danh-sach.html", "Danh sách đơn"], [m + "m2/don-trung.html", "Đơn trùng"], [m + "m5/order-requests.html", "Yêu cầu về đơn hàng", [], 8], [m + "m2/cai-dat-don.html", "Cài đặt đơn"]]],
		["Vận chuyển", [[m + "m3/van-hanh.html#goi-lai", "Cần gọi lại", [], 12, 3], [m + "m3/van-hanh.html#don-ket", "Đơn kẹt", [], 4], [m + "m3/van-hanh.html#can-xem", "Cần xem", [], 6], [m + "m3/van-hanh.html#giu-hang", "Giữ hàng quá hạn"], [m + "m3/ket-noi-van-chuyen.html", "Kết nối vận chuyển"], [m + "m3/ket-noi-van-chuyen.html#cai-dat", "Cài đặt vận chuyển"]]],
		["Tiền", [[m + "m3/doi-soat-cod.html", "Đối soát COD"], [m + "m3/doi-soat-cod.html#lech", "Lệch đối soát", [], 3], [m + "m3/doi-soat-cod.html#qua-han", "Quá hạn chưa có tiền", [], 2], [m + "m3/tien-tren-don.html#nghia-vu", "Nghĩa vụ hoàn", [], 2]]],
		["Khách hàng", [[m + "m2/khach-danh-sach.html", "Khách hàng", ["nut-xuat"]], [m + "m5/feedback.html", "Góp ý"]]],
		["Khuyến mãi", [[m + "m2/ma-giam-gia.html", "Mã giảm giá"], [m + "m5/web-campaigns.html", "Chiến dịch giảm giá"], [m + "m5/birthday-gifts.html", "Quà sinh nhật"], [m + "m5/flash-sales.html", "Flash sale"]]],
		["Website", [[m + "m5/web-banners.html", "Banner trang chủ"], [m + "m5/web-looks.html", "Bộ phối"], [m + "m5/web-carriers.html", "Hãng trên web"], [m + "m5/web-intake.html", "Nhận đơn web"], [m + "m5/service-keys.html", "Khóa dịch vụ"]]],
		["Sản phẩm", [[m + "m1/san-pham-danh-sach.html", "Danh sách sản phẩm"], [m + "m1/nhap-excel.html", "Nhập Excel"]]],
		["Kho", [[m + "m1/kho-ton.html", "Tồn kho"], [m + "m3/kho.html", "Đóng gói"], [m + "m3/kho.html#nhan-hoan", "Nhận hàng hoàn"], [m + "m1/kho-phieu.html", "Phiếu kho"], [m + "m1/kho-kiem-kho.html", "Kiểm kho"], [m + "m1/kho-nha-cung-cap.html", "Nhà cung cấp"]]],
		["Báo cáo", [["tong-quan.html", "Tổng quan", ["tong-quan"]], ["don.html", "Báo cáo đơn", ["don"]], ["van-chuyen.html", "Hãng vận chuyển", ["van-chuyen"]], ["ban-chay.html", "Bán chạy", ["ban-chay"]], ["ton-kho.html", "Tồn và xuất nhập tồn", ["ton-kho"]], ["tep-xuat.html", "Tệp xuất", ["tep-xuat"]]]],
		["Hệ thống", [[m + "m1/suc-khoe.html", "Sức khỏe hệ thống"], [m + "m1/su-kien-loi.html", "Sự kiện lỗi"], [m + "m1/tep-bi-mat.html", "Tệp bị mất"]]],
		["Quản trị", [[m + "m1/nhan-vien.html", "Nhân viên"], [m + "m1/vai-tro.html", "Vai trò"], [m + "m5/audit.html", "Nhật ký thao tác"]]],
	];
	const page = document.body.dataset.page;
	const app = document.querySelector(".app");
	let nav = "";
	for (const [g, items] of groups) {
		nav += `<div class="nav-g"><p>${g}</p>`;
		for (const [href, label, pages = [], n, late] of items) {
			const cur = pages.includes(page) ? ' aria-current="page"' : "";
			let badge = "";
			if (n !== undefined) {
				const lateTxt = late ? `, ${late} quá hạn` : "";
				badge = `<span class="nav-n${late ? " is-late" : ""}" aria-label="${n} việc${lateTxt}">${n}</span>`;
			}
			nav += `<a class="nav-a" href="${href}"${cur}><span class="grow">${label}</span>${badge}</a>`;
		}
		nav += "</div>";
	}
	const side = document.createElement("aside");
	side.className = "side";
	side.innerHTML = `<div class="brand"><span class="wordmark">ELIMAZ</span><span class="xs">CRM</span></div><nav aria-label="Menu chính">${nav}</nav><div class="side-user"><b>Nguyễn Thị Lan</b><br><span class="muted">Chủ · Toàn shop</span></div>`;
	const top = document.createElement("header");
	top.className = "top";
	top.innerHTML = `<button class="ibtn" aria-label="Mở menu">☰</button><span class="t ellip">${document.body.dataset.title || ""}</span><span class="wordmark xs">ELIMAZ</span><span class="ibtn" aria-label="Tài khoản" role="button" tabindex="0">L</span>`;
	app.prepend(side);
	app.querySelector(".main").before(top);
})();
