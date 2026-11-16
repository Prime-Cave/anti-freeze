// The safety check (section 7.5). Shown before every quit. The default
// button is "Keep [App] open", it has focus, and Escape triggers it.
// Quitting is graceful; force quit is a separate, hidden-by-default option
// with a second confirmation.
import { api, errorText, on } from "./api";
import { clear, h, icon } from "./dom";
import { bytes, percent } from "./format";
import type { SafetyCheck } from "./types";

const dialog = document.getElementById("dialog")!;

let check: SafetyCheck | null = null;
let appKey = "";
let busyAction = false;

function closeDialog() {
  check = null;
  appKey = "";
  api.closeSafetyCheck().catch(() => {});
}

function fit() {
  requestAnimationFrame(() => {
    const hgt = Math.ceil(dialog.getBoundingClientRect().height) + 2;
    api.resizeSafety(hgt).catch(() => {});
  });
}

function evidenceRow(label: string, value: string, flagged: boolean): HTMLElement {
  return h("tr", flagged ? { class: "flag" } : null,
    h("th", { scope: "row" }, label),
    h("td", { class: "num" }, value));
}

function render(sc: SafetyCheck) {
  check = sc;
  clear(dialog);
  const name = sc.shortName;
  const title = sc.busy ? `${name} looks busy. Quit anyway?` : `Quit ${name}?`;
  const body = sc.busy
    ? "It may be in the middle of a task. Quitting now could stop that work before it finishes."
    : "It doesn't look busy right now.";

  const keep = h("button", { class: "btn primary", type: "button", on: { click: closeDialog } }, `Keep ${name} open`);
  const quit = h("button", { class: "btn destructive", type: "button", on: { click: () => doQuit() } }, `Quit ${name}`);

  const extra = sc.evidence.filter((e) => !e.startsWith("Processor use") && !e.startsWith("It is reading"));
  const diskBusy = sc.diskRate >= 512 * 1024;
  const table = h("table", { class: "evidence", "aria-label": "What Headroom checked" },
    h("tbody", null,
      evidenceRow(`Processor use, last ${Math.min(30, Math.round(sc.windowSec)) || 30} seconds`, percent(sc.cpu), sc.cpu >= 10),
      evidenceRow("Reading or writing files", diskBusy ? `Yes, ${bytes(sc.diskRate)}/s` : "No", diskBusy),
      evidenceRow("Memory you'd get back", bytes(sc.frees), false),
    ));

  const parts: (HTMLElement | null)[] = [
    h("div", { class: "head" },
      h("span", { class: "warn-icon" + (sc.busy ? "" : " calm") }, icon.warn()),
      h("div", null,
        h("h1", { id: "sc-title" }, title),
        h("p", { id: "sc-body", class: "body" }, body))),
    table,
    extra.length ? h("ul", { class: "notes" }, extra.map((e) => h("li", null, e))) : null,
    h("p", { class: "note" }, "Headroom asks the app to quit normally, so it can ask you to save first. It never force quits unless you choose that separately."),
    h("p", { class: "result", role: "status", "aria-live": "polite", hidden: true }),
    h("div", { class: "buttons" }, quit, keep),
    sc.canForceQuit ? h("div", { class: "force" },
      h("button", { class: "link", type: "button", on: { click: () => askForce() } }, "Force quit…")) : null,
  ];
  dialog.append(...parts.filter((p): p is HTMLElement => p !== null));
  keep.focus();
  fit();
}

function showResult(msg: string, isError = false) {
  const r = dialog.querySelector<HTMLElement>(".result");
  if (!r) return;
  r.textContent = msg;
  r.className = "result" + (isError ? " error" : "");
  r.hidden = false;
  fit();
}

function setButtons(disabled: boolean) {
  dialog.querySelectorAll<HTMLButtonElement>("button").forEach((b) => {
    if (!b.classList.contains("primary")) b.disabled = disabled;
  });
}

async function doQuit() {
  if (!check || busyAction) return;
  busyAction = true;
  setButtons(true);
  try {
    const r = await api.confirmQuit(check.token);
    showResult(r.message, !r.ok);
    if (r.ok) setTimeout(closeDialog, 1400);
    else refresh(); // still open (asking to save): a fresh check for another try
  } catch (e) {
    showResult(errorText(e), true);
    refresh();
  } finally {
    busyAction = false;
  }
}

function askForce() {
  if (!check) return;
  const name = check.shortName;
  const box = dialog.querySelector<HTMLElement>(".force")!;
  clear(box);
  const cancel = h("button", { class: "btn secondary", type: "button", on: { click: () => check && render(check) } }, "Cancel");
  box.append(
    h("p", { class: "force-warn" }, `Force quit ${name}? Anything not saved in ${name} will be lost.`),
    h("div", { class: "buttons" },
      h("button", { class: "btn destructive", type: "button", on: { click: () => doForce() } }, `Force quit ${name}`),
      cancel),
  );
  cancel.focus();
  fit();
}

async function doForce() {
  if (!check || busyAction) return;
  busyAction = true;
  setButtons(true);
  try {
    const r = await api.confirmForceQuit(check.token, "force");
    showResult(r.message, !r.ok);
    if (r.ok) setTimeout(closeDialog, 1400);
  } catch (e) {
    showResult(errorText(e), true);
    refresh();
  } finally {
    busyAction = false;
  }
}

// Tokens are single-use and expire, so after any failure the check runs
// again (and re-measures whether the app is busy).
async function refresh() {
  if (!appKey) return;
  try {
    const sc = await api.prepareQuit(appKey);
    const msg = dialog.querySelector<HTMLElement>(".result");
    const keepMsg = msg && !msg.hidden ? { text: msg.textContent ?? "", err: msg.classList.contains("error") } : null;
    render(sc);
    if (keepMsg) showResult(keepMsg.text, keepMsg.err);
  } catch (e) {
    showError(e);
  }
}

function showError(e: unknown) {
  check = null;
  clear(dialog);
  const ok = h("button", { class: "btn primary", type: "button", on: { click: closeDialog } }, "OK");
  dialog.append(
    h("div", { class: "head" },
      h("span", { class: "warn-icon" }, icon.warn()),
      h("div", null, h("h1", { id: "sc-title" }, "Headroom can't quit that app"),
        h("p", { id: "sc-body", class: "body" }, errorText(e)))),
    h("div", { class: "buttons" }, ok),
  );
  ok.focus();
  fit();
}

async function open(key: string) {
  if (!key) return;
  appKey = key;
  clear(dialog);
  dialog.append(h("div", { class: "skeleton", "aria-busy": "true", "aria-label": "Checking" },
    h("span", { class: "sk sk-title" }), h("span", { class: "sk sk-line" }), h("span", { class: "sk sk-bar" })));
  try {
    render(await api.prepareQuit(key));
  } catch (e) {
    showError(e);
  }
}

// Escape always means "Keep [App] open".
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    closeDialog();
  }
});

async function start() {
  await on("safety-open", (key: string) => open(key));
  const pending = await api.pendingSafetyCheck().catch(() => "");
  if (pending) open(pending);
}

start();
