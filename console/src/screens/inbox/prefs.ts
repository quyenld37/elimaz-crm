// Thiết lập riêng của máy cho Hộp chat (localStorage; máy chặn lưu thì chỉ nhớ trong lần mở này):
// - bộ lọc lần trước (HV06 đã sửa);
// - âm báo tin mới (HV04 đã sửa): tắt mặc định; một tiếng ngắn dựng bằng Web Audio (không tệp âm thanh). Trình duyệt
//   không có Web Audio hay chặn âm thanh khi chưa bấm gì: im lặng.
import { FILTERS_STORAGE_KEY, SOUND_STORAGE_KEY } from "./inboxRules";

/** Chuỗi tham số bộ lọc đã lưu ("" khi chưa có). */
export function readSavedFilters(): string {
  try {
    return localStorage.getItem(FILTERS_STORAGE_KEY) ?? "";
  } catch {
    return "";
  }
}

export function saveFilters(search: string): void {
  try {
    localStorage.setItem(FILTERS_STORAGE_KEY, search);
  } catch {
    // Máy chặn lưu: bỏ qua.
  }
}

export function readSound(): boolean {
  try {
    return localStorage.getItem(SOUND_STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

export function writeSound(on: boolean): void {
  try {
    localStorage.setItem(SOUND_STORAGE_KEY, on ? "1" : "0");
  } catch {
    // Máy chặn lưu (chế độ riêng tư): chỉ nhớ trong lần mở này.
  }
}

let ctx: AudioContext | null = null;

/** Kêu một tiếng ngắn (0,15 s, 880 Hz, nhỏ). */
export function beep(): void {
  try {
    if (typeof AudioContext === "undefined") return;
    ctx ??= new AudioContext();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.frequency.value = 880;
    gain.gain.value = 0.08;
    osc.connect(gain).connect(ctx.destination);
    osc.start();
    osc.stop(ctx.currentTime + 0.15);
  } catch {
    // Trình duyệt chặn âm thanh: bỏ qua.
  }
}
