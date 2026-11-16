// The dashboard window (section 7.6): sidebar plus Overview, Memory, Apps,
// Disk, Processor and heat, History and Settings.
import { api, errorText, on } from "./api";
import { add, clear, h } from "./dom";
import { appAvatar } from "./avatar";
import { timelineChart } from "./chart";
import { bytes, dateTime, GROWTH_LARGE, NOT_AVAILABLE, percent, rate, signedBytes } from "./format";
import { runway } from "./runway";
import type {
  Action, AppRow, DiskItem, DiskReport, LargeFile, MachineInfo, NotifyStatus, Settings, Status, Timeline,
} from "./types";

const root = document.getElementById("app")!;

type SectionId = "overview" | "memory" | "apps" | "disk" | "cpu" | "history" | "settings";
const SECTIONS: [SectionId, string][] = [
  ["overview", "Overview"], ["memory", "Memory"], ["apps", "Apps"], ["disk", "Disk"],
  ["cpu", "Processor and heat"], ["history", "History"], ["settings", "Settings"],
];

let status: Status | null = null;
let machine: MachineInfo | null = null;
let settings: Settings | null = null;
let current: SectionId = "overview";
const iconCache = new Map<string, string>();

// ---- shell ------------------------------------------------------------------------

const nav = h("nav", { class: "side", "aria-label": "Sections" });
const navButtons = new Map<SectionId, HTMLElement>();
for (const [id, label] of SECTIONS) {
  const b = h("button", { class: "nav-item", type: "button", "data-id": id, on: { click: () => go(id) } }, label);
  navButtons.set(id, b);
  nav.append(b);
}
const demoTag = h("div", { class: "demo-tag side-demo", hidden: true }, "Demo data");
nav.append(demoTag);

const main = h("main", { class: "main", tabindex: "-1" });
const toast = h("div", { class: "toast dash-toast", role: "status", "aria-live": "polite", hidden: true });
root.append(nav, main, toast);

// Keyboard: up/down moves between sections in the sidebar.
nav.addEventListener("keydown", (e) => {
  const ids = SECTIONS.map((s) => s[0]);
  const i = ids.indexOf(current);
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault();
    const next = ids[(i + (e.key === "ArrowDown" ? 1 : ids.length - 1)) % ids.length];
    go(next);
    navButtons.get(next)!.focus();
  }
});

const pages = new Map<SectionId, HTMLElement>();
function page(id: SectionId, title: string): HTMLElement {
  const p = h("section", { class: "page", "aria-labelledby": `h-${id}`, hidden: true },
    h("h1", { id: `h-${id}`, class: "page-title" }, title));
  pages.set(id, p);
  main.append(p);
  return p;
}

function go(id: SectionId) {
  if (!pages.has(id)) id = "overview";
  current = id;
  for (const [pid, p] of pages) p.hidden = pid !== id;
  for (const [bid, b] of navButtons) {
    b.classList.toggle("active", bid === id);
    if (bid === id) b.setAttribute("aria-current", "page");
    else b.removeAttribute("aria-current");
  }
  main.scrollTop = 0;
  onShow[id]?.();
}

let toastTimer = 0;
function showToast(msg: string) {
  toast.textContent = msg;
  toast.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => (toast.hidden = true), 6000);
}
const fail = (e: unknown) => showToast(errorText(e));

function card(title: string | null, ...children: (Node | null)[]): HTMLElement {
  const c = h("div", { class: "dcard" });
  if (title) c.append(h("h2", null, title));
  add(c, ...children);
  return c;
}

function skeleton(): HTMLElement {
  return h("div", { class: "skeleton", "aria-busy": "true", "aria-label": "Loading" },
    h("span", { class: "sk sk-title" }), h("span", { class: "sk sk-big" }), h("span", { class: "sk sk-bar" }),
    h("span", { class: "sk sk-line" }), h("span", { class: "sk sk-line short" }));
}

// ---- overview --------------------------------------------------------------------

const ov = page("overview", "Overview");
const ovSkel = skeleton();
const ovDot = h("span", { class: "dot", "aria-hidden": "true" });
const ovLabel = h("span", { class: "ov-label" });
const ovFigure = h("span", { class: "figure56 num" });
const ovForecast = h("p", { class: "ov-forecast", "aria-live": "polite" });
const ovMachine = h("p", { class: "ov-machine" });
const ovRunway = runway({ tall: true });
const ovStatus = h("div", { class: "ov-status" },
  h("div", { class: "ov-head" }, ovDot, ovLabel),
  h("div", { class: "figure-line" }, ovFigure, h("span", { class: "unit" }, "to spare")),
  ovForecast, ovMachine);
const ovStatusCard = card(null, ovStatus, ovRunway.root);
ovStatusCard.hidden = true;

const RANGES: [string, string][] = [["1h", "1 hour"], ["24h", "24 hours"], ["7d", "7 days"], ["30d", "30 days"]];
let range = "24h";
const tlSummary = h("p", { class: "tl-summary" });
const tlEmpty = h("p", { class: "empty", hidden: true }, "History starts now. Check back in an hour to see your first trend.");
const chart = timelineChart();
const rangeGroup = h("div", { class: "seg-toggle", role: "radiogroup", "aria-label": "Time range" });
for (const [k, label] of RANGES) {
  rangeGroup.append(h("button", { type: "button", role: "radio", "data-range": k, "aria-checked": String(k === range),
    on: { click: () => { range = k; loadTimeline(); } } }, label));
}
const tlCard = card(null,
  h("div", { class: "card-head" }, h("h2", null, "Timeline"), rangeGroup),
  tlSummary, tlEmpty, chart.root);

const ovApps = h("tbody");
const ovAppsCard = card(null,
  h("div", { class: "card-head" }, h("h2", null, "Apps by memory"),
    h("button", { class: "btn secondary", type: "button", on: { click: () => go("apps") } }, "All apps")),
  h("table", { class: "apps-table compact" }, appsHead(false), ovApps));

ov.append(ovSkel, ovStatusCard, tlCard, ovAppsCard);

function renderStatus() {
  const st = status;
  if (!st || !st.ready) return;
  ovSkel.hidden = true;
  ovStatusCard.hidden = false;
  ovStatus.className = "ov-status " + st.state;
  ovLabel.textContent = st.label;
  ovFigure.textContent = st.runway.memKnown ? st.headroomText : "—";
  ovForecast.textContent = st.runway.memKnown ? st.forecast.text : NOT_AVAILABLE;
  ovRunway.update(st.runway);
  renderMemory();
  renderCpu();
}

async function loadTimeline() {
  rangeGroup.querySelectorAll("button").forEach((b) => b.setAttribute("aria-checked", String(b.dataset.range === range)));
  try {
    const t: Timeline = await api.timeline(range);
    tlSummary.textContent = t.empty ? "" : t.summary;
    tlEmpty.hidden = !t.empty;
    chart.root.hidden = t.empty;
    if (!t.empty) chart.update(t);
  } catch (e) {
    fail(e);
  }
}

// The Memory and Processor sections always show the last 24 hours.
async function loadDay() {
  try {
    const t: Timeline = await api.timeline("24h");
    memChartData(t);
    cpuChartData(t);
  } catch (e) {
    fail(e);
  }
}

// ---- apps table --------------------------------------------------------------------

type SortKey = "name" | "footprint" | "growth" | "cpu";
let sortKey: SortKey = "footprint";
let sortDesc = true;
let apps: AppRow[] = [];

function appsHead(sortable: boolean): HTMLElement {
  const cols: [SortKey | null, string, string][] = [
    ["name", "App", "col-app"], ["footprint", "Memory", "col-num"], ["growth", "Last hour change", "col-num"],
    ["cpu", "Processor", "col-num"], [null, "", "col-act"],
  ];
  return h("thead", null, h("tr", null, ...cols.map(([k, label, cls]) => {
    if (!sortable || !k) return h("th", { scope: "col", class: cls }, label);
    const active = k === sortKey;
    return h("th", { scope: "col", class: cls, "aria-sort": active ? (sortDesc ? "descending" : "ascending") : "none" },
      h("button", { class: "sort" + (active ? " active" : ""), type: "button", on: { click: () => {
        if (sortKey === k) sortDesc = !sortDesc;
        else { sortKey = k; sortDesc = k !== "name"; }
        renderApps();
      } } }, label, h("span", { class: "arrow", "aria-hidden": "true" }, active ? (sortDesc ? "↓" : "↑") : "")));
  })));
}

function appRow(a: AppRow): HTMLElement {
  const growth = a.growth > 0 ? signedBytes(a.growth) : a.growth < 0 ? signedBytes(a.growth) : "—";
  const action = a.protected
    ? h("span", { class: "badge", title: a.reason ?? "Part of macOS" }, "Protected")
    : h("button", { class: "btn secondary small", type: "button", "aria-label": `Quit ${a.name}…`,
      on: { click: () => api.openSafetyCheck(a.key).catch(fail) } }, "Quit…");
  return h("tr", null,
    h("td", { class: "col-app" }, h("span", { class: "app-cell" }, appAvatar(a, iconCache),
      h("span", { class: "nm" }, a.name), h("span", { class: "ct num" }, `(${a.count})`))),
    h("td", { class: "col-num num" }, a.known ? bytes(a.footprint) : "—"),
    h("td", { class: "col-num num" + (a.growth >= GROWTH_LARGE ? " growth-large" : "") }, growth),
    h("td", { class: "col-num num" }, percent(a.cpu)),
    h("td", { class: "col-act" }, action));
}

const ap = page("apps", "Apps");
const apHeadSlot = h("thead");
const apBody = h("tbody");
const apTable = h("table", { class: "apps-table" }, apHeadSlot, apBody);
ap.append(card(null, h("p", { class: "lead" },
  "Each app with all its helper processes. Quitting always opens a safety check first. Apps that are part of macOS are protected."), apTable));

function sorted(rows: AppRow[]): AppRow[] {
  const dir = sortDesc ? -1 : 1;
  return [...rows].sort((a, b) => {
    const va = a[sortKey], vb = b[sortKey];
    if (typeof va === "string" && typeof vb === "string") return va.localeCompare(vb) * dir;
    return ((va as number) - (vb as number)) * dir;
  });
}

function renderApps() {
  apTable.querySelector("thead")!.replaceWith(appsHead(true));
  clear(apBody);
  for (const a of sorted(apps)) apBody.append(appRow(a));
  clear(ovApps);
  for (const a of apps.slice(0, 6)) ovApps.append(appRow(a));
}

async function loadApps() {
  try {
    apps = (await api.apps()) ?? [];
    renderApps();
    renderCpuApps();
  } catch (e) {
    fail(e);
  }
}

// ---- memory -----------------------------------------------------------------------

const breakEls = { free: h("dd", { class: "num" }), recl: h("dd", { class: "num" }), swap: h("dd", { class: "num" }), total: h("dd", { class: "num" }) };
const mem = page("memory", "Memory");
const memRunway = runway({ tall: true });
const memFacts = h("dl", { class: "facts" });
const memChart = timelineChart();
mem.append(
  card("Right now", memRunway.root, memFacts),
  card("How much room is left", h("p", { class: "lead" },
    "Headroom adds up memory that is free now, memory macOS can take back from caches, and how far overflow can grow on disk before things slow down."),
  memBreak()),
  card("Last 24 hours", memChart.root),
);

function memBreak(): HTMLElement {
  return h("dl", { class: "facts" },
    h("dt", null, "Free now"), breakEls.free,
    h("dt", null, "Can be taken back from caches"), breakEls.recl,
    h("dt", { title: "Swap" }, "Room for overflow on disk"), breakEls.swap,
    h("dt", { class: "total" }, "To spare"), breakEls.total);
}

function fact(dl: HTMLElement, label: string, value: string, title?: string) {
  dl.append(h("dt", title ? { title } : null, label), h("dd", { class: "num" }, value));
}

function renderMemory() {
  const st = status;
  if (!st) return;
  memRunway.update(st.runway);
  clear(memFacts);
  const pressure: Record<string, string> = { normal: "Normal", warn: "Raised", critical: "High" };
  fact(memFacts, "Memory pressure", pressure[st.pressure] ?? NOT_AVAILABLE, "macOS's own measure of how hard memory is working");
  const th = st.thresholds;
  if (th?.swapBudget) fact(memFacts, "Overflow budget", bytes(th.swapBudget), "Swap Headroom allows before calling it tight. Learned from your Mac after a week.");
  if (th?.diskReserve) fact(memFacts, "Disk kept free", bytes(th.diskReserve), "Below this, overflow has no room to grow");
  const hr = st.headroom;
  const known = st.runway.memKnown;
  breakEls.free.textContent = known ? bytes(hr.free) : "—";
  breakEls.recl.textContent = known ? bytes(hr.reclaimable + hr.purgeable) : "—";
  breakEls.swap.textContent = st.runway.swapKnown ? bytes(hr.swapAllowance) : "—";
  breakEls.total.textContent = known ? st.headroomText : "—";
}

const memEmpty = h("p", { class: "empty", hidden: true }, "History starts now. Check back in an hour to see your first trend.");
memChart.root.before(memEmpty);
function memChartData(t: Timeline) {
  memEmpty.hidden = !t.empty;
  memChart.root.hidden = t.empty;
  if (!t.empty) memChart.update(t);
}

// ---- processor and heat -------------------------------------------------------------

const cpu = page("cpu", "Processor and heat");
const cpuFacts = h("dl", { class: "facts" });
const cpuSpark = h("div", { class: "spark", role: "img" });
const cpuBody = h("tbody");
cpu.append(
  card("Right now", cpuFacts),
  card("Processor use, last 24 hours", cpuSpark),
  card("Busiest apps", h("table", { class: "apps-table compact" },
    h("thead", null, h("tr", null, h("th", { scope: "col" }, "App"), h("th", { scope: "col", class: "col-num" }, "Processor"),
      h("th", { scope: "col", class: "col-num" }, "Disk activity"))), cpuBody)),
);

function renderCpu() {
  const st = status;
  if (!st) return;
  clear(cpuFacts);
  const na = (st.unavailable ?? []).includes("cpu");
  fact(cpuFacts, "Processor", na ? NOT_AVAILABLE : percent(st.cpu), "Share of all processor cores in use");
  const heat: Record<string, string> = { Normal: "Normal", Fair: "Warm", Serious: "Hot, slowing down to cool", Critical: "Very hot, slowing down" };
  fact(cpuFacts, "Heat", heat[st.thermal] ?? st.thermal, "macOS thermal state");
  if (machine) fact(cpuFacts, "Chip", `${machine.profile.chip}, ${machine.profile.cores} cores`);
}

function renderCpuApps() {
  clear(cpuBody);
  for (const a of [...apps].sort((x, y) => y.cpu - x.cpu).slice(0, 8)) {
    cpuBody.append(h("tr", null,
      h("td", null, h("span", { class: "app-cell" }, appAvatar(a, iconCache), h("span", { class: "nm" }, a.name))),
      h("td", { class: "col-num num" }, percent(a.cpu)),
      h("td", { class: "col-num num" }, rate(a.diskRate))));
  }
}

function cpuChartData(t: Timeline) {
  const pts = t.points ?? [];
  clear(cpuSpark);
  if (!pts.length) {
    cpuSpark.append(h("p", { class: "empty" }, "History starts now. Check back in an hour to see your first trend."));
    return;
  }
  const w = 600, hh = 80;
  const t0 = new Date(t.from).getTime(), t1 = new Date(t.to).getTime();
  const d = pts.map((p, i) => `${i ? "L" : "M"}${(((new Date(p.time).getTime() - t0) / (t1 - t0)) * w).toFixed(1)},${(hh - (Math.min(100, p.cpu) / 100) * (hh - 4)).toFixed(1)}`).join("");
  const svgNS = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(svgNS, "svg");
  svg.setAttribute("viewBox", `0 0 ${w} ${hh}`);
  svg.setAttribute("preserveAspectRatio", "none");
  svg.setAttribute("aria-hidden", "true");
  const path = document.createElementNS(svgNS, "path");
  path.setAttribute("d", d);
  path.setAttribute("class", "spark-line");
  path.setAttribute("vector-effect", "non-scaling-stroke");
  svg.append(path);
  cpuSpark.append(svg);
  const peak = Math.max(...pts.map((p) => p.cpu));
  const avg = pts.reduce((a, p) => a + p.cpu, 0) / pts.length;
  cpuSpark.setAttribute("aria-label", `Processor use over the range: average ${percent(avg)}, highest ${percent(peak)}.`);
  cpuSpark.append(h("p", { class: "spark-caption" }, `Average ${percent(avg)}. Highest ${percent(peak)}.`));
}

// ---- disk ---------------------------------------------------------------------------

const disk = page("disk", "Disk");
const diskList = h("div", { class: "disk-list" });
const diskScanned = h("p", { class: "caption" });
const scanBtn = h("button", { class: "btn primary", type: "button", on: { click: () => loadDisk(true) } }, "Measure disk use");
disk.append(card(null,
  h("div", { class: "card-head" }, h("h2", null, "Where your disk went"), scanBtn),
  h("p", { class: "lead" }, "Headroom looks only at Downloads, app caches, the Trash and developer leftovers, and only when you ask."),
  diskScanned, diskList,
  h("p", { class: "footnote" }, "Headroom never deletes files. It moves what you pick to the Trash, so you can put it back.")));

let diskBusy = false;
async function loadDisk(scan: boolean) {
  if (diskBusy) return;
  diskBusy = true;
  scanBtn.setAttribute("disabled", "");
  if (scan) {
    clear(diskList);
    diskList.append(skeleton());
  }
  try {
    renderDisk(await api.diskReport(scan));
  } catch (e) {
    fail(e);
  } finally {
    diskBusy = false;
    scanBtn.removeAttribute("disabled");
  }
}

function renderDisk(r: DiskReport) {
  clear(diskList);
  scanBtn.textContent = r.scanned ? "Measure again" : "Measure disk use";
  diskScanned.textContent = r.scanned ? `Measured ${dateTime(r.scanned)}.` : "";
  if (!r.scanned) {
    diskList.append(h("p", { class: "empty" }, "Not measured yet. macOS may ask once to let Headroom read your Downloads folder."));
    return;
  }
  for (const it of r.items ?? []) diskList.append(diskRow(it));
}

function diskRow(it: DiskItem): HTMLElement {
  const actions = h("div", { class: "row-actions" });
  const reviewBox = h("div", { class: "review", hidden: true });
  if (it.kind === "trash") {
    actions.append(h("button", { class: "btn secondary small", type: "button", on: { click: () => api.openTrash().catch(fail) } }, "Open Trash"));
  } else {
    if (it.canReveal) actions.append(h("button", { class: "btn secondary small", type: "button",
      on: { click: () => api.revealInFinder(it.path).catch(fail) } }, "Show in Finder"));
    if (it.canReview) actions.append(h("button", { class: "btn secondary small", type: "button", "aria-expanded": "false",
      on: { click: (e: Event) => toggleReview(it, reviewBox, e.currentTarget as HTMLElement) } }, "Review…"));
  }
  return h("div", { class: "disk-item" },
    h("div", { class: "disk-row" },
      h("div", { class: "disk-name" }, h("span", { class: "nm" }, it.name),
        it.note ? h("span", { class: "caption" }, it.note) : null),
      h("span", { class: "disk-size num" }, it.known ? bytes(it.size) : "—"),
      actions),
    reviewBox);
}

async function toggleReview(it: DiskItem, box: HTMLElement, btn: HTMLElement) {
  if (!box.hidden) {
    box.hidden = true;
    btn.setAttribute("aria-expanded", "false");
    return;
  }
  box.hidden = false;
  btn.setAttribute("aria-expanded", "true");
  clear(box);
  box.append(skeleton());
  try {
    const files = (await api.largeFiles(it.path)) ?? [];
    renderReview(it, box, files);
  } catch (e) {
    clear(box);
    box.append(h("p", { class: "caption" }, errorText(e)));
  }
}

function renderReview(it: DiskItem, box: HTMLElement, files: LargeFile[]) {
  clear(box);
  if (!files.length) {
    box.append(h("p", { class: "caption" }, "Nothing here."));
    return;
  }
  const chosen = new Set<string>();
  const moveBtn = h("button", { class: "btn secondary small", type: "button", disabled: true,
    on: { click: () => planTrash(it, box, [...chosen]) } }, "Move to Trash…");
  const countEl = h("span", { class: "caption" }, "Pick the items you no longer need.");
  const list = h("ul", { class: "file-list" });
  for (const f of files) {
    const cb = h("input", { type: "checkbox", id: `f-${btoa(encodeURIComponent(f.path)).slice(0, 40)}` }) as HTMLInputElement;
    cb.addEventListener("change", () => {
      if (cb.checked) chosen.add(f.path); else chosen.delete(f.path);
      const total = files.filter((x) => chosen.has(x.path)).reduce((a, x) => a + x.size, 0);
      countEl.textContent = chosen.size ? `${chosen.size} picked, ${bytes(total)}.` : "Pick the items you no longer need.";
      (moveBtn as HTMLButtonElement).disabled = chosen.size === 0;
    });
    list.append(h("li", null,
      h("label", { for: cb.id }, cb, h("span", { class: "nm" }, f.name)),
      h("span", { class: "caption" }, new Date(f.modified).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })),
      h("span", { class: "num" }, bytes(f.size)),
      h("button", { class: "btn link-btn", type: "button", "aria-label": `Show ${f.name} in Finder`,
        on: { click: () => api.revealInFinder(f.path).catch(fail) } }, "Show")));
  }
  box.append(list, h("div", { class: "review-foot" }, countEl, moveBtn));
}

async function planTrash(it: DiskItem, box: HTMLElement, paths: string[]) {
  try {
    const plan = await api.planTrash(paths);
    box.querySelector(".confirm")?.remove();
    const confirm = h("div", { class: "confirm", role: "alertdialog", "aria-labelledby": "trash-q" },
      h("p", { id: "trash-q", class: "confirm-title" },
        `Move ${plan.count} ${plan.count === 1 ? "item" : "items"} (${bytes(plan.total)}) to the Trash?`),
      h("ul", { class: "names" }, ...plan.names.map((n) => h("li", null, n))),
      h("p", { class: "caption" }, "You can put them back from the Trash until you empty it."));
    const cancel = h("button", { class: "btn primary small", type: "button", on: { click: () => confirm.remove() } }, "Cancel");
    confirm.append(h("div", { class: "row-actions" },
      h("button", { class: "btn destructive small", type: "button", on: { click: async () => {
        try {
          const r = await api.confirmTrash(plan.token);
          showToast(r.message);
          loadDisk(true);
        } catch (e) {
          fail(e);
          confirm.remove();
        }
      } } }, "Move to Trash"),
      cancel));
    box.append(confirm);
    cancel.focus();
    void it;
  } catch (e) {
    fail(e);
  }
}

// ---- history ------------------------------------------------------------------------

const hist = page("history", "History");
const epList = h("div", { class: "episodes" });
const epSummary = h("p", { class: "lead" });
const actBody = h("tbody");
const actEmpty = h("p", { class: "empty", hidden: true }, "Nothing yet. When Headroom quits an app or moves files to the Trash for you, it's listed here.");
hist.append(
  card("Slowdowns, last 30 days", epSummary, epList),
  card("What Headroom did", actEmpty, h("table", { class: "apps-table compact" },
    h("thead", null, h("tr", null, ...["When", "Action", "What", "Freed"].map((c, i) => h("th", { scope: "col", class: i === 3 ? "col-num" : "" }, c)))),
    actBody)),
);

const KIND: Record<string, string> = { quit: "Quit", "force-quit": "Force quit", trash: "Moved to Trash", export: "Exported history" };

async function loadHistory() {
  try {
    const [t, acts] = await Promise.all([api.timeline("30d"), api.history()]);
    epSummary.textContent = t.empty ? "History starts now. Check back in an hour to see your first trend." : t.summary;
    clear(epList);
    for (const e of [...(t.episodes ?? [])].reverse()) {
      const top = (e.topFootprint ?? [])[0];
      const mins = Math.max(1, Math.round((new Date(e.end || Date.now()).getTime() - new Date(e.start).getTime()) / 60000));
      const lasted = mins < 60 ? `${mins} min` : `${Math.floor(mins / 60)} h${mins % 60 ? ` ${mins % 60} min` : ""}`;
      epList.append(h("div", { class: "episode" },
        h("span", { class: "ep-when" }, dateTime(e.start)),
        h("span", null, `Lasted ${lasted}. Overflow on disk peaked at ${bytes(e.peakSwap)}, ${bytes(e.minDiskFree)} disk free.` +
          (top ? ` Largest app: ${top.name}, ${bytes(top.footprint)}.` : ""))));
    }
    renderActions(acts ?? []);
  } catch (e) {
    fail(e);
  }
}

function renderActions(acts: Action[]) {
  clear(actBody);
  actEmpty.hidden = acts.length > 0;
  for (const a of acts) {
    actBody.append(h("tr", { title: a.detail },
      h("td", null, dateTime(a.time)), h("td", null, KIND[a.kind] ?? a.kind), h("td", null, a.target),
      h("td", { class: "col-num num" }, a.freed ? bytes(a.freed) : "—")));
  }
}

// ---- settings -----------------------------------------------------------------------

const set = page("settings", "Settings");
const setForm = h("div", { class: "settings" });
const notifyNote = h("div", { class: "note-box", hidden: true });
set.append(card(null, setForm));

function toggle(key: keyof Settings, label: string, help: string): HTMLElement {
  const id = `s-${key}`;
  const input = h("input", { type: "checkbox", role: "switch", id, class: "switch" }) as HTMLInputElement;
  input.checked = Boolean(settings?.[key]);
  input.addEventListener("change", () => save(key, String(input.checked)));
  return h("div", { class: "set-row" },
    h("div", { class: "set-text" }, h("label", { for: id }, label), h("p", { class: "caption" }, help)), input);
}

function choice(key: keyof Settings, label: string, help: string, opts: [string, string][]): HTMLElement {
  const group = h("div", { class: "seg-toggle", role: "radiogroup", "aria-label": label });
  for (const [v, l] of opts) {
    const b = h("button", { type: "button", role: "radio", "aria-checked": String(String(settings?.[key]) === v),
      on: { click: () => save(key, v) } }, l);
    group.append(b);
  }
  return h("div", { class: "set-row" }, h("div", { class: "set-text" }, h("span", { class: "lbl" }, label), h("p", { class: "caption" }, help)), group);
}

function select(key: keyof Settings, label: string, help: string, opts: [string, string][]): HTMLElement {
  const id = `s-${key}`;
  const sel = h("select", { id }) as HTMLSelectElement;
  for (const [v, l] of opts) sel.append(h("option", { value: v }, l));
  sel.value = String(settings?.[key]);
  sel.addEventListener("change", () => save(key, sel.value));
  return h("div", { class: "set-row" }, h("div", { class: "set-text" }, h("label", { for: id }, label), h("p", { class: "caption" }, help)), sel);
}

async function save(key: keyof Settings, value: string) {
  try {
    settings = await api.setSetting(key, value);
    renderSettings();
  } catch (e) {
    fail(e);
    renderSettings();
  }
}

function renderSettings() {
  if (!settings) return;
  const focusedId = (document.activeElement as HTMLElement | null)?.id;
  clear(setForm);
  setForm.append(
    h("h2", null, "General"),
    toggle("startAtLogin", "Start at login", "Headroom opens quietly in the menu bar when you log in."),
    choice("iconStyle", "Menu bar icon", "Fill shows how full memory is. Simple only changes when things get tight.", [["fill", "Fill"], ["simple", "Simple"]]),
    h("h2", null, "Alerts"),
    choice("sensitivity", "Alert sensitivity", "Early warns sooner. Relaxed waits until a slowdown is closer.",
      [["relaxed", "Relaxed"], ["balanced", "Balanced"], ["early", "Early"]]),
    toggle("notifications", "Notifications", "One notification when things get tight, with the app to quit."),
    notifyNote,
    select("cooldownMinutes", "Quiet time between alerts", "The same reason won't notify you again within this time.",
      [["10", "10 minutes"], ["20", "20 minutes"], ["30", "30 minutes"], ["60", "1 hour"], ["120", "2 hours"]]),
    toggle("showForceQuit", "Show force quit", "Adds a force quit option to the safety check, with a second confirmation. Unsaved work in that app is lost."),
    h("h2", null, "History"),
    select("retentionDays", "Keep history for", "Detailed samples are kept for a day, then summarised.", [["30", "30 days"], ["90", "90 days"]]),
    h("div", { class: "set-row" },
      h("div", { class: "set-text" }, h("span", { class: "lbl" }, "Export history"), h("p", { class: "caption" }, "Saves your history as a CSV file you choose where to keep.")),
      h("button", { class: "btn secondary", type: "button", on: { click: exportHistory } }, "Export history…")),
    deleteRow(),
  );
  if (focusedId) document.getElementById(focusedId)?.focus();
  loadNotifyStatus();
}

let deleting = false;
function deleteRow(): HTMLElement {
  const rowEl = h("div", { class: "set-row" },
    h("div", { class: "set-text" }, h("span", { class: "lbl" }, "Delete all history"),
      h("p", { class: "caption" }, "Deletes every sample, slowdown and action record from this Mac. Settings stay.")));
  if (!deleting) {
    rowEl.append(h("button", { class: "btn destructive", type: "button", on: { click: () => { deleting = true; renderSettings(); } } }, "Delete all history…"));
    return rowEl;
  }
  const cancel = h("button", { class: "btn primary", type: "button", id: "del-cancel", on: { click: () => { deleting = false; renderSettings(); } } }, "Cancel");
  const box = h("div", { class: "confirm", role: "alertdialog", "aria-labelledby": "del-q" },
    h("p", { id: "del-q", class: "confirm-title" }, "Delete all history? This can't be undone."),
    h("div", { class: "row-actions" },
      h("button", { class: "btn destructive", type: "button", on: { click: async () => {
        try {
          await api.deleteHistory("delete");
          deleting = false;
          showToast("History deleted.");
          renderSettings();
          loadTimeline();
          loadHistory();
        } catch (e) {
          fail(e);
        }
      } } }, "Delete all history"),
      cancel));
  setTimeout(() => cancel.focus());
  return h("div", null, rowEl, box);
}

async function exportHistory() {
  try {
    const path = await api.exportHistory();
    if (path) showToast(`History saved to ${path.split("/").pop()}.`);
  } catch (e) {
    fail(e);
  }
}

async function loadNotifyStatus() {
  if (!settings?.notifications) {
    notifyNote.hidden = true;
    return;
  }
  try {
    const n: NotifyStatus = await api.notifyStatus();
    clear(notifyNote);
    if (!n.available) {
      notifyNote.append(h("p", null, "Notifications work when Headroom runs from the Applications folder."));
      notifyNote.hidden = false;
    } else if (!n.authorized) {
      notifyNote.append(
        h("p", null, "Notifications are turned off for Headroom in System Settings."),
        h("button", { class: "btn secondary small", type: "button", on: { click: () => api.openNotificationSettings().catch(fail) } }, "Open System Settings"));
      notifyNote.hidden = false;
    } else {
      notifyNote.hidden = true;
    }
  } catch {
    notifyNote.hidden = true;
  }
}

// ---- wiring -------------------------------------------------------------------------

const onShow: Partial<Record<SectionId, () => void>> = {
  overview: () => { loadTimeline(); loadApps(); },
  memory: () => loadDay(),
  apps: () => loadApps(),
  cpu: () => { loadApps(); loadDay(); },
  disk: () => loadDisk(false),
  history: () => loadHistory(),
  settings: () => renderSettings(),
};

async function start() {
  go("overview");
  try {
    machine = await api.machine();
    demoTag.hidden = !machine.demo;
    ovMachine.textContent = machine.line;
  } catch (e) {
    fail(e);
  }
  try {
    settings = await api.settings();
  } catch (e) {
    fail(e);
  }
  await on("status", (st: Status) => { status = st; renderStatus(); });
  await on("navigate", (id: string) => go(id as SectionId));
  await on("toast", (msg: string) => showToast(msg));
  await on("history-changed", () => {
    if (current === "history") loadHistory();
    if (current === "apps" || current === "overview") loadApps();
  });
  try {
    status = await api.status();
    renderStatus();
  } catch (e) {
    fail(e);
  }
  // Refresh lists while visible; the status itself arrives as events.
  setInterval(() => {
    if (document.hidden) return;
    if (current === "apps" || current === "overview" || current === "cpu") loadApps();
  }, 5000);
  setInterval(() => {
    if (document.hidden) return;
    if (current === "overview") loadTimeline();
    if (current === "memory" || current === "cpu") loadDay();
  }, 60000);
}

start();
