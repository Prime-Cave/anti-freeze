// The menu bar dropdown (section 7.3).
import { api, errorText, on } from "./api";
import { clear, h, icon } from "./dom";
import { appAvatar } from "./avatar";
import { bytes, GROWTH_LARGE, NOT_AVAILABLE, percent, signedBytes } from "./format";
import { runway } from "./runway";
import type { AppRow, Status, Suggestion } from "./types";

const panel = document.getElementById("panel")!;

// ---- static structure -----------------------------------------------------------

const dot = h("span", { class: "dot", "aria-hidden": "true" });
const title = h("h1", { class: "title", id: "state-title" });
const bigFigure = h("span", { class: "big figure num" });
const forecast = h("p", { class: "forecast", "aria-live": "polite" });
const header = h("section", { class: "status", "aria-labelledby": "state-title" },
  h("div", { class: "status-line" }, dot, title),
  h("div", { class: "figure-line" }, bigFigure, h("span", { class: "unit" }, "to spare")),
  forecast,
);

const rw = runway();
const runwaySec = h("section", { class: "sec runway-sec", "aria-label": "Memory and disk" }, rw.root);

const helpList = h("div", { class: "cards" });
const helpSec = h("section", { class: "sec help", "aria-labelledby": "help-h" },
  h("h2", { id: "help-h" }, "What would help most"), helpList);

const appsList = h("ul", { class: "apps", role: "list" });
const appsHeadGrowth = h("span", { class: "col-growth", "aria-hidden": "true" }, "Last hour");
const appsSec = h("section", { class: "sec", "aria-labelledby": "apps-h" },
  h("div", { class: "sec-head" }, h("h2", { id: "apps-h" }, "Using the most memory"), appsHeadGrowth),
  appsList);

const miniCpu = h("span", { class: "mv num" });
const miniHeat = h("span", { class: "mv" });
const miniDisk = h("span", { class: "mv num" });
const miniSec = h("section", { class: "sec mini", "aria-label": "Other readings" },
  h("div", { class: "mini-item" }, h("span", { class: "ml" }, "Processor"), miniCpu),
  h("div", { class: "mini-item" }, h("span", { class: "ml" }, "Heat"), miniHeat),
  h("div", { class: "mini-item" }, h("span", { class: "ml" }, "Disk free"), miniDisk),
);

const dashBtn = h("button", { class: "btn primary", type: "button", on: { click: () => api.openDashboard("overview") } }, "Open dashboard");
const settingsBtn = h("button", { class: "btn icon-btn", type: "button", "aria-label": "Settings", title: "Settings",
  on: { click: () => api.openDashboard("settings") } }, icon.gear());
const footer = h("footer", { class: "foot" }, dashBtn, settingsBtn);

const demoTag = h("div", { class: "demo-tag", hidden: true }, "Demo data");
const toast = h("div", { class: "toast", role: "status", "aria-live": "polite", hidden: true });

const skeleton = h("div", { class: "skeleton", "aria-busy": "true", "aria-label": "Loading" },
  h("span", { class: "sk sk-title" }), h("span", { class: "sk sk-big" }), h("span", { class: "sk sk-line" }),
  h("span", { class: "sk sk-bar" }), h("span", { class: "sk sk-line" }), h("span", { class: "sk sk-line short" }),
);

const content = h("div", { class: "content", hidden: true }, header, runwaySec, helpSec, appsSec, miniSec, footer);
panel.append(demoTag, skeleton, content, toast);

// ---- rendering ------------------------------------------------------------------

let last: Status | null = null;
const dismissed = new Set<string>(); // "Not now" hides a card until the state changes
let lastState = "";

function render(st: Status) {
  last = st;
  if (!st.ready) return;
  skeleton.hidden = true;
  content.hidden = false;

  if (st.state !== lastState) {
    dismissed.clear();
    lastState = st.state;
  }
  document.body.dataset.state = st.state;
  header.className = "status " + st.state;
  title.textContent = st.label;
  const memKnown = st.runway.memKnown;
  bigFigure.textContent = memKnown ? st.headroomText : "—";
  forecast.textContent = memKnown ? st.forecast.text : NOT_AVAILABLE;

  rw.update(st.runway);

  // Suggestions only when tight or critical.
  const cards = (st.suggestions ?? []).filter((c) => !dismissed.has(cardKey(c)));
  helpSec.hidden = st.state === "ok" || cards.length === 0;
  clear(helpList);
  cards.forEach((c, i) => helpList.append(card(c, i === 0)));

  // Footer button is primary in ok state, secondary when a card holds the primary.
  const cardHasPrimary = !helpSec.hidden;
  dashBtn.className = "btn " + (cardHasPrimary ? "secondary" : "primary");

  // Apps: growth column only in tight/critical.
  const showGrowth = st.state !== "ok";
  appsHeadGrowth.hidden = !showGrowth;
  clear(appsList);
  for (const a of st.topApps ?? []) appsList.append(appRow(a, showGrowth));
  appsSec.hidden = (st.topApps ?? []).length === 0;

  // Mini stats only in ok state.
  miniSec.hidden = st.state !== "ok";
  miniCpu.textContent = (st.unavailable ?? []).includes("cpu") ? NOT_AVAILABLE : percent(st.cpu);
  miniHeat.textContent = st.thermal;
  miniDisk.textContent = st.runway.diskKnown ? bytes(st.runway.diskFree) : NOT_AVAILABLE;

  fit();
}

function cardKey(c: Suggestion) {
  return c.kind + ":" + (c.appKey ?? "");
}

function card(c: Suggestion, primary: boolean): HTMLElement {
  const actions = h("div", { class: "card-actions" });
  if (c.kind === "quit" && c.appKey) {
    actions.append(
      h("button", { class: "btn " + (primary ? "primary" : "secondary"), type: "button",
        on: { click: () => api.openSafetyCheck(c.appKey!).catch(showError) } }, c.action),
      h("button", { class: "btn secondary", type: "button",
        on: { click: () => { dismissed.add(cardKey(c)); if (last) render(last); } } }, "Not now"),
    );
  } else {
    actions.append(h("button", { class: "btn " + (primary ? "primary" : "secondary"), type: "button",
      on: { click: () => api.openDashboard("disk") } }, c.action));
  }
  return h("article", { class: "card" }, h("p", null, c.text), actions);
}

const iconCache = new Map<string, string>();

function appRow(a: AppRow, showGrowth: boolean): HTMLElement {
  const big = a.growth >= GROWTH_LARGE;
  const size = a.known ? bytes(a.footprint) : "—";
  const growthText = a.growth > 0 ? signedBytes(a.growth) : "";
  const label = `${a.name}, ${a.count} ${a.count === 1 ? "process" : "processes"}, ${a.known ? bytes(a.footprint) : NOT_AVAILABLE}` +
    (showGrowth && growthText ? `, ${growthText} in the last hour` : "");
  return h("li", { class: "app", "aria-label": label },
    appAvatar(a, iconCache),
    h("span", { class: "app-name" }, h("span", { class: "nm" }, a.name), h("span", { class: "ct num" }, `(${a.count})`)),
    showGrowth ? h("span", { class: "growth num" + (big ? " large" : "") }, growthText) : null,
    h("span", { class: "size num" }, size),
  );
}

// ---- toast and errors -------------------------------------------------------------

let toastTimer = 0;
function showToast(msg: string) {
  toast.textContent = msg;
  toast.hidden = false;
  fit();
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => { toast.hidden = true; fit(); }, 5000);
}
function showError(e: unknown) {
  showToast(errorText(e));
}

// ---- window sizing ----------------------------------------------------------------

let lastHeight = 0;
function fit() {
  requestAnimationFrame(() => {
    const hgt = Math.ceil(panel.getBoundingClientRect().height) + 2;
    if (Math.abs(hgt - lastHeight) > 1) {
      lastHeight = hgt;
      api.resizeDropdown(hgt).catch(() => {});
    }
  });
}

// ---- keyboard ---------------------------------------------------------------------

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    api.hideDropdown().catch(() => {});
  }
});

// ---- start ------------------------------------------------------------------------

async function start() {
  fit();
  try {
    const m = await api.machine();
    demoTag.hidden = !m.demo;
  } catch {
    /* machine info is optional here */
  }
  await on("status", (st: Status) => render(st));
  await on("toast", (msg: string) => showToast(msg));
  await on("focus-panel", () => dashBtn.focus());
  try {
    render(await api.status());
  } catch (e) {
    showError(e);
  }
  window.addEventListener("focus", () => api.panelOpened().catch(() => {}));
}

start();
