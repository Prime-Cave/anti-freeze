// Formatting shared by the dropdown and dashboard. Copy rules: plain words,
// sentence case; "overflow on disk" in the UI, "swap" only in tooltips.

const GB = 1024 ** 3;
const MB = 1024 ** 2;

export function bytes(b: number): string {
  if (!Number.isFinite(b)) return "—";
  const a = Math.abs(b);
  if (a >= 9.95 * GB) return `${Math.round(b / GB)} GB`;
  if (a >= 0.995 * GB) return `${(b / GB).toFixed(1)} GB`;
  return `${Math.round(b / MB)} MB`;
}

export function signedBytes(b: number): string {
  if (b === 0) return "0 MB";
  return (b > 0 ? "+" : "−") + bytes(Math.abs(b));
}

/** "Memory, 8 GB" — installed memory reads as marketed. */
export function capacity(b: number): string {
  const g = b / GB;
  const steps = [4, 8, 16, 18, 24, 32, 36, 48, 64, 96, 128, 192];
  let best = steps[0];
  for (const s of steps) if (g >= s * 0.85) best = s;
  return `${best} GB`;
}

export function percent(p: number): string {
  return `${Math.round(p)}%`;
}

export function rate(bps: number): string {
  if (bps < 1024) return "Idle";
  if (bps < MB) return `${Math.round(bps / 1024)} KB/s`;
  return `${(bps / MB).toFixed(1)} MB/s`;
}

export const NOT_AVAILABLE = "Not available on this Mac";

export const GROWTH_LARGE = 500 * MB;

export function time(t: string | Date): string {
  const d = typeof t === "string" ? new Date(t) : t;
  return d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}

export function dateTime(t: string | Date): string {
  const d = typeof t === "string" ? new Date(t) : t;
  return d.toLocaleString(undefined, { day: "numeric", month: "short", hour: "numeric", minute: "2-digit" });
}

export function initial(name: string): string {
  const m = name.trim().match(/[\p{L}\p{N}]/u);
  return m ? m[0].toUpperCase() : "?";
}
