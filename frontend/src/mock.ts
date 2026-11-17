// Development-only mock backend with synthetic data, so the UI can be built
// and checked in a plain browser (`npx vite`, then ?state=ok|tight|critical|
// loading|na). Never included in production builds.
import type {
  AppRow, DiskReport, Episode, MachineInfo, Point, SafetyCheck, Settings, Status, Timeline,
} from "./types";

const GB = 1024 ** 3;
const MB = 1024 ** 2;
const params = new URLSearchParams(location.search);
const stage = params.get("state") ?? "ok";

function app(name: string, count: number, fp: number, growth: number, cpu: number, extra: Partial<AppRow> = {}): AppRow {
  return { key: "/Applications/" + name + ".app", name, count, footprint: fp, known: true, growth, growthSpanSec: 3600,
    cpu, diskRate: 0, protected: false, mainPid: 100, ...extra };
}

const appsOK: AppRow[] = [
  app("Google Chrome", 23, 1.6 * GB, 120 * MB, 8.2),
  app("Slack", 6, 740 * MB, 10 * MB, 1.1),
  app("Code", 14, 690 * MB, 40 * MB, 3.4),
  app("Spotify", 5, 310 * MB, 0, 0.6),
  { ...app("WindowServer", 1, 280 * MB, 0, 6.0), protected: true, reason: "Part of macOS", key: "proc:WindowServer" },
  { ...app("kernel_task", 1, 0, 0, 0), protected: true, reason: "Owned by the system or another user", known: false, key: "proc:kernel_task" },
];
const appsTight: AppRow[] = [
  app("Google Chrome", 41, 3.1 * GB, 1.9 * GB, 34.0),
  app("Slack", 6, 820 * MB, 60 * MB, 2.0),
  app("Code", 14, 710 * MB, 30 * MB, 4.1),
  app("Claude", 7, 520 * MB, 210 * MB, 1.2),
  ...appsOK.slice(4),
];

function status(): Status {
  const base = {
    time: new Date().toISOString(), ready: stage !== "loading", causes: [], cpu: 18, thermal: "Normal", pressure: "normal",
    unavailable: [] as string[],
  };
  if (stage === "tight" || stage === "critical") {
    const crit = stage === "critical";
    return {
      ...base, state: stage, label: crit ? "Slowdown likely" : "Getting tight", rawState: stage,
      spareRoom: { total: crit ? 0.6 * GB : 1.4 * GB, free: 0.2 * GB, reclaimable: 0.4 * GB, purgeable: 0, swapAllowance: 0.8 * GB },
      spareRoomText: crit ? "614 MB" : "1.4 GB",
      forecast: crit ? { confident: true, eta: 3e11, minutes: 4, text: "Slowdowns likely in about 4 minutes." }
        : { confident: true, eta: 9e11, minutes: 15, text: "Slowdowns likely in about 15 minutes." },
      runway: { memTotal: 8 * GB, inUse: 5.3 * GB, squeezed: 2.1 * GB, spare: 0.6 * GB, overflow: crit ? 3.4 * GB : 2.2 * GB,
        diskFree: 9 * GB, diskTotal: 245e9, memKnown: true, diskKnown: true, swapKnown: true },
      cpu: 41, thermal: crit ? "Fair" : "Normal", pressure: crit ? "critical" : "warn",
      topApps: appsTight.slice(0, 4),
      suggestions: [
        { kind: "quit", text: "Google Chrome grew by 1.9 GB in the last hour. Quitting it frees about 3.1 GB.",
          appKey: appsTight[0].key, appName: "Google Chrome", shortName: "Chrome", frees: 3.1 * GB, action: "Quit Chrome…" },
        { kind: "disk", text: "Only 9 GB of disk is left, and overflow needs room to grow. Downloads holds 6.2 GB.", action: "Review large files" },
      ],
    };
  }
  const na = stage === "na";
  return {
    ...base, state: "ok", label: "Plenty of room", rawState: "ok",
    spareRoom: { total: 2.4 * GB, free: 0.5 * GB, reclaimable: 0.9 * GB, purgeable: 0, swapAllowance: GB },
    spareRoomText: "2.4 GB", forecast: { confident: false, eta: 0, minutes: 0, text: "Steady. Nothing is filling up fast." },
    runway: { memTotal: 8 * GB, inUse: 4.1 * GB, squeezed: 1.2 * GB, spare: 2.7 * GB, overflow: GB, diskFree: 38 * GB,
      diskTotal: 245e9, memKnown: true, diskKnown: !na, swapKnown: true },
    cpu: 12, topApps: appsOK.slice(0, 4), suggestions: [],
    unavailable: na ? ["disk", "cpu"] : [],
  };
}

function timeline(range: string): Timeline {
  const spans: Record<string, number> = { "1h": 3600e3, "24h": 86400e3, "7d": 7 * 86400e3, "30d": 30 * 86400e3 };
  const span = spans[range] ?? spans["24h"];
  const to = Date.now();
  const from = to - span;
  const n = 240;
  const pts: Point[] = [];
  const episodes: Episode[] = [];
  const epAt = [0.35, 0.78];
  for (let i = 0; i < n; i++) {
    const t = from + (span * i) / (n - 1);
    const x = i / (n - 1);
    let demand = 5.4 * GB + 0.9 * GB * Math.sin(x * 9) + 0.4 * GB * Math.sin(x * 37);
    for (const e of epAt) demand += 6.5 * GB * Math.exp(-((x - e) ** 2) / 0.0006);
    const swap = Math.max(0.4 * GB, demand - 6.8 * GB);
    const memUsed = Math.min(7.6 * GB, demand);
    pts.push({ time: new Date(t).toISOString(), memUsed, memTotal: 8 * GB, swapUsed: swap, diskFree: 14 * GB - swap * 0.6,
      spareRoom: Math.max(0, 12 * GB - memUsed - swap), state: memUsed + swap > 11.5 * GB ? 2 : memUsed + swap > 10 * GB ? 1 : 0,
      cpu: 20, topApp: i % 3 ? "Google Chrome" : "Slack" });
  }
  for (const e of epAt) {
    const s = from + span * (e - 0.02);
    episodes.push({ id: episodes.length + 1, start: new Date(s).toISOString(), end: new Date(s + span * 0.04).toISOString(),
      startSwap: 2.6 * GB, peakSwap: 5.2 * GB, startDiskFree: 11.2 * GB, minDiskFree: 9 * GB, causes: ["pressure-critical"],
      topFootprint: [{ name: "Google Chrome", footprint: 3.2 * GB, growth: 1.4 * GB }], topGrowth: null });
  }
  const empty = params.get("empty") === "1";
  return { range, from: new Date(from).toISOString(), to: new Date(to).toISOString(), points: empty ? [] : pts,
    episodes: empty ? [] : episodes, empty,
    summary: empty ? "No slowdowns in this period." : "Two slowdowns. Both started when Google Chrome passed 3 GB while disk was under 12 GB free.",
    slowdownLine: 12 * GB, memTotal: 8 * GB };
}

let settings: Settings = { startAtLogin: true, sensitivity: "balanced", cooldownMinutes: 20, iconStyle: "fill",
  retentionDays: 90, showForceQuit: false, notifications: true };

const handlers: Record<string, (...a: any[]) => unknown> = {
  Status: status,
  Machine: (): MachineInfo => ({ profile: { model: "Demo", modelName: "MacBook Air", chip: "Apple M1", arch: "arm64",
    osVersion: "14.6", memoryBytes: 8 * GB, diskBytes: 245e9, cores: 8, firstSeen: new Date(Date.now() - 7 * 86400e3).toISOString() },
    line: "MacBook Air, 8 GB memory, 256 GB disk. Watching since 9 Nov.", demo: true }),
  Apps: () => (stage === "ok" ? appsOK : appsTight),
  Timeline: timeline,
  History: () => [
    { id: 2, time: new Date(Date.now() - 3600e3).toISOString(), kind: "quit", target: "Google Chrome", freed: 3.1 * GB, detail: "Asked to quit normally" },
    { id: 1, time: new Date(Date.now() - 86400e3).toISOString(), kind: "trash", target: "2 items from Downloads", freed: 4.2 * GB, detail: "" },
  ],
  Settings: () => settings,
  SetSetting: (k: string, v: string) => {
    settings = { ...settings, [k]: v === "true" ? true : v === "false" ? false : isNaN(+v) ? v : +v };
    return settings;
  },
  PrepareQuit: (key: string): SafetyCheck => {
    const busy = params.get("busy") !== "0";
    const a = [...appsTight, ...appsOK].find((x) => x.key === key) ?? appsTight[0];
    const short = a.name.replace(/^Google /, "");
    return { token: "t", appName: a.name, shortName: short, busy, cpu: busy ? 38 : 2, diskRate: busy ? 3.2 * MB : 0,
      windowSec: 30, children: busy ? 2 : 0, frees: a.footprint,
      evidence: busy ? ["Processor use is 38% over the last 30 seconds.", "It is writing files right now."] : [], canForceQuit: false };
  },
  ConfirmQuit: () => ({ ok: true, message: "Chrome quit. 3.1 GB freed.", freed: 3.1 * GB }),
  DiskReport: (): DiskReport => ({ scanned: new Date().toISOString(), total: 14.9 * GB, items: [
    { name: "Downloads", path: "~/Downloads", size: 6.2 * GB, known: true, kind: "downloads", canReveal: true, canReview: true },
    { name: "App caches", path: "~/Library/Caches", size: 2.9 * GB, known: true, kind: "caches", canReveal: true, canReview: false },
    { name: "Trash", path: "~/.Trash", size: 0, known: false, note: "Needs Full Disk Access to measure. Anti-Freeze doesn't ask for it.", kind: "trash", canReveal: false, canReview: false },
    { name: "Developer leftovers", path: "~/Library/Developer", size: 5.8 * GB, known: true, note: "Xcode DerivedData and simulators", kind: "developer", canReveal: true, canReview: true },
  ] }),
  LargeFiles: () => [
    { path: "/x/a.dmg", name: "Xcode_15.dmg", size: 3.4 * GB, modified: new Date(Date.now() - 40 * 86400e3).toISOString() },
    { path: "/x/b.zip", name: "photos-export.zip", size: 1.9 * GB, modified: new Date(Date.now() - 12 * 86400e3).toISOString() },
    { path: "/x/c.mov", name: "Screen Recording.mov", size: 640 * MB, modified: new Date(Date.now() - 3 * 86400e3).toISOString() },
  ],
  PlanTrash: (paths: string[]) => ({ token: "t", names: paths.map((p) => p.split("/").pop()), total: 5.3 * GB, count: paths.length }),
  ConfirmTrash: () => ({ ok: true, message: "Moved 2 items to the Trash. 5.3 GB freed.", freed: 5.3 * GB }),
  NotificationStatus: () => ({ authorized: params.get("notify") !== "0", determined: true, available: true }),
  ExportHistory: () => "/Users/demo/Documents/Anti-Freeze history.csv",
  AppIcon: () => "",
  PendingSafetyCheck: () => appsTight[0].key,
};

export const backend = {
  async call<T>(method: string, ...args: unknown[]): Promise<T> {
    const fn = handlers[method];
    return (fn ? fn(...args) : undefined) as T;
  },
  on(event: string, cb: (d: any) => void) {
    if (event !== "status") return () => {};
    const id = setInterval(() => cb(status()), 2000);
    return () => clearInterval(id);
  },
};
