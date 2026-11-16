// Calls into the Go API (internal/app.API). In the app, calls go through the
// Wails runtime over the wails:// scheme (no network). In a plain browser
// during development, a mock with synthetic data answers instead; the mock
// is never bundled into a production build.
import type {
  Action, DiskReport, LargeFile, MachineInfo, NotifyStatus, QuitResult, SafetyCheck,
  Settings, Status, Timeline, TrashPlan,
} from "./types";

const PREFIX = "github.com/Prime-Cave/headroom/internal/app.API.";

type Backend = {
  call<T>(method: string, ...args: unknown[]): Promise<T>;
  on(event: string, cb: (data: any) => void): () => void;
};

const inApp = window.location.protocol === "wails:";

let backendPromise: Promise<Backend> | null = null;

function backend(): Promise<Backend> {
  if (!backendPromise) {
    backendPromise = (async (): Promise<Backend> => {
      // import.meta.env.DEV is false in production builds, so this branch and
      // the mock module are removed from the bundle entirely.
      if (import.meta.env.DEV && !inApp) {
        const mock = await import("./mock");
        return mock.backend;
      }
      const rt = await import("@wailsio/runtime");
      return {
        call: <T>(m: string, ...args: unknown[]) => rt.Call.ByName(PREFIX + m, ...args) as Promise<T>,
        on: (e, cb) => rt.Events.On(e, (ev: { data: any }) => cb(ev.data)),
      };
    })();
  }
  return backendPromise;
}

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const b = await backend();
  return b.call<T>(method, ...args);
}

export async function on(event: string, cb: (data: any) => void): Promise<() => void> {
  const b = await backend();
  return b.on(event, cb);
}

/** Turns a Go error into the user-facing message it carries. */
export function errorText(e: unknown): string {
  if (e && typeof e === "object" && "message" in e) {
    const m = String((e as { message: unknown }).message);
    try {
      const parsed = JSON.parse(m);
      if (parsed && typeof parsed.message === "string") return parsed.message;
    } catch {
      /* plain message */
    }
    return m;
  }
  return String(e);
}

export const api = {
  status: () => call<Status>("Status"),
  machine: () => call<MachineInfo>("Machine"),
  apps: () => call<import("./types").AppRow[]>("Apps"),
  timeline: (range: string) => call<Timeline>("Timeline", range),
  history: () => call<Action[] | null>("History"),
  settings: () => call<Settings>("Settings"),
  setSetting: (k: string, v: string) => call<Settings>("SetSetting", k, v),
  openDashboard: (section = "overview") => call<void>("OpenDashboard", section),
  hideDropdown: () => call<void>("HideDropdown"),
  resizeDropdown: (h: number) => call<void>("ResizeDropdown", h),
  panelOpened: () => call<void>("PanelOpened"),
  // Safety check and actions (section 5)
  prepareQuit: (appKey: string) => call<SafetyCheck>("PrepareQuit", appKey),
  confirmQuit: (token: string) => call<QuitResult>("ConfirmQuit", token),
  confirmForceQuit: (token: string, confirmation: string) => call<QuitResult>("ConfirmForceQuit", token, confirmation),
  openSafetyCheck: (appKey: string) => call<void>("OpenSafetyCheck", appKey),
  pendingSafetyCheck: () => call<string>("PendingSafetyCheck"),
  closeSafetyCheck: () => call<void>("CloseSafetyCheck"),
  diskReport: (scan: boolean) => call<DiskReport>("DiskReport", scan),
  largeFiles: (placePath: string) => call<LargeFile[] | null>("LargeFiles", placePath),
  planTrash: (paths: string[]) => call<TrashPlan>("PlanTrash", paths),
  confirmTrash: (token: string) => call<QuitResult>("ConfirmTrash", token),
  revealInFinder: (path: string) => call<void>("RevealInFinder", path),
  openTrash: () => call<void>("OpenTrash"),
  exportHistory: () => call<string>("ExportHistory"),
  deleteHistory: (confirmation: string) => call<void>("DeleteHistory", confirmation),
  notifyStatus: () => call<NotifyStatus>("NotificationStatus"),
  requestNotifications: () => call<NotifyStatus>("RequestNotifications"),
  openNotificationSettings: () => call<void>("OpenNotificationSettings"),
  appIcon: (bundlePath: string) => call<string>("AppIcon", bundlePath),
  resizeSafety: (h: number) => call<void>("ResizeSafety", h),
};

// Used only by `headroom --selftest`: native code calls this to have the page
// report the text it rendered. It can only send text to the app itself.
(window as any).__headroomReport = () =>
  call<void>("SelftestReport", document.body.className, document.body.innerText).catch(() => {});
