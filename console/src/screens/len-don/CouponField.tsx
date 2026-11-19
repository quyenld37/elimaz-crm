// Ô mã giảm giá của màn Lên đơn (mockup docs/mockups/m2/len-don.html trạng thái A, F; HV10). Kiểm bằng checkCoupon khi
// gõ xong (rời ô hoặc Enter): không trừ lượt; lý do từ chối bằng chữ ngay dưới ô. Mỗi đơn tối đa một mã (Q27).
import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { checkCoupon, type CouponCheck } from "../../api";
import { unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { formatMoney } from "../products/productRules";
import { couponReasonText } from "./orderRules";

export const COUPON_TOPIC = "promotion.coupon";

export interface CouponQuery {
  /** Mã đã chốt (viết hoa); rỗng: không dùng mã. */
  code: string;
  /** Tạm tính sau giảm dòng và giảm tay cấp đơn; null khi chưa tính được. */
  base: number | null;
  customerId: string | null;
  phone: string | null;
}

/** Kết quả kiểm mã cho đơn đang soạn; undefined khi chưa có mã hay đang kiểm. */
export function useCouponCheck(q: CouponQuery): { check: CouponCheck | undefined; failed: boolean } {
  const r = useQuery({
    queryKey: queryKey(COUPON_TOPIC, "check", q),
    enabled: q.code !== "" && q.base !== null,
    queryFn: () =>
      unwrap(
        checkCoupon({
          query: {
            code: q.code,
            base: q.base ?? 0,
            channel: "manual",
            ...(q.customerId === null ? {} : { customerId: q.customerId }),
            ...(q.customerId === null && q.phone !== null ? { phone: q.phone } : {}),
          },
        }),
      ),
  });
  return { check: q.code === "" ? undefined : r.data, failed: r.isError };
}

export interface CouponFieldProps {
  text: string;
  onText: (s: string) => void;
  /** Chốt mã đang gõ (rời ô, Enter). */
  onCommit: () => void;
  onClear: () => void;
  check: CouponCheck | undefined;
  failed: boolean;
  /** Lý do máy chủ từ chối khi lưu (coupon_rejected), ưu tiên hơn kết quả kiểm trước. */
  rejectedReason: string | undefined | null;
}

export function CouponField({ text, onText, onCommit, onClear, check, failed, rejectedReason }: CouponFieldProps) {
  const id = useId();
  const err =
    rejectedReason !== null
      ? couponReasonText(rejectedReason)
      : check !== undefined && !check.ok
        ? couponReasonText(check.reason)
        : failed
          ? "Chưa kiểm được mã. Thử lại sau ít giây"
          : null;
  const ok = err === null && check?.ok === true;
  return (
    <div className="sum-r" style={{ alignItems: "flex-start" }}>
      <label htmlFor={id} style={{ paddingTop: 8 }}>
        Mã giảm giá
      </label>
      <div className="stack-sm" style={{ justifyItems: "end" }}>
        <div className="inline-f">
          <input
            id={id}
            className={err === null ? "input" : "input is-err"}
            maxLength={32}
            autoComplete="off"
            aria-invalid={err !== null}
            aria-describedby={`${id}-m`}
            value={text}
            onChange={(e) => {
              onText(e.target.value);
            }}
            onBlur={onCommit}
            onKeyDown={(e) => {
              // Ctrl/⌘+Enter là phím lưu (OrderForm): không chặn ở đây.
              if (e.key === "Enter" && !e.ctrlKey && !e.metaKey) {
                e.preventDefault();
                onCommit();
              }
            }}
          />
          {text !== "" && (
            <button type="button" className="btn btn-quiet" onClick={onClear}>
              Bỏ mã
            </button>
          )}
        </div>
        <span id={`${id}-m`}>
          {err !== null && <span className="field-err">{err}</span>}
          {ok && (
            <span className="small ok-text">
              {check.remainingUses === undefined
                ? `Áp được: giảm ${formatMoney(check.discount)}`
                : `Áp được: giảm ${formatMoney(check.discount)} · còn ${String(check.remainingUses)} lượt`}
            </span>
          )}
        </span>
      </div>
    </div>
  );
}
