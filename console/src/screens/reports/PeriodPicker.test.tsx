// @vitest-environment jsdom
// Chọn kỳ (Task 239, M6-16; mockup tong-quan.html trạng thái A, C): 5 lựa chọn; Tùy chọn có hai ô ngày và nút Xem;
// quá 366 ngày hay ngày tương lai khóa nút Xem và nói rõ lý do.
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PeriodPicker } from "./PeriodPicker";
import { presetPeriod, type Period } from "./period";

afterEach(cleanup);
const TODAY = "2026-11-27";

function setup(value: Period = presetPeriod("today", TODAY), disabled = false) {
  const onChange = vi.fn<(p: Period) => void>();
  render(<PeriodPicker value={value} today={TODAY} onChange={onChange} disabled={disabled} />);
  return onChange;
}

describe("PeriodPicker", () => {
  it("hiện năm lựa chọn, Hôm nay đang bấm", () => {
    setup();
    const names = ["Hôm nay", "Hôm qua", "7 ngày", "Tháng này", "Tùy chọn"];
    for (const n of names) expect(screen.getByRole("button", { name: n })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Hôm nay" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "Tùy chọn" }).getAttribute("aria-pressed")).toBe("false");
  });

  it("bấm 7 ngày → báo kỳ 21/11–27/11", () => {
    const onChange = setup();
    fireEvent.click(screen.getByRole("button", { name: "7 ngày" }));
    expect(onChange).toHaveBeenCalledWith({ preset: "7d", from: "2026-11-21", to: TODAY });
  });

  it("chọn 01/01/2026–03/01/2027 → nút Xem bị khóa, có lời giải thích số ngày", () => {
    const onChange = setup({ preset: "custom", from: "2026-11-01", to: TODAY });
    fireEvent.change(screen.getByLabelText("Từ ngày"), { target: { value: "2026-01-01" } });
    fireEvent.change(screen.getByLabelText("Đến ngày"), { target: { value: "2027-01-03" } });
    const see = screen.getByRole("button", { name: "Xem" });
    expect((see as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/Khoảng tối đa 366 ngày; đang chọn 368 ngày/)).toBeTruthy();
    fireEvent.click(see);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("ngày tương lai → khóa Xem; ô ngày không cho chọn quá hôm nay (max)", () => {
    setup({ preset: "custom", from: "2026-11-20", to: TODAY });
    expect(screen.getByLabelText("Đến ngày").getAttribute("max")).toBe(TODAY);
    fireEvent.change(screen.getByLabelText("Đến ngày"), { target: { value: "2026-11-28" } });
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Xem" }).disabled).toBe(true);
    expect(screen.getByText("Không chọn được ngày sau hôm nay (27/11/2026).")).toBeTruthy();
  });

  it("khoảng hợp lệ → Xem báo kỳ tùy chọn", () => {
    const onChange = setup({ preset: "custom", from: "2026-11-01", to: TODAY });
    fireEvent.change(screen.getByLabelText("Từ ngày"), { target: { value: "2026-11-05" } });
    fireEvent.click(screen.getByRole("button", { name: "Xem" }));
    expect(onChange).toHaveBeenCalledWith({ preset: "custom", from: "2026-11-05", to: TODAY });
  });

  it("bấm Tùy chọn mở hai ô ngày (chưa gọi báo cáo)", () => {
    const onChange = setup();
    fireEvent.click(screen.getByRole("button", { name: "Tùy chọn" }));
    expect(screen.getByLabelText("Từ ngày")).toBeTruthy();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("đang tải (disabled) → mọi nút khóa", () => {
    setup(presetPeriod("today", TODAY), true);
    for (const b of screen.getAllByRole("button")) expect((b as HTMLButtonElement).disabled).toBe(true);
  });
});
