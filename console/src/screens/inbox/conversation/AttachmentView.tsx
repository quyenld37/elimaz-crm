// Tệp đính kèm của một tin (Task 175; mockup hoi-thoai.html trạng thái A; HV13; TP04). Không nạp gì khi chưa bấm: bấm
// mới xin đường có hạn 5 phút (getAttachmentUrl). Ảnh hiện trong trang, cao tối đa 360 px, có liên kết xem cỡ thật; tệp
// khác chỉ là liên kết tải về (download), không xem trước. Tên tệp từ khách hiện dạng chữ.
import { useState } from "react";
import { getAttachmentUrl, type ChatAttachment } from "../../../api";
import { errorMessage, unwrap } from "../../../app/errors";

type Loaded =
  { state: "idle" } | { state: "loading" } | { state: "ready"; url: string } | { state: "error"; text: string };

function useAttachmentUrl(conversationId: string, fileId: string | undefined) {
  const [s, setS] = useState<Loaded>({ state: "idle" });
  const load = () => {
    if (fileId === undefined || s.state === "loading") return;
    setS({ state: "loading" });
    unwrap(getAttachmentUrl({ path: { id: conversationId, fileId } })).then(
      (r) => {
        setS({ state: "ready", url: r.url });
      },
      (e: unknown) => {
        setS({ state: "error", text: errorMessage(e) });
      },
    );
  };
  return { s, load };
}

const KIND_TEXT: Readonly<Record<string, string>> = {
  video: "Video",
  audio: "Âm thanh",
  sticker: "Nhãn dán",
  file: "Tệp",
};

/** Tệp chưa có ở kho CRM (status của mục đính kèm, Task 155): đang tải về, hay không lấy được (lý do). */
const BROKEN: Readonly<Record<string, string>> = {
  pending: "Đang tải tệp về CRM",
  blocked: "Không lấy được tệp: địa chỉ không thuộc máy chủ của kênh",
  unavailable: "Không lấy được tệp: kênh báo tệp không còn",
  too_large: "Không lấy được tệp: quá 25 MB",
  failed: "Không lấy được tệp từ kênh",
  expired: "Không lấy được tệp: quá hạn tải về",
};
const notReady = (a: ChatAttachment) => a.fileId === undefined || a.status in BROKEN;
const brokenText = (a: ChatAttachment) => BROKEN[a.status] ?? "Không lấy được tệp từ kênh";

function ImageAttachment({
  conversationId,
  a,
  outgoing,
}: {
  conversationId: string;
  a: ChatAttachment;
  outgoing: boolean;
}) {
  const { s, load } = useAttachmentUrl(conversationId, a.fileId);
  const label = outgoing ? "Ảnh shop gửi" : "Ảnh khách gửi";
  if (s.state === "ready")
    return (
      <div className="att-view">
        <img className="att-img" src={s.url} alt={label} />
        <a className="btn btn-text" href={s.url} target="_blank" rel="noreferrer noopener">
          Xem cỡ thật
        </a>
      </div>
    );
  return (
    <div className="att-view">
      <button
        type="button"
        className="att-img is-tall att-btn"
        aria-label={`${label}, bấm để xem`}
        disabled={notReady(a) || s.state === "loading"}
        onClick={load}
      >
        {notReady(a) ? brokenText(a) : s.state === "loading" ? "Đang mở ảnh…" : "Bấm để xem ảnh"}
      </button>
      {s.state === "error" && <span className="small err-text">{s.text}</span>}
    </div>
  );
}

function FileAttachment({ conversationId, a }: { conversationId: string; a: ChatAttachment }) {
  const { s, load } = useAttachmentUrl(conversationId, a.fileId);
  const name = a.name ?? KIND_TEXT[a.type] ?? "Tệp";
  return (
    <div className="att-file">
      <div className="grow">
        <b className="wrap-any">{name}</b>
        <span className="xs muted">
          {notReady(a)
            ? brokenText(a)
            : s.state === "ready"
              ? "Liên kết tải có hạn 5 phút"
              : `${KIND_TEXT[a.type] ?? "Tệp"} · không xem trước`}
        </span>
        {s.state === "error" && <span className="small err-text">{s.text}</span>}
      </div>
      {s.state === "ready" ? (
        <a className="btn btn-line" href={s.url} download={name}>
          Tải về
        </a>
      ) : (
        <button type="button" className="btn btn-quiet" disabled={notReady(a) || s.state === "loading"} onClick={load}>
          Tải về
        </button>
      )}
    </div>
  );
}

export function AttachmentView({
  conversationId,
  attachments,
  outgoing,
}: {
  conversationId: string;
  attachments: readonly ChatAttachment[];
  outgoing: boolean;
}) {
  return (
    <>
      {attachments.map((a, i) =>
        a.type === "image" ? (
          <ImageAttachment key={a.fileId ?? i} conversationId={conversationId} a={a} outgoing={outgoing} />
        ) : (
          <FileAttachment key={a.fileId ?? i} conversationId={conversationId} a={a} />
        ),
      )}
    </>
  );
}
