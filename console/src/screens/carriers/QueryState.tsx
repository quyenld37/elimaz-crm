// Hộp lỗi và dòng "Đang tải…" của một truy vấn trong thẻ (thẻ Địa giới, Địa chỉ lấy hàng): lúc tải, lúc lỗi không vẽ
// như dữ liệu rỗng.
import { errorMessage } from "../../app/errors";

export function QueryState({ q }: { q: { isError: boolean; isPending: boolean; error: unknown } }) {
  if (q.isError) {
    return (
      <div className="box box-err small" role="alert">
        {errorMessage(q.error)}
      </div>
    );
  }
  return q.isPending ? <p className="muted small">Đang tải…</p> : null;
}
