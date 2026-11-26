// Khung hội thoại ở cột giữa của /inbox/:conversationId (Task 174 dựng khung nạp lười; Task 175 làm nội dung: tin, ô
// gõ, trạng thái gửi, thao tác comment, ghi chú, ai đang xem). Trên điện thoại hội thoại là màn riêng: nút "‹" về danh
// sách giữ bộ lọc (HV02).
import type { UploadOptions } from "../../products/useUpload";
import { ConversationView } from "./ConversationView";

export interface ConversationPaneProps {
  conversationId: string;
  /** Đường về danh sách (kèm bộ lọc). */
  backTo: string;
  /** Chỉ test: PUT giả lên kho tệp khi gửi ảnh. */
  uploadOptions?: UploadOptions;
}

export default function ConversationPane({ conversationId, backTo, uploadOptions }: ConversationPaneProps) {
  return (
    <ConversationView
      key={conversationId}
      conversationId={conversationId}
      backTo={backTo}
      {...(uploadOptions === undefined ? {} : { uploadOptions })}
    />
  );
}
