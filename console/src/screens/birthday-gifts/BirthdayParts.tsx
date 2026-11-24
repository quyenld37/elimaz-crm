// Phần nhỏ dùng ở nhiều tab của màn Quà sinh nhật: hộp lỗi (403 → câu không quyền của mockup D), băng báo, ô khách.
import { errorMessage } from "../../app/errors";
import { TEXT_FORBIDDEN, isForbidden } from "./birthdayData";

/** Lỗi đọc: 403 → câu không quyền (không vẽ như danh sách rỗng); khác → câu theo mã lỗi. */
export function ErrorBox({ error }: { error: unknown }) {
  if (isForbidden(error))
    return (
      <div className="box box-err stack-sm bg-gap">
        <b>{TEXT_FORBIDDEN}</b>
        <p>
          Nhờ Quản lý cấp quyền "Xem khuyến mãi" (xem mọi tab) hoặc "Quản lý quà sinh nhật" (tạo mã, nhắn, xuất, đổi cài
          đặt).
        </p>
      </div>
    );
  return <div className="box box-err bg-gap">{errorMessage(error)}</div>;
}

export function Toast({ text }: { text: string | null }) {
  if (text === null) return null;
  return (
    <div className="toast bg-gap" role="status">
      <span className="grow">{text}</span>
    </div>
  );
}

/** Ô tên khách: tên (cắt hai dòng, đủ tên khi rê) và SĐT che giữa. */
export function Who({ name, phone }: { name: string; phone: string }) {
  return (
    <div className="who">
      <b className="clamp2 wrap-any" title={name}>
        {name}
      </b>
      <span className="small muted num">{phone}</span>
    </div>
  );
}
