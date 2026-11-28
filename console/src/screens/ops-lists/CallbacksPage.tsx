// Cần gọi lại /ops/callbacks (kế hoạch M3 Task 135; mockup docs/mockups/m3/van-hanh.html trạng thái A–C; VC07, KH09).
// Quá hạn đứng đầu (máy chủ sắp), nhãn "Quá hạn 3 giờ"; danh sách ảo (200+ việc cuộn không giật), tải trang kế khi cuộn
// gần cuối. SĐT không có trong danh sách: chỉ đọc (getOrder) khi bấm "Hiện số", rồi "Gọi" mở ứng dụng điện thoại (tel:).
import { useVirtualizer } from "@tanstack/react-virtual";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { assignCallbackTask, getOrder, listCallbackTasks, type CallbackTask } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useWriteAction } from "../../shared";
import { carrierName } from "../shipping/QuoteTable";
import { AssignDialog } from "./AssignDialog";
import { CallbackOutcomeDialog } from "./CallbackOutcomeDialog";
import { callbacksKey, headers, reloadOps, usePerms, useOpsCounts, useOpsPaged } from "./opsData";
import { ListState, OpsFrame, Seg } from "./OpsTabs";
import { dueText, overdueLabel, phoneText, riskChip, statsText, statusText, telHref } from "./opsRules";
import "./ops.css";

/** Chiều cao ước tính một dòng (px), đo lại sau khi vẽ. */
const ROW_ESTIMATE = 120;

type StatusFilter = "" | "open" | "in_progress" | "done";
const STATUS_FILTERS: readonly { key: StatusFilter; label: string }[] = [
  { key: "", label: "Đang mở" },
  { key: "open", label: "Chưa xử lý" },
  { key: "in_progress", label: "Đang xử lý" },
  { key: "done", label: "Đã xong" },
];

type Who = "all" | "mine" | "none";
const WHO_FILTERS: readonly { key: Who; label: string }[] = [
  { key: "all", label: "Mọi người" },
  { key: "mine", label: "Của tôi" },
  { key: "none", label: "Chưa ai nhận" },
];

interface RowProps {
  t: CallbackTask;
  now: number;
  meId: string;
  canWork: boolean;
  canManage: boolean;
  canReadOrder: boolean;
  onOutcome: (t: CallbackTask) => void;
  onAssign: (t: CallbackTask) => void;
  onClaim: (t: CallbackTask) => void;
  claiming: boolean;
}

/** "Hiện số": đọc số qua getOrder khi bấm, giữ trong state của dòng (không vào khóa truy vấn dùng chung). */
function PhoneCell({ t, canReadOrder }: { t: CallbackTask; canReadOrder: boolean }) {
  const [phone, setPhone] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  if (phone !== null) {
    const href = telHref(phone);
    return href === null ? (
      <span className="small muted">Đơn chưa có số điện thoại</span>
    ) : (
      <a className="btn btn-dark" href={href}>{`Gọi ${phoneText(phone)}`}</a>
    );
  }
  const reveal = () => {
    setBusy(true);
    setErr(null);
    unwrap(getOrder({ path: { id: t.orderId } }))
      .then((o) => {
        setPhone(o.shipAddress?.phone ?? "");
      })
      .catch((e: unknown) => {
        setErr(errorMessage(e));
      })
      .finally(() => {
        setBusy(false);
      });
  };
  return (
    <>
      <button
        type="button"
        className="btn btn-line"
        disabled={busy || !canReadOrder}
        title={canReadOrder ? undefined : "Không có quyền xem số (cần quyền xem đơn)"}
        onClick={reveal}
      >
        Hiện số
      </button>
      {err !== null && <span className="small err-text">{err}</span>}
    </>
  );
}

function Row(p: RowProps) {
  const { t } = p;
  const late = overdueLabel(t, p.now);
  const chip = t.deliveryStats === undefined ? null : riskChip(t.deliveryStats.risk);
  const closed = t.status === "done" || t.status === "closed";
  const mine = t.assignee?.id === p.meId;
  const status = statusText(t);
  return (
    <div className={late === null ? "cb-row" : "cb-row is-late"}>
      <div className="cb-who">
        <Link className="num bold" to={`/don/${t.orderId}`}>
          {t.orderCode}
        </Link>
        <span className="small ellip-2">{t.customerName ?? "Khách"}</span>
        {t.deliveryStats !== undefined && (
          <span className="xs muted">
            {statsText(t.deliveryStats)}
            {chip !== null && <span className="chip chip-line">{chip}</span>}
          </span>
        )}
      </div>
      <div className="cb-why small">
        <span className="xs muted">{`Lần ${String(t.attemptNo)} · ${t.carrierKey === undefined ? "" : carrierName(t.carrierKey, t.carrierKey)}`}</span>
        <span>{t.failedReason ?? "Giao thất bại"}</span>
      </div>
      <div className="cb-due small">
        {late !== null && <span className="chip chip-err">{late}</span>}
        {status !== "" && <span className="chip">{status}</span>}
        <span className="muted num">{dueText(t.dueAt)}</span>
      </div>
      <div className="cb-who2 small">
        {t.assignee === undefined ? (
          <span className="muted">Chưa ai nhận</span>
        ) : (
          <span>{mine ? `${t.assignee.name} (bạn)` : t.assignee.name}</span>
        )}
      </div>
      {!closed && p.canWork && (
        <div className="cb-act">
          <PhoneCell t={t} canReadOrder={p.canReadOrder} />
          {t.assignee === undefined && (
            <button
              type="button"
              className="btn btn-line"
              disabled={p.claiming}
              onClick={() => {
                p.onClaim(t);
              }}
            >
              Nhận việc
            </button>
          )}
          {p.canManage && (
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                p.onAssign(t);
              }}
            >
              Giao lại
            </button>
          )}
          <button
            type="button"
            className="btn btn-dark"
            onClick={() => {
              p.onOutcome(t);
            }}
          >
            Ghi kết quả
          </button>
        </div>
      )}
    </div>
  );
}

export function CallbacksPage() {
  const qc = useQueryClient();
  const perms = usePerms();
  const [status, setStatus] = useState<StatusFilter>("");
  const [who, setWho] = useState<Who>("all");
  const mine = who === "mine";
  const unassigned = who === "none";
  // Hộp mở cho một việc: giữ bản đã bấm, nhưng gửi đi bản mới nhất trong danh sách (sau 409 danh sách tải lại, hộp
  // phải dùng version mới, không thì lưu lại cứ 409 mãi).
  const [outcomeOf, setOutcomeFor] = useState<CallbackTask | null>(null);
  const [assignOf, setAssignFor] = useState<CallbackTask | null>(null);
  const counts = useOpsCounts().data;
  const paged = useOpsPaged(callbacksKey(status, mine), (query) =>
    unwrap(
      listCallbackTasks({ query: { ...query, ...(status === "" ? {} : { status }), assignee: mine ? "me" : "any" } }),
    ),
  );
  const { list } = paged;
  const loaded = list.data?.pages.flatMap((p) => p.items) ?? [];
  const fresh = (t: CallbackTask | null) => (t === null ? null : (loaded.find((r) => r.id === t.id) ?? t));
  const outcomeFor = fresh(outcomeOf);
  const assignFor = fresh(assignOf);
  const rows = unassigned ? loaded.filter((r) => r.assignee === undefined) : loaded;
  const openCount = list.data?.pages[0]?.openCount;
  const claim = useWriteAction((key, t: CallbackTask) =>
    unwrap(
      assignCallbackTask({
        path: { id: t.id },
        body: { assigneeId: perms.userId, version: t.version },
        headers: headers(key),
      }),
    ),
  );
  const scrollRef = useRef<HTMLDivElement>(null);
  // eslint-disable-next-line react-hooks/incompatible-library -- useVirtualizer đổi kết quả theo lần cuộn; thành phần này không cần React Compiler ghi nhớ
  const v = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_ESTIMATE,
    getItemKey: (i) => rows[i]?.id ?? i,
    overscan: 6,
    measureElement: (el) => {
      const h = el.getBoundingClientRect().height;
      return h > 0 ? h : ROW_ESTIMATE;
    },
  });
  const items = v.getVirtualItems();
  const last = items.at(-1)?.index ?? -1;
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = list;
  useEffect(() => {
    // Cuộn gần cuối thì tải trang kế (còn nextCursor thì chưa coi là hết).
    if (hasNextPage && !isFetchingNextPage && last >= rows.length - 5) void fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage, last, rows.length]);

  const onClaim = (t: CallbackTask) => {
    void claim.run(t).then(() => {
      reloadOps(qc);
    });
  };
  const now = Date.now();
  const failed = list.isError && list.data === undefined ? errorMessage(list.error) : null;
  const sub = "Đơn giao thất bại cần gọi lại khách: quá hạn đứng đầu; tự tải lại khi có báo hiệu và mỗi 20 giây.";
  return (
    <OpsFrame title="Cần gọi lại" sub={sub}>
      <div className="ops-filters">
        <Seg label="Người làm" value={who} options={WHO_FILTERS} onChange={setWho} />
        <Seg label="Trạng thái" value={status} options={STATUS_FILTERS} onChange={setStatus} />
      </div>
      <ListState
        pending={list.isPending}
        error={failed}
        empty={!list.isPending && failed === null && rows.length === 0 && !hasNextPage}
        emptyTitle="Không có đơn cần gọi lại"
        emptyText="Mỗi lần hãng báo giao thất bại, đơn tự vào đây."
      />
      {claim.message !== null && (
        <div className="box box-err small" role="alert">
          {claim.message}
        </div>
      )}
      {!list.isPending && failed === null && rows.length > 0 && (
        <div className="cb-list" ref={scrollRef} data-testid="cblist">
          <div className="cb-body" style={{ height: v.getTotalSize() }}>
            {items.map((r) => {
              const t = rows[r.index];
              if (t === undefined) return null;
              return (
                <div
                  key={r.key}
                  className="cb-slot"
                  data-index={r.index}
                  ref={v.measureElement}
                  style={{ transform: `translateY(${String(r.start)}px)` }}
                >
                  <Row
                    t={t}
                    now={now}
                    meId={perms.userId}
                    canWork={perms.can("fulfillment.callbacks.work")}
                    canManage={perms.can("identity.users.manage")}
                    canReadOrder={perms.can("order.read")}
                    onOutcome={setOutcomeFor}
                    onAssign={setAssignFor}
                    onClaim={onClaim}
                    claiming={claim.pending}
                  />
                </div>
              );
            })}
          </div>
        </div>
      )}
      {!list.isPending && failed === null && rows.length > 0 && (
        <p className="ops-foot small muted">
          {`${(openCount ?? rows.length).toLocaleString("vi-VN")} việc mở${
            counts === undefined ? "" : ` · ${String(counts.callbacksOverdue)} quá hạn`
          } · đang hiện ${String(rows.length)}${hasNextPage ? ", cuộn để tải tiếp" : ""}. Sắp theo hạn gọi, quá hạn trước.`}
        </p>
      )}
      <p className="hint">
        Số điện thoại chỉ hiện khi bấm "Hiện số" (giảm lộ trên màn chia sẻ); nút "Gọi" mở ứng dụng điện thoại. Mỗi lần
        giao thất bại sinh một việc, hạn 24 giờ.
      </p>
      {outcomeFor !== null && (
        <CallbackOutcomeDialog
          task={outcomeFor}
          onClose={() => {
            setOutcomeFor(null);
          }}
        />
      )}
      {assignFor !== null && (
        <AssignDialog
          task={assignFor}
          onClose={() => {
            setAssignFor(null);
          }}
        />
      )}
    </OpsFrame>
  );
}
