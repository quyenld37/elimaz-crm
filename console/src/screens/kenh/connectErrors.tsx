// Hộp lỗi khi kết nối Page (mockup ket-noi-page.html trạng thái B "Lỗi khi kết nối"): câu theo mã lỗi của máy chủ
// (Task 149: page_connected_elsewhere 409, page_not_accessible 422, oauth_state_invalid 400); mã khác dùng câu chung.
import { ApiError, errorMessage } from "../../app/errors";

function texts(code: string, pageName: string): [string, string] | null {
  switch (code) {
    case "page_connected_elsewhere":
      return [
        `${pageName} đang nối với một CRM khác.`,
        "Một Page chỉ nối với một shop. Gỡ ở CRM kia trước rồi thử lại.",
      ];
    case "page_not_accessible":
      return [
        `Chưa đủ quyền trên ${pageName}.`,
        "Tài khoản Facebook của bạn cần quyền nhắn tin và kiểm duyệt comment trên Page này.",
      ];
    case "oauth_state_invalid":
      return ["Phiên kết nối đã hết hạn hoặc đã dùng.", 'Bấm "Kết nối Page Facebook" để làm lại.'];
    default:
      return null;
  }
}

export function ConnectError({ error, pageName }: { error: unknown; pageName: string }) {
  const t = error instanceof ApiError ? texts(error.code, pageName) : null;
  return (
    <div className="box box-err small" role="alert">
      {t === null ? (
        <b>{errorMessage(error)}</b>
      ) : (
        <>
          <b>{t[0]}</b> {t[1]}
        </>
      )}
    </div>
  );
}
