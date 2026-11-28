// Hộp lỗi tải của màn Đợt flash sale: tiêu đề, câu giải thích, nút Thử lại (không vẽ dữ liệu cũ hay số 0 thay chỗ lỗi).
export function RetryBox(o: { title: string; text: string; onRetry: () => void }) {
  return (
    <div className="box box-err stack-sm">
      <b>{o.title}</b>
      <p>{o.text}</p>
      <div>
        <button type="button" className="btn btn-line" onClick={o.onRetry}>
          Thử lại
        </button>
      </div>
    </div>
  );
}
