// Luật hiện của màn Nhật ký thao tác (kế hoạch M5 Task 462; mockup docs/mockups/m5/audit.html; HV24): nhãn tiếng Việt
// của mã hành động, loại thực thể, loại người làm, tên trường đổi mà máy chủ ghi (mã lấy từ các chỗ gọi audit trong
// internal/); mã chưa có nhãn hiện nguyên mã. Khoảng ngày VN → mốc giờ [from, to) của listAuditLogs.
import type { AuditActor, AuditLog } from "../../api";

/** Nhãn của mã hành động; cũng là danh sách của ô lọc "Thao tác" (API lọc đúng một mã). */
export const ACTION_TEXT: Readonly<Record<string, string>> = {
  "order.web_intake.set": "Tạm dừng / mở lại nhận đơn web",
  "order.create": "Tạo đơn",
  "order.update": "Sửa đơn",
  "order.confirm": "Xác nhận đơn",
  "order.cancel": "Hủy đơn",
  "order.packing": "Chuyển đơn sang đóng gói",
  "order.add_note": "Ghi chú đơn",
  "order.duplicate.resolve": "Xử lý đơn trùng",
  "order.approve_discount": "Duyệt giảm giá đơn",
  "order.settings.update": "Sửa cài đặt đơn",
  "order.traffic_source.create": "Tạo nguồn đơn",
  "order.traffic_source.update": "Sửa nguồn đơn",
  "order.request.create": "Tạo yêu cầu về đơn hàng",
  "order.request.status": "Đổi trạng thái yêu cầu",
  "order.request.reply": "Trả lời yêu cầu",
  "order.request.customer_reply": "Khách trả lời yêu cầu",
  "order.request.customer_cancel": "Khách hủy yêu cầu",
  "order.return.create": "Tạo đơn trả",
  "order.return.receive": "Nhận hàng trả",
  "order.return.close": "Đóng đơn trả",
  "order.return.cancel": "Hủy đơn trả",
  "order.address_change": "Đổi địa chỉ giao",
  "fulfillment.shipment.create": "Tạo vận đơn",
  "fulfillment.shipment.cancel": "Hủy vận đơn",
  "fulfillment.shipment.manual_status": "Cập nhật trạng thái vận đơn tay",
  "fulfillment.shipment.partial_delivery": "Giao một phần",
  "fulfillment.shipment.resend": "Gửi lại vận đơn",
  "fulfillment.shipment.resolve_create": "Xử lý vận đơn chưa rõ",
  "fulfillment.shipment.sync": "Đồng bộ vận đơn",
  "fulfillment.label_batch.create": "In nhãn hàng loạt",
  "fulfillment.address_change.request": "Yêu cầu đổi địa chỉ",
  "fulfillment.address_change.resolve": "Xử lý đổi địa chỉ",
  "fulfillment.anomaly.resolve": "Xử lý bất thường vận đơn",
  "fulfillment.web_carrier_settings.update": "Sửa hãng trên web",
  "fulfillment.web_carrier_order.update": "Đổi thứ tự hãng trên web",
  "cash.payment.record": "Ghi nhận thanh toán",
  "cash.refund.pay": "Hoàn tiền",
  "cash.refund.waive": "Bỏ khoản hoàn",
  "cash.statement.import": "Nhập bảng đối soát COD",
  "cash.statement.confirm": "Xác nhận bảng đối soát COD",
  "cash.cod.adjust": "Điều chỉnh COD",
  "customer.create": "Tạo khách",
  "customer.update": "Sửa khách",
  "customer.merge": "Gộp khách",
  "customer.merge_undo": "Bỏ gộp khách",
  "customer.note_add": "Ghi chú khách",
  "customer.tag_add": "Gắn nhãn khách",
  "customer.tag_remove": "Bỏ nhãn khách",
  "customer.feedback.submit": "Nhận góp ý",
  "customer.feedback.update": "Xử lý góp ý",
  "customer.birthday.register": "Đăng ký sinh nhật",
  "customer.birthday.stop": "Dừng nhận quà sinh nhật",
  "promotion.coupon.create": "Tạo mã giảm giá",
  "promotion.coupon.update": "Sửa mã giảm giá",
  "promotion.coupon_batch.create": "Tạo đợt mã",
  "promotion.web_campaign.create": "Tạo chiến dịch",
  "promotion.web_campaign.save": "Sửa chiến dịch",
  "promotion.web_campaign.state": "Bật / tạm dừng chiến dịch",
  "promotion.web_campaign.delete": "Xóa chiến dịch",
  "promotion.birthday.settings": "Sửa cài đặt quà sinh nhật",
  "promotion.birthday.run": "Tạo mã quà sinh nhật",
  "promotion.birthday.messaged": "Đánh dấu đã nhắn quà sinh nhật",
  "promotion.birthday.export": "Xuất danh sách sinh nhật",
  "content.banners.save": "Lưu banner trang chủ",
  "content.look.save": "Lưu bộ phối",
  "catalog.product.create": "Tạo sản phẩm",
  "catalog.product.update": "Sửa sản phẩm",
  "catalog.variant.create": "Thêm biến thể",
  "catalog.variant.update": "Sửa biến thể",
  "catalog.import.apply": "Nhập sản phẩm từ tệp",
  "inventory.document.create": "Tạo phiếu kho",
  "inventory.document.lines": "Sửa dòng phiếu kho",
  "inventory.document.post": "Ghi sổ phiếu kho",
  "inventory.document.cancel": "Hủy phiếu kho",
  "inventory.stocktake.start": "Bắt đầu kiểm kho",
  "inventory.stocktake.count": "Đếm hàng kiểm kho",
  "inventory.stocktake.apply": "Áp kết quả kiểm kho",
  "inventory.stocktake.cancel": "Hủy kiểm kho",
  "inventory.supplier.create": "Tạo nhà cung cấp",
  "inventory.supplier.update": "Sửa nhà cung cấp",
  "identity.user.create": "Tạo nhân viên",
  "identity.user.grants": "Đổi quyền nhân viên",
  "identity.user.lock": "Khóa nhân viên",
  "identity.user.unlock": "Mở khóa nhân viên",
  "identity.role.create": "Tạo vai trò",
  "identity.role.update": "Sửa vai trò",
  "identity.role.archive": "Lưu trữ vai trò",
  "identity.service_key.create": "Tạo khóa dịch vụ",
  "identity.service_key.revoke": "Thu hồi khóa dịch vụ",
  "platform.job.retry": "Chạy lại việc nền",
  "platform.job.abandon": "Bỏ việc nền",
  "platform.files.regenerate": "Tạo lại ảnh",
  "platform.files.verify": "Kiểm tệp",
};

/** Mã hành động của ô lọc, xếp theo nhãn. */
export const ACTION_OPTIONS: readonly string[] = Object.keys(ACTION_TEXT).sort((a, b) =>
  (ACTION_TEXT[a] ?? a).localeCompare(ACTION_TEXT[b] ?? b, "vi"),
);

export const actionText = (code: string) => ACTION_TEXT[code] ?? code;

const ENTITY_TEXT: Readonly<Record<string, string>> = {
  "order.web_intake": "Nhận đơn web",
  order: "Đơn",
  "order.settings": "Cài đặt đơn",
  "order.traffic_source": "Nguồn đơn",
  "order.address_change": "Đổi địa chỉ",
  order_request: "Yêu cầu",
  return: "Đơn trả",
  shipment: "Vận đơn",
  shipment_anomaly: "Bất thường vận đơn",
  label_batch: "Lô nhãn",
  address_change: "Đổi địa chỉ",
  carrier_account: "Tài khoản hãng",
  "fulfillment.web_carrier_settings": "Hãng trên web",
  "cash.payment": "Thanh toán",
  "cash.refund_obligation": "Khoản hoàn",
  "cash.cod_statement": "Bảng đối soát COD",
  "cash.cod_transfer": "Chuyển COD",
  customer: "Khách",
  "customer.feedback": "Góp ý",
  "customer.birthday": "Sinh nhật",
  "promotion.coupon": "Mã giảm giá",
  "promotion.coupon_batch": "Đợt mã",
  "promotion.web_campaign": "Chiến dịch",
  "promotion.birthday_settings": "Cài đặt quà sinh nhật",
  "promotion.birthday_run": "Lượt tạo quà sinh nhật",
  "promotion.birthday_gift": "Quà sinh nhật",
  "promotion.birthday_export": "Quà sinh nhật",
  "content.home_banners": "Banner trang chủ",
  "content.looks": "Bộ phối",
  "inventory.stock_document": "Phiếu kho",
  "inventory.supplier": "Nhà cung cấp",
  "inventory.stocktake": "Kiểm kho",
  warehouse: "Kho",
  "identity.service_key": "Khóa dịch vụ",
  "platform.job": "Việc nền",
  "platform.file_preset": "Cỡ ảnh",
};

/** Thực thể toàn shop: mã thực thể là mã shop, không có nghĩa với người xem — chỉ hiện nhãn. */
const SHOP_WIDE = new Set([
  "order.web_intake",
  "order.settings",
  "content.home_banners",
  "content.looks",
  "fulfillment.web_carrier_settings",
  "promotion.birthday_settings",
]);

/**
 * Cột "Mục": nhãn loại thực thể và tên dễ đọc máy chủ trả (mã YC…, mã phiếu, tên tài khoản hãng, tên khóa; Task 488),
 * không có tên thì mã thực thể (vắng với thực thể toàn shop hay khi không có).
 */
export function entityText(r: Pick<AuditLog, "entityType" | "entityId" | "entityName">): {
  label: string;
  id?: string;
} {
  const label = ENTITY_TEXT[r.entityType] ?? r.entityType;
  if (SHOP_WIDE.has(r.entityType)) return { label };
  if (r.entityName !== undefined && r.entityName !== "") return { label, id: r.entityName };
  if (r.entityId === undefined || r.entityId === "") return { label };
  return { label, id: r.entityId };
}

/** Nhãn một người làm của ô lọc (listAuditActors): nhân viên theo tên, khóa kèm "Khóa:"; đã khóa / thu hồi ghi rõ. */
export function actorOptionText(a: Pick<AuditActor, "actorType" | "name" | "active">): string {
  if (a.actorType === "staff") return a.active ? a.name : `${a.name} (đã khóa)`;
  return a.active ? `Khóa: ${a.name}` : `Khóa: ${a.name} (đã thu hồi)`;
}

const ACTOR_TYPE_TEXT: Readonly<Record<AuditLog["actorType"], string>> = {
  staff: "Nhân viên",
  service: "Khóa dịch vụ",
  system: "Hệ thống",
  carrier: "Hãng vận chuyển",
  customer: "Khách",
};

/** Người làm: tên (hệ thống luôn "Hệ thống"; thiếu tên → mã, rồi "Không rõ") và loại. */
export function actorText(r: Pick<AuditLog, "actorType" | "actorId" | "actorName">): { name: string; kind: string } {
  const kind = ACTOR_TYPE_TEXT[r.actorType];
  if (r.actorType === "system") return { name: "Hệ thống", kind };
  const name = r.actorName ?? r.actorId ?? "Không rõ";
  return { name: name === "" ? "Không rõ" : name, kind };
}

const FIELD_TEXT: Readonly<Record<string, string>> = {
  paused: "tạm dừng",
  reason: "lý do",
  status: "trạng thái",
  state: "trạng thái",
  name: "tên",
  note: "ghi chú",
  version: "phiên bản",
};

/** Trường đổi: nhãn ngắn cho tên thường gặp, còn lại nguyên tên; không có trường → "—". */
export function fieldsText(fields: readonly string[]): string {
  if (fields.length === 0) return "—";
  return fields.map((f) => FIELD_TEXT[f] ?? f).join(", ");
}

const DAY_MS = 86_400_000;

/** Đầu ngày "yyyy-mm-dd" theo giờ VN (UTC+7, không đổi giờ mùa hè) ra mốc ms; chuỗi sai → null. */
function vnDayStart(day: string): number | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return null;
  const ms = Date.parse(`${day}T00:00:00+07:00`);
  return Number.isNaN(ms) ? null : ms;
}

/**
 * Khoảng ngày VN (ô "Từ ngày", "Đến ngày", "" = bỏ trống) → tham số from, to của listAuditLogs: from là 00:00 ngày đầu,
 * to là 00:00 ngày sau ngày cuối (lấy trọn ngày cuối). Ngày đầu sau ngày cuối → error (API trả 400 nếu gửi).
 */
export function dayRange(fromDay: string, toDay: string): { from?: string; to?: string } | { error: string } {
  const from = fromDay === "" ? null : vnDayStart(fromDay);
  const to = toDay === "" ? null : vnDayStart(toDay);
  if ((fromDay !== "" && from === null) || (toDay !== "" && to === null)) return { error: "Ngày không hợp lệ." };
  if (from !== null && to !== null && from > to) return { error: '"Từ ngày" phải trước hoặc bằng "Đến ngày".' };
  return {
    ...(from === null ? {} : { from: new Date(from).toISOString() }),
    ...(to === null ? {} : { to: new Date(to + DAY_MS).toISOString() }),
  };
}
