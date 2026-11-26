// Người phụ trách (Task 176; mockup khung-ben-phai.html trạng thái A, C; HV31; CH06 phía màn hình):
// - "Nhận" (claimConversation) khi chưa ai nhận và có conversation.reply trên Page; người khác vừa nhận → 409
//   already_assigned, câu tiếng Việt.
// - "Giao cho…" (hộp chọn nhân viên từ listAssignableUsers: đang hoạt động, có quyền xem hội thoại ở Page) và "Trả lại
//   (bỏ nhận)" chỉ khi có conversation.assign trên Page; gửi expectedVersion của chi tiết đã đọc.
// Danh sách không có số hội thoại đang phụ trách (máy chủ không trả).
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import {
  assignConversation,
  claimConversation,
  listAssignableUsers,
  type ConversationDetail,
  type Grant,
} from "../../../api";
import { errorMessage, unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { queryKey } from "../../../app/queryClient";
import { Dialog } from "../../staff/staffData";
import { CONVERSATION_TOPIC } from "../useConversationList";
import { canOnPage, detailKey } from "./sidePanelData";

export interface AssigneeControlProps {
  detail: ConversationDetail;
  grants: readonly Grant[];
  meId: string;
}

const fold = (s: string) =>
  s
    .normalize("NFD")
    .replace(/\p{Diacritic}/gu, "")
    .replace(/đ/g, "d")
    .replace(/Đ/g, "D")
    .toLowerCase();

function AssignDialog({
  detail,
  meId,
  onDone,
  onClose,
}: {
  detail: ConversationDetail;
  meId: string;
  onDone: () => void;
  onClose: () => void;
}) {
  const id = useId();
  const pageId = detail.page.id;
  const [q, setQ] = useState("");
  const [pick, setPick] = useState<string | null>(null);
  const users = useQuery({
    queryKey: queryKey(CONVERSATION_TOPIC, "assignable-users", pageId),
    queryFn: () => unwrap(listAssignableUsers({ path: { id: pageId } })),
  });
  const assign = useWriteAction((key, userId: string) =>
    unwrap(
      assignConversation({
        path: { id: detail.conversation.id },
        body: { userId, expectedVersion: detail.conversation.version },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const label = (u: { id: string; name: string }) => (u.id === meId ? `${u.name} (bạn)` : u.name);
  const shown = (users.data?.items ?? []).filter((u) => fold(label(u)).includes(fold(q.trim())));
  const submit = async () => {
    if (pick === null) return;
    const r = await assign.run(pick);
    if (r.ok) onDone();
  };
  return (
    <Dialog
      title="Giao cho"
      closeButton
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={onClose}>
            Hủy
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={pick === null || assign.pending}
            onClick={() => {
              void submit();
            }}
          >
            Giao
          </button>
        </>
      }
    >
      <input
        className="input is-search"
        type="search"
        aria-label="Tìm nhân viên"
        placeholder="Tìm nhân viên"
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
        }}
      />
      {users.isError && (
        <div className="box box-err" role="alert">
          {errorMessage(users.error)}
        </div>
      )}
      {users.isPending && <p className="muted small">Đang tải nhân viên…</p>}
      {users.isSuccess && shown.length === 0 && <p className="muted small">Không có nhân viên nào khớp.</p>}
      <div className="radios" role="radiogroup" aria-label="Nhân viên">
        {shown.map((u) => (
          <label key={u.id} className="check">
            <input
              type="radio"
              name={`${id}-nv`}
              checked={pick === u.id}
              onChange={() => {
                setPick(u.id);
              }}
            />{" "}
            {label(u)}
          </label>
        ))}
      </div>
      {assign.message !== null && (
        <p className="err-text" role="alert">
          {assign.message}
        </p>
      )}
      <p className="xs muted">
        Chỉ hiện nhân viên đang làm việc có quyền xem hội thoại ở Page này. Người được giao thấy hội thoại trong
        &quot;Của tôi&quot;.
      </p>
    </Dialog>
  );
}

export function AssigneeControl({ detail, grants, meId }: AssigneeControlProps) {
  const qc = useQueryClient();
  const pageId = detail.page.id;
  const convId = detail.conversation.id;
  const assignee = detail.conversation.assignee;
  const canReply = canOnPage(grants, "conversation.reply", pageId);
  const canAssign = canOnPage(grants, "conversation.assign", pageId);
  const [open, setOpen] = useState(false);
  const reload = () => {
    void qc.invalidateQueries({ queryKey: detailKey(convId) });
  };
  const claim = useWriteAction((key, id: string) =>
    unwrap(claimConversation({ path: { id }, headers: { "Idempotency-Key": key } })),
  );
  const unassign = useWriteAction((key, version: number) =>
    unwrap(
      assignConversation({
        path: { id: convId },
        body: { userId: null, expectedVersion: version },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const message = claim.message ?? unassign.message;

  return (
    <section className="sec" aria-label="Người phụ trách">
      <h3>Người phụ trách</h3>
      <p className="small">
        {assignee === undefined ? "Chưa ai nhận" : assignee.id === meId ? `${assignee.name} (bạn)` : assignee.name}
      </p>
      <div className="row">
        {assignee === undefined && canReply && (
          <button
            type="button"
            className="btn btn-line"
            disabled={claim.pending}
            onClick={() => {
              void claim.run(convId).then((r) => {
                if (r.ok) reload();
              });
            }}
          >
            Nhận
          </button>
        )}
        {canAssign && (
          <button
            type="button"
            className="btn btn-quiet"
            onClick={() => {
              setOpen(true);
            }}
          >
            Giao cho…
          </button>
        )}
        {canAssign && assignee !== undefined && (
          <button
            type="button"
            className="btn btn-quiet"
            disabled={unassign.pending}
            onClick={() => {
              void unassign.run(detail.conversation.version).then((r) => {
                if (r.ok) reload();
              });
            }}
          >
            Trả lại (bỏ nhận)
          </button>
        )}
      </div>
      {message !== null && (
        <p className="err-text" role="alert">
          {message}
        </p>
      )}
      {open && (
        <AssignDialog
          detail={detail}
          meId={meId}
          onClose={() => {
            setOpen(false);
          }}
          onDone={() => {
            setOpen(false);
            reload();
          }}
        />
      )}
    </section>
  );
}
