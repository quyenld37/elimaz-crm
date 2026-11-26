// Lỗi của API trên màn hình (kế hoạch M1 Task 48). Thân lỗi luôn {"error": "<mã>", "request_id": "<id>", ...trường
// thêm như available}; ERROR_MESSAGES có câu tiếng Việt cho mọi mã trong x-error-codes của openapi/openapi.yaml
// (test errors.test.ts đọc tệp đó). Câu chỉ chèn trường số đã biết ({available}), không bao giờ chèn chữ từ máy chủ.

/** Câu khi mã lạ (thường do màn hình cũ hơn API): kèm mã yêu cầu để tra log. */
export const TEXT_UNKNOWN_ERROR = "Có lỗi, chưa làm được. Thử lại sau ít phút";
/** Câu khi yêu cầu không tới được máy chủ (mạng, cổng vào). */
export const TEXT_NETWORK = "Mất kết nối, chưa rõ đã lưu chưa. Bấm lại để gửi lại, không bị trùng";

export const ERROR_MESSAGES: Readonly<Record<string, string>> = {
  // Chung (M0)
  already_processed: "Việc này đã được xử lý rồi",
  bad_origin: "Yêu cầu không đến từ trang CRM. Tải lại trang rồi thử lại",
  bad_request: "Dữ liệu gửi lên không đúng dạng. Tải lại trang rồi thử lại",
  body_too_large: "Dữ liệu gửi lên quá lớn",
  forbidden: "Bạn chưa được cấp quyền làm việc này",
  idempotency_key_invalid: "Yêu cầu thiếu mã chống trùng hợp lệ. Tải lại trang rồi thử lại",
  idempotency_key_required: "Yêu cầu thiếu mã chống trùng. Tải lại trang rồi thử lại",
  idempotency_key_reused: "Nội dung đã đổi so với lần gửi trước. Bấm lưu lại một lần nữa",
  internal: "Hệ thống gặp lỗi, chưa làm được. Thử lại sau ít phút",
  invalid_input: "Có ô nhập chưa đúng. Kiểm tra lại các ô được đánh dấu",
  job_state_conflict: "Việc nền đã đổi trạng thái. Tải lại để xem trạng thái mới",
  mfa_required: "Cần đăng nhập lại có bước xác minh thứ hai",
  not_found: "Không tìm thấy, có thể đã bị xóa",
  not_implemented: "Chức năng này chưa có",
  rate_limited: "Bạn thao tác quá nhanh. Chờ một lát rồi thử lại",
  request_in_progress: "Yêu cầu trước vẫn đang chạy. Chờ một lát",
  retry_later: "Hệ thống đang bận, thử lại sau ít giây",
  stream_unavailable: "Chưa nhận được tin báo tức thời. Trang vẫn tự tải lại định kỳ",
  unauthenticated: "Phiên đã hết hạn, tải lại trang",
  unsupported_media_type: "Dạng dữ liệu gửi lên không được nhận",
  version_conflict: "Người khác vừa sửa mục này. Tải lại để xem bản mới rồi sửa lại",
  // Nhân viên và vai trò
  cannot_modify_self: "Bạn không thể sửa quyền hay khóa chính mình",
  email_taken: "Email này đã có trong hệ thống",
  grant_exceeds_actor: "Bạn không thể cấp quyền mà chính bạn không có",
  last_owner: "Phải còn ít nhất một Chủ đang hoạt động",
  owner_role_locked: "Vai trò Chủ luôn có đủ mọi quyền, không sửa được",
  role_archived: "Vai trò đã lưu trữ, không gán được",
  role_in_use: "Vai trò đang được gán cho nhân viên, gỡ ra trước rồi mới lưu trữ",
  role_name_taken: "Đã có vai trò cùng tên",
  unknown_permission: "Có quyền không còn trong hệ thống. Tải lại trang",
  ip_not_allowed: "Máy gọi không nằm trong dải IP được phép của khóa",
  permission_not_allowed_for_key: "Khóa cho website chỉ nhận các quyền web.*",
  // Sản phẩm, danh mục
  already_exists: "Đã có mục cùng tên hoặc cùng mã",
  barcode_taken: "Mã vạch đã dùng cho biến thể khác",
  category_cycle: "Không đặt danh mục làm con của chính nó hay của danh mục con",
  category_in_use: "Danh mục đang có sản phẩm hoặc danh mục con",
  category_too_deep: "Danh mục lồng quá nhiều cấp",
  code_taken: "Mã này đã có",
  duplicate_option: "Có hai biến thể trùng màu và size",
  invalid_article: "Bài viết có khối không hợp lệ. Kiểm tra lại các khối được đánh dấu",
  invalid_price: "Giá không hợp lệ",
  not_for_sale: "Biến thể này không bán được (đã ngừng bán)",
  sku_taken: "Mã hàng đã có",
  // Kho
  document_not_draft: "Phiếu đã ghi sổ hoặc đã hủy, không sửa được",
  insufficient_stock: "Không đủ hàng: còn {available}",
  stock_short: "Tồn thực không đủ cho đơn đang giữ: trên kệ còn {available}. Điều chỉnh tồn, đổi kho hoặc hủy đơn",
  stocktake_not_open: "Phiên kiểm kho đã đóng",
  stocktake_open: "Kho đang có phiên kiểm kho chưa xong",
  supplier_archived: "Nhà cung cấp đã lưu trữ",
  supplier_name_taken: "Đã có nhà cung cấp cùng tên",
  supplier_not_for_kind: "Chỉ phiếu nhập mới có nhà cung cấp",
  // Đơn, vận chuyển
  mixed_sellers: "Đơn có sản phẩm của người bán khác",
  out_of_stock: "Không đủ hàng: còn {available}",
  shipment_exists: "Đơn đã có vận đơn",
  total_overflow: "Tổng tiền quá lớn",
  variant_not_for_sale: "Có biến thể đã ngừng bán",
  // Tệp, ảnh, nhập Excel
  file_not_ready: "Tệp đang được xử lý, chờ một lát",
  file_purpose_not_allowed: "Không tải được loại tệp này vào chỗ này",
  file_too_large: "Tệp quá lớn",
  file_type_not_allowed: "Loại tệp này không được nhận",
  import_not_ready: "Tệp nhập chưa kiểm xong hoặc còn dòng lỗi",
  restore_expired: "Đã quá hạn khôi phục",
  unknown_preset: "Bộ kích thước ảnh này chưa có",
  upload_missing: "Tệp chưa tải lên xong. Tải lại tệp",
  // Khách, đơn, mã giảm giá (M2)
  address_incomplete: "Địa chỉ giao chưa đủ tỉnh, xã, số nhà",
  coupon_locked_after_use: "Mã đã có người dùng: chỉ đổi được tên, hạn cuối, số lượt, trạng thái",
  coupon_rejected: "Mã giảm giá không dùng được cho đơn này",
  discount_needs_approval: "Mức giảm giá này cần người có quyền duyệt",
  identity_taken: "Thông tin này đã thuộc một khách khác",
  import_file_invalid: "Tệp nhập không dùng được. Tải lại tệp đúng mẫu",
  import_too_large: "Tệp nhập quá lớn hoặc quá nhiều dòng",
  invalid_phone: "Số điện thoại không hợp lệ",
  invalid_tag_rule: "Quy tắc nhãn chưa đúng. Kiểm tra lại các điều kiện",
  invalid_transition: "Đơn đang ở trạng thái không làm được việc này. Tải lại để xem trạng thái mới",
  line_discount_exceeds: "Giảm giá dòng lớn hơn thành tiền của dòng",
  max_uses_below_used: "Số lượt tối đa nhỏ hơn số lượt đã dùng",
  merge_not_latest: "Phải hoàn tác lần gộp sau trước",
  merge_same_customer: "Hai hồ sơ đã là một khách",
  merge_window_expired: "Đã quá 30 ngày, không hoàn tác gộp được",
  needs_approval: "Đơn đang chờ duyệt giảm giá",
  order_discount_exceeds: "Giảm giá đơn lớn hơn tạm tính",
  order_locked_by_shipment: "Đơn đã gửi hãng vận chuyển, không sửa được mục này",
  query_too_short: "Nhập thêm vài ký tự để tìm",
  return_not_allowed: "Đơn hoặc phiếu trả không ở trạng thái làm được việc này",
  return_qty_exceeds: "Số lượng trả vượt số còn trả được",
  qty_mismatch: "Số tốt, hỏng, thiếu của mỗi dòng phải cộng đúng bằng số trên phiếu, và đủ mọi dòng",
  setting_out_of_range: "Giá trị cài đặt ngoài khoảng cho phép",
  shipment_already_attached: "Đơn đã có vận đơn khác",
  traffic_source_inactive: "Nguồn đơn đã tắt",
  unknown_admin_unit: "Không tìm thấy tỉnh hoặc xã đã chọn",
  variant_not_found: "Có sản phẩm không còn trong danh mục",
  ward_not_in_province: "Xã/phường không thuộc tỉnh đã chọn",
  // Vận chuyển, tiền (M3, Task M3-04)
  address_unmapped: "Hãng chưa nhận địa chỉ này (tỉnh hoặc xã chưa khớp mã của hãng). Báo quản lý đối chiếu địa giới",
  capability_not_supported: "Hãng hoặc tài khoản này không hỗ trợ việc này",
  carrier_account_unavailable:
    "Tài khoản hãng chưa sẵn sàng (chưa kết nối, tạm dừng hoặc lỗi khóa). Chọn tài khoản khác hoặc báo quản lý",
  cod_locked_by_shipment: "Đơn đã gửi hãng, không đổi được tiền thu hộ. Ghi tiền sau khi vận đơn kết thúc hoặc đã hủy",
  correction_blocked: "Chưa sửa được: vận đơn đã có tiền về trong bảng kê đã chốt. Kế toán điều chỉnh tiền trước",
  create_in_progress: "Đang gửi hãng hoặc chưa rõ hãng đã tạo vận đơn. Chờ kết quả hoặc xử lý mục Không rõ trước",
  order_not_shippable: "Đơn chưa ở trạng thái gửi hãng được (phải Đã xác nhận và chưa có vận đơn)",
  payment_exceeds_total: "Tổng tiền đã thu sẽ vượt tổng đơn. Kiểm lại số tiền",
  preview_stale: "Dữ liệu đã đổi sau khi xem trước. Xem trước lại rồi xác nhận",
  quote_stale: "Báo phí đã hết hạn hoặc đơn đã đổi. Bấm so phí lại",
  refund_exceeds_remaining: "Số tiền chi vượt số còn phải hoàn cho khách",
  shipment_not_cancellable: "Hãng đã lấy hàng, không hủy được vận đơn. Chờ hãng chuyển hoàn",
  statement_changed: "Bảng kê vừa được người khác thay đổi. Tải lại rồi chốt lại",
  statement_duplicate: "Tệp bảng kê này đã được nhập trước đó",
  tracking_code_conflict: "Mã vận đơn của hãng đã dùng cho vận đơn khác",
  // Chiến dịch giảm giá web (Task 452)
  campaign_running: "Chiến dịch đang bật. Tạm dừng trước rồi mới sửa hoặc xóa",
  percent_too_high: "Phần trăm giảm vượt mức tối đa cài đặt của shop (mặc định 50%)",
  preview_required: "Cần xem trước giá của bản hiện tại (hoặc xem lại vì hàng đã đổi) rồi mới bật",
  // Đợt mã của website (Task 197; chỉ website nhận, màn hình nhân viên không gọi route này)
  batch_conflict: "Đã có lô mã cùng tên với điều khoản khác",
  // Đơn web (Task 194; chỉ website nhận, màn hình nhân viên không gọi route này)
  coupon_invalid: "Mã giảm giá không dùng được cho đơn này",
  item_unavailable: "Có sản phẩm đã ngừng bán trên website",
  quote_changed: "Giá hoặc tổng tiền đã đổi. Xem lại giỏ hàng rồi đặt lại",
  too_fast: "Khách đặt quá nhiều đơn trong thời gian ngắn. Thử lại sau ít phút",
  coupon_attempts_exceeded: "Khách nhập sai mã giảm giá quá nhiều lần. Thử lại sau ít phút hoặc đặt không dùng mã",
  // Tạm dừng nhận đơn web (Task 456)
  another_run_in_progress: "Một lượt phát quà sinh nhật khác đang chạy. Chờ vài phút rồi xem lượt chạy cuối",
  invalid_gift_settings:
    "Cài đặt quà chưa đúng: mệnh giá 10.000–1.000.000 (bước 1.000), đơn tối thiểu tới 100.000.000, phát trước 0–30 ngày, hạn 1–60 ngày, đăng ký trước 0–120 ngày",
  carrier_not_on_web: "Hãng này chưa bật hiện trên website hoặc đang tạm dừng. Bật hãng trước rồi mới chọn ưu tiên",
  last_web_carrier: "Phải còn ít nhất một hãng hiện trên website. Bật hãng khác trước rồi mới tắt hãng này",
  web_orders_paused: "ELIMAZ đang tạm dừng nhận đơn trên website. Mở lại ở màn Nhận đơn web",
  shipping_carrier_unavailable: "Hãng giao khách chọn không còn giao trên website. Khách cần chọn lại cách giao",
  shipping_fee_mismatch: "Phí giao khách chọn không khớp báo phí hiện tại. Khách cần xem lại phí giao",
  // Banner trang chủ (Task 453)
  too_many_banners: "Tối đa 8 banner trên trang chủ",
  invalid_banner:
    "Banner chưa đúng: chữ quá dài (nhỏ 40, tiêu đề 60, nút 30, mô tả ảnh 120 ký tự), loại hoặc màu lạ, hoặc ghim trùng",
  banner_campaign_required: "Banner giảm giá cần chọn chiến dịch",
  banner_category_required: "Banner danh mục cần chọn danh mục",
  banner_image_required: "Banner ảnh cần ảnh cho máy tính, mô tả ảnh và nơi đến khi bấm",
  banner_window_invalid: "Thời gian kết thúc phải sau thời gian bắt đầu",
  too_many_pins: "Mỗi banner ghim tối đa 3 sản phẩm",
  pin_not_in_source: "Sản phẩm ghim phải đang bán và thuộc chiến dịch hoặc danh mục của banner",
  banner_image_invalid: "Ảnh banner không còn hoặc không phải ảnh tải lên cho banner. Tải ảnh lại",
  too_many_looks: "Tối đa 500 bộ phối",
  too_many_home_looks: "Tối đa 6 bộ phối trên trang chủ. Tắt bớt một bộ trước khi bật bộ khác",
  invalid_look:
    "Bộ phối chưa đúng: cần tên (tối đa 28 ký tự), chữ không quá dài, chiều cao người mẫu 130–200 cm, tối đa 6 danh mục",
  look_items_invalid: "Mỗi bộ phối có 1 đến 3 sản phẩm khác nhau",
  look_dot_invalid: "Chấm sản phẩm phải nằm trong ảnh",
  look_photo_invalid: "Ảnh người mẫu phải là một ảnh của sản phẩm trong bộ. Chọn lại ảnh",
  look_window_invalid: "Thời gian kết thúc phải sau thời gian bắt đầu",
  // Yêu cầu về đơn hàng (Task 195)
  window_closed: "Đơn đã quá hạn đổi, trả hàng",
  qty_exceeded: "Số lượng yêu cầu vượt số còn đổi được của đơn",
  photo_required: "Yêu cầu đổi vì hàng lỗi hay giao nhầm cần kèm ảnh",
  too_many_files: "Mỗi yêu cầu kèm tối đa 5 ảnh",
  duplicate_open: "Đơn đã có một yêu cầu hủy đang xử lý",
  file_rejected: "Ảnh không dùng được: không phải ảnh hợp lệ, quá lớn, hoặc quá nhỏ (ảnh banner cần rộng từ 600 px)",
  file_not_found: "Không dùng được ảnh kèm (không có hoặc đã gắn yêu cầu khác)",
  request_not_allowed: "Đơn hay yêu cầu không ở trạng thái cho việc này",
  // Khách hủy đơn web (Task 196)
  cannot_cancel: "Đơn đã giao, đã hoàn hay đã hủy nên khách không hủy được",
  // Hộp chat, Live (M4, Task 143)
  already_assigned: "Hội thoại đã có người khác nhận. Tải lại để xem ai đang phụ trách",
  assignee_not_allowed: "Nhân viên này không còn hoạt động hoặc không được xem Page này",
  attachment_not_ready: "Ảnh đang được xử lý, chờ vài giây rồi thử lại",
  attachment_rejected: "Tệp không dùng được: không phải ảnh hợp lệ hoặc quá lớn",
  channel_read_only: "Page đang ở chế độ chỉ xem, chưa gửi được. Quản lý bật gửi thật ở màn Kênh kết nối",
  comment_claimed: "Comment này đã được lên đơn. Tải lại để xem đơn",
  oauth_state_invalid: "Phiên đăng nhập Facebook đã hết hạn hoặc đã dùng. Bấm Kết nối Page lại từ đầu",
  open_draft_exists: "Khách đã có đơn nháp trong phiên live này. Thêm vào đơn đó, hoặc chọn tạo đơn mới",
  page_connected_elsewhere: "Page này đang nối với một cửa hàng khác",
  page_not_accessible: "Tài khoản chưa có quyền nhắn tin và kiểm duyệt trên Page này",
  private_reply_used: "Comment này đã được nhắn riêng một lần, không nhắn riêng thêm được",
  seller_mismatch: "Sản phẩm không thuộc người bán của Page này",
  conversation_customer_mismatch:
    "Khách của đơn không phải khách của hội thoại này. Lên đơn cho khách của hội thoại, hoặc gộp hồ sơ trước",
  send_not_allowed: "Không gửi được lúc này: đã quá thời hạn trả lời của Facebook hoặc khách đã chặn Page",
  template_variable_missing: "Mẫu trả lời thiếu thông tin để điền. Kiểm tra khách, đơn rồi thử lại",
};

/** Câu cho từng lý do từ chối mã giảm giá (CouponRejectReason: checkCoupon.reason, thân lỗi coupon_rejected.reason). */
export const COUPON_REASON_MESSAGES: Readonly<Record<string, string>> = {
  not_found: "Không có mã này",
  disabled: "Mã đã bị tắt",
  not_started: "Mã chưa tới ngày dùng",
  expired: "Mã đã hết hạn",
  exhausted: "Mã đã hết lượt dùng",
  min_subtotal: "Đơn chưa đủ giá trị tối thiểu của mã",
  channel: "Mã không dùng cho kênh này",
  bound_other: "Mã dành riêng cho khách khác",
  customer_limit: "Khách đã dùng hết lượt của mã này",
  seller: "Mã không áp cho sản phẩm của người bán này",
};

/** Lỗi API có mã (thân {"error", "request_id", ...}). code rỗng khi thân không đúng dạng (ví dụ 502 của cổng vào). */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId: string | undefined;
  /** Các trường thêm của thân lỗi (ví dụ available, variantId). */
  readonly details: Readonly<Record<string, unknown>>;
  /** Thời gian chờ theo header Retry-After (ms); null khi không có hoặc sai dạng. */
  readonly retryAfterMs: number | null;

  constructor(o: {
    status: number;
    code: string;
    requestId?: string;
    details: Record<string, unknown>;
    retryAfterMs?: number | null;
  }) {
    super(`API ${String(o.status)} ${o.code}`);
    this.name = "ApiError";
    this.status = o.status;
    this.code = o.code;
    this.requestId = o.requestId;
    this.details = o.details;
    this.retryAfterMs = o.retryAfterMs ?? null;
  }
}

/** Yêu cầu không tới được máy chủ, hoặc không có câu trả lời: chưa rõ máy chủ đã làm hay chưa. */
export class NetworkError extends Error {
  constructor(cause?: unknown) {
    super("không tới được máy chủ", { cause });
    this.name = "NetworkError";
  }
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

// Retry-After: số giây (RFC 9110 cũng cho ngày giờ HTTP; API chỉ gửi số giây). Sai dạng → null.
function parseRetryAfter(v: string | null): number | null {
  if (v === null || !/^\d+$/.test(v.trim())) return null;
  return Number(v.trim()) * 1000;
}

/** Kết quả của một hàm trong client sinh (dạng mặc định {data, error, response}). */
export interface ClientResult<T> {
  data?: T;
  error?: unknown;
  response?: Response;
}

/**
 * Đổi kết quả của client sinh thành dữ liệu; lỗi HTTP → ApiError, không có câu trả lời → NetworkError.
 * Dùng: `unwrap(getProduct({ path: { id } }))`.
 */
export async function unwrap<T>(p: Promise<ClientResult<T>>): Promise<T> {
  let r: ClientResult<T>;
  try {
    r = await p;
  } catch (e) {
    throw new NetworkError(e);
  }
  const res = r.response;
  if (res === undefined) throw new NetworkError(r.error);
  if (res.ok) {
    // 2xx: máy chủ đã làm xong. Đọc thân hỏng (mạng rớt giữa chừng, thân sai dạng) → NetworkError để đường ghi giữ
    // khóa và gửi lại cùng khóa (máy chủ trả lại kết quả đã lưu), không coi là lỗi dứt khoát. Client sinh trả {} cho 204.
    if (r.error !== undefined || r.data === undefined)
      throw new NetworkError(r.error ?? new Error("2xx không có dữ liệu"));
    return r.data;
  }
  const body = isRecord(r.error) ? r.error : {};
  const { error: code, request_id: requestId, ...details } = body;
  throw new ApiError({
    status: res.status,
    code: typeof code === "string" ? code : "",
    requestId: typeof requestId === "string" ? requestId : undefined,
    details,
    retryAfterMs: parseRetryAfter(res.headers.get("retry-after")),
  });
}

// fill thay {tên} bằng trường số nguyên không âm của details; thiếu thì bỏ cả phần ": còn {tên}".
function fill(text: string, details: Readonly<Record<string, unknown>>): string {
  return text.replace(/(?::\s*[^{:]*)?\{(\w+)\}/g, (whole, name: string) => {
    const v = details[name];
    return typeof v === "number" && Number.isSafeInteger(v) && v >= 0 ? whole.replace(`{${name}}`, String(v)) : "";
  });
}

/** Câu tiếng Việt cho một lỗi bất kỳ (ApiError, NetworkError hay lỗi khác). */
export function errorMessage(err: unknown): string {
  if (err instanceof NetworkError) return TEXT_NETWORK;
  if (!(err instanceof ApiError)) return TEXT_UNKNOWN_ERROR;
  const text = Object.hasOwn(ERROR_MESSAGES, err.code) ? ERROR_MESSAGES[err.code] : undefined;
  if (text !== undefined) return fill(text, err.details);
  return err.requestId === undefined ? TEXT_UNKNOWN_ERROR : `${TEXT_UNKNOWN_ERROR} (mã yêu cầu ${err.requestId})`;
}
