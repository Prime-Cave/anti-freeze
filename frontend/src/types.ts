// Mirrors the Go JSON types in internal/analyzer, internal/app and internal/store.

export type StateName = "ok" | "tight" | "critical";

export interface SpareRoom {
  total: number;
  free: number;
  reclaimable: number;
  purgeable: number;
  swapAllowance: number;
}

export interface Forecast {
  confident: boolean;
  eta: number;
  minutes: number;
  cause?: string;
  text: string;
}

export interface Runway {
  memTotal: number;
  inUse: number;
  squeezed: number;
  spare: number;
  overflow: number;
  diskFree: number;
  diskTotal: number;
  memKnown: boolean;
  diskKnown: boolean;
  swapKnown: boolean;
}

export interface AppRow {
  key: string;
  name: string;
  bundleId?: string;
  bundlePath?: string;
  count: number;
  footprint: number;
  known: boolean;
  growth: number;
  growthSpanSec: number;
  cpu: number;
  diskRate: number;
  protected: boolean;
  reason?: string;
  mainPid?: number;
}

export interface Suggestion {
  kind: "quit" | "disk";
  text: string;
  appKey?: string;
  appName?: string;
  shortName?: string;
  frees?: number;
  action: string;
}

export interface Status {
  time: string;
  ready: boolean;
  state: StateName;
  label: string;
  rawState: StateName;
  causes: string[] | null;
  spareRoom: SpareRoom;
  spareRoomText: string;
  forecast: Forecast;
  runway: Runway;
  cpu: number;
  thermal: string;
  pressure: string;
  topApps: AppRow[] | null;
  suggestions: Suggestion[] | null;
  unavailable?: string[] | null;
  thresholds?: Thresholds;
}

export interface Thresholds {
  diskReserve: number;
  diskFloor: number;
  swapBudget: number;
  swapGrowthTight: number;
  forecastCritical: number;
  learned?: boolean;
}

export interface Profile {
  model: string;
  modelName: string;
  chip: string;
  arch: string;
  osVersion: string;
  memoryBytes: number;
  diskBytes: number;
  cores: number;
  firstSeen: string;
}

export interface MachineInfo {
  profile: Profile;
  line: string;
  demo: boolean;
}

export interface Settings {
  startAtLogin: boolean;
  sensitivity: "relaxed" | "balanced" | "early";
  cooldownMinutes: number;
  iconStyle: "fill" | "simple";
  retentionDays: number;
  showForceQuit: boolean;
  notifications: boolean;
  loginAvailable?: boolean;
  loginNeedsApproval?: boolean;
}

export interface Point {
  time: string;
  memUsed: number;
  memTotal: number;
  swapUsed: number;
  diskFree: number;
  spareRoom: number;
  state: number;
  cpu: number;
  topApp: string;
}

export interface EpisodeApp {
  name: string;
  footprint: number;
  growth: number;
}

export interface Episode {
  id: number;
  start: string;
  end: string;
  startSwap: number;
  peakSwap: number;
  startDiskFree: number;
  minDiskFree: number;
  causes: string[] | null;
  topFootprint: EpisodeApp[] | null;
  topGrowth: EpisodeApp[] | null;
}

export interface Timeline {
  range: string;
  from: string;
  to: string;
  points: Point[] | null;
  episodes: Episode[] | null;
  summary: string;
  slowdownLine: number;
  memTotal: number;
  empty: boolean;
}

export interface Action {
  id: number;
  time: string;
  kind: string;
  target: string;
  freed: number;
  detail: string;
}

export interface SafetyCheck {
  token: string;
  appName: string;
  shortName: string;
  busy: boolean;
  cpu: number;
  diskRate: number;
  windowSec: number;
  children: number;
  frees: number;
  evidence: string[];
  canForceQuit: boolean;
}

export interface QuitResult {
  ok: boolean;
  message: string;
  freed: number;
}

export interface DiskItem {
  name: string;
  path: string;
  size: number;
  known: boolean;
  note?: string;
  kind: "downloads" | "caches" | "trash" | "developer" | "backups";
  canReveal: boolean;
  canReview: boolean;
}

export interface DiskReport {
  scanned: string;
  items: DiskItem[];
  total: number;
}

export interface LargeFile {
  path: string;
  name: string;
  size: number;
  modified: string;
}

export interface TrashPlan {
  token: string;
  names: string[];
  total: number;
  count: number;
}

export interface NotifyStatus {
  authorized: boolean;
  determined: boolean;
  available: boolean;
}
