// Băng trạng thái tài khoản kênh trên đầu danh sách (mockup inbox.html trạng thái C; HV08; HT15 phía màn hình), từ
// listChannelAccounts (tải lại theo báo hiệu channel.status):
// - status degraded: "Page {tên} mất kết nối, cần quản trị kết nối lại." kèm lý do theo statusReason (Task 149, 150);
// - còn kết nối mà sendMode read_only: "Page {tên} đang ở chế độ chỉ xem." (CRM không gửi gì ra ngoài).
// Tài khoản quản trị đã ngắt (disconnected) không có băng. Nút sang Kênh kết nối chỉ hiện khi có channel.manage.
import { Link } from "react-router";
import type { ChannelAccount } from "../../api";
import { accountNoun, channelName, degradedReason } from "./inboxRules";

export const CHANNELS_PATH = "/settings/channels";

export interface ChannelBannersProps {
  accounts: readonly ChannelAccount[];
  /** Người xem có channel.manage (mở được màn Kênh kết nối). */
  canManage: boolean;
}

export function ChannelBanners({ accounts, canManage }: ChannelBannersProps) {
  const broken = accounts.filter((a) => a.status === "degraded");
  const readOnly = accounts.filter((a) => a.status === "active" && a.sendMode === "read_only");
  if (broken.length === 0 && readOnly.length === 0) return null;
  return (
    <>
      {broken.map((a) => (
        <div key={a.id} className="bnr bnr-err" role="status">
          <b>{`${accountNoun(a)} ${a.name} mất kết nối, cần quản trị kết nối lại.`}</b>
          <span className="grow">
            {`${degradedReason(a)} CRM chưa gửi được gì ra ${channelName(a)} cho tới khi kết nối lại.`}
          </span>
          {canManage && (
            <Link className="btn btn-quiet" to={CHANNELS_PATH}>
              Mở Kênh kết nối
            </Link>
          )}
        </div>
      ))}
      {readOnly.map((a) => (
        <div key={a.id} className="bnr bnr-info" role="status">
          <span className="grow">
            <b>{`${accountNoun(a)} ${a.name} đang ở chế độ chỉ xem.`}</b>
            {` CRM nhận tin và comment nhưng chưa gửi gì ra ${channelName(a)}. Quản trị bật gửi ở Kênh kết nối.`}
          </span>
        </div>
      ))}
    </>
  );
}
