// Khung điều hướng chung của mockup M5 đợt 1 (Task M5-22, số toàn cục 205): cùng cấu trúc với docs/mockups/m3/khung.js
// (menu trái từ 1000px, thanh trên 56px dưới 1000px). Menu theo mục "Xếp module trên CRM" của kế hoạch M5 (Task 450):
// Đơn hàng thêm "Yêu cầu về đơn hàng" (số đếm yêu cầu đang mở thay màn Tổng quan của admin, QĐ37); Khách hàng thêm "Góp ý";
// Khuyến mãi thêm "Chiến dịch giảm giá", "Quà sinh nhật" (Flash sale là đợt 2, chưa có mục); nhóm mới "Website" giữa
// Khuyến mãi và Sản phẩm; Quản trị thêm "Nhật ký thao tác". Mục của M1–M3 trỏ sang mockup của mốc đó.
// Trang đặt <body data-page="..." data-title="..."> và có <div class="app"><main class="main">…</main></div>.
// Trang có <div id="drawer"></div> thì khung chép menu vào đó (mockup menu mở trên điện thoại, chỉ hiện dưới 1000px).
// Menu chỉ hiện mục người dùng có quyền; mockup vẽ với tài khoản Chủ.
(function () {
	// [đường dẫn, tên, trang mockup làm sáng mục này, số đếm (tùy chọn), số quá hạn (tùy chọn), khóa data-ca (tùy chọn)]
	const groups = [
		["Đơn hàng", [["../m2/len-don.html", "Lên đơn"], ["../m2/don-danh-sach.html", "Danh sách đơn"], ["../m2/don-trung.html", "Đơn trùng"], ["order-requests.html", "Yêu cầu về đơn hàng", ["order-requests"], 8, 0, "yeu-cau so-dem-menu"], ["../m2/cai-dat-don.html", "Cài đặt đơn"]]],
		["Vận chuyển", [["../m3/van-hanh.html#goi-lai", "Cần gọi lại", [], 12, 3], ["../m3/van-hanh.html#don-ket", "Đơn kẹt", [], 4], ["../m3/van-hanh.html#can-xem", "Cần xem", [], 6], ["../m3/van-hanh.html#giu-hang", "Giữ hàng quá hạn"], ["../m3/ket-noi-van-chuyen.html", "Kết nối vận chuyển"], ["../m3/ket-noi-van-chuyen.html#cai-dat", "Cài đặt vận chuyển"]]],
		["Tiền", [["../m3/doi-soat-cod.html", "Đối soát COD"], ["../m3/doi-soat-cod.html#lech", "Lệch đối soát", [], 3], ["../m3/doi-soat-cod.html#qua-han", "Quá hạn chưa có tiền", [], 2], ["../m3/tien-tren-don.html#nghia-vu", "Nghĩa vụ hoàn", [], 2]]],
		["Khách hàng", [["../m2/khach-danh-sach.html", "Khách hàng"], ["feedback.html", "Góp ý", ["feedback"], undefined, 0, "gop-y"]]],
		["Khuyến mãi", [["../m2/ma-giam-gia.html", "Mã giảm giá"], ["web-campaigns.html", "Chiến dịch giảm giá", ["web-campaigns"], undefined, 0, "chien-dich"], ["birthday-gifts.html", "Quà sinh nhật", ["birthday-gifts"], undefined, 0, "qua-sinh-nhat"]]],
		["Website", [["web-banners.html", "Banner trang chủ", ["web-banners"], undefined, 0, "banner"], ["web-looks.html", "Bộ phối", ["web-looks"], undefined, 0, "bo-phoi"], ["web-carriers.html", "Hãng trên web", ["web-carriers"], undefined, 0, "hang-tren-web"], ["web-intake.html", "Nhận đơn web", ["web-intake"], undefined, 0, "nhan-don-web"], ["service-keys.html", "Khóa dịch vụ", ["service-keys"], undefined, 0, "khoa-dich-vu"]]],
		["Sản phẩm", [["../m1/san-pham-danh-sach.html", "Danh sách sản phẩm"], ["../m1/nhap-excel.html", "Nhập Excel"]]],
		["Kho", [["../m1/kho-ton.html", "Tồn kho"], ["../m3/kho.html", "Đóng gói"], ["../m3/kho.html#nhan-hoan", "Nhận hàng hoàn"], ["../m1/kho-phieu.html", "Phiếu kho"], ["../m1/kho-kiem-kho.html", "Kiểm kho"], ["../m1/kho-nha-cung-cap.html", "Nhà cung cấp"]]],
		["Hệ thống", [["../m1/suc-khoe.html", "Sức khỏe hệ thống"], ["../m1/su-kien-loi.html", "Sự kiện lỗi"], ["../m1/tep-bi-mat.html", "Tệp bị mất"]]],
		["Quản trị", [["../m1/nhan-vien.html", "Nhân viên"], ["../m1/vai-tro.html", "Vai trò"], ["audit.html", "Nhật ký thao tác", ["audit"], undefined, 0, "nhat-ky"]]],
	];
	const page = document.body.dataset.page;
	const app = document.querySelector(".app");
	let nav = "";
	for (const [g, items] of groups) {
		nav += `<div class="nav-g"${g === "Website" ? ' data-ca="nhom-website"' : ""}><p>${g}</p>`;
		for (const [href, label, pages = [], n, late, ca] of items) {
			const cur = pages.includes(page) ? ' aria-current="page"' : "";
			let badge = "";
			if (n !== undefined) {
				const lateTxt = late ? `, ${late} quá hạn` : "";
				badge = `<span class="nav-n${late ? " is-late" : ""}" aria-label="${n} việc${lateTxt}">${n}</span>`;
			}
			const dca = ca ? ` data-ca="${ca}"` : "";
			nav += `<a class="nav-a" href="${href}"${cur}${dca}><span class="grow">${label}</span>${badge}</a>`;
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
	const drawer = document.getElementById("drawer");
	if (drawer) drawer.innerHTML = `<nav aria-label="Menu chính (điện thoại)" class="drawer-nav">${nav.replace(/ data-ca="[^"]*"/g, "")}</nav>`;
})();
