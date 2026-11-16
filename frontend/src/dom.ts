// Tiny DOM builder. Text always goes through textContent and styles through
// CSSOM, so untrusted strings (app names, file names) can never become markup,
// and the strict CSP (no inline style attributes) is respected.

type Child = Node | string | number | null | undefined | false;
type Attrs = {
  class?: string;
  style?: Partial<Record<string, string>>;
  on?: Partial<Record<string, (e: any) => void>>;
  [attr: string]: unknown;
};

export function h(tag: string, attrs: Attrs | null = null, ...children: (Child | Child[])[]): HTMLElement {
  const el = document.createElement(tag);
  if (attrs) applyAttrs(el, attrs);
  append(el, children);
  return el;
}

const SVG_NS = "http://www.w3.org/2000/svg";

export function s(tag: string, attrs: Record<string, string | number> = {}, ...children: Child[]): SVGElement {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, String(v));
  append(el, children);
  return el;
}

function applyAttrs(el: HTMLElement, attrs: Attrs) {
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === null || v === false) continue;
    if (k === "class") el.className = String(v);
    else if (k === "style") {
      for (const [p, val] of Object.entries(v as Record<string, string>)) {
        if (p.startsWith("--")) el.style.setProperty(p, val);
        else (el.style as any)[p] = val;
      }
    } else if (k === "on") {
      for (const [ev, fn] of Object.entries(v as Record<string, (e: Event) => void>)) el.addEventListener(ev, fn);
    } else if (v === true) el.setAttribute(k, "");
    else el.setAttribute(k, String(v));
  }
}

function append(el: Element, children: (Child | Child[])[]) {
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    el.append(typeof c === "number" ? String(c) : c);
  }
}

/** Appends children, skipping null/false (for conditional parts). */
export function add(el: Element, ...children: (Child | Child[])[]) {
  append(el, children);
}

export function clear(el: Element) {
  while (el.firstChild) el.removeChild(el.firstChild);
}

/** Icons drawn as inline SVG (no icon font, no remote assets). */
export const icon = {
  gear(): SVGElement {
    // Eight teeth from a dashed ring, a body ring, and a hole.
    return s("svg", { viewBox: "0 0 16 16", width: 16, height: 16, "aria-hidden": "true", class: "ico" },
      s("circle", { cx: 8, cy: 8, r: 6, fill: "none", stroke: "currentColor", "stroke-width": 2.4, "stroke-dasharray": "2.36 2.35" }),
      s("circle", { cx: 8, cy: 8, r: 4.6, fill: "none", stroke: "currentColor", "stroke-width": 1.6 }),
      s("circle", { cx: 8, cy: 8, r: 1.6, fill: "none", stroke: "currentColor", "stroke-width": 1.4 }));
  },
  warn(): SVGElement {
    return s("svg", { viewBox: "0 0 24 24", width: 28, height: 28, "aria-hidden": "true", class: "ico warn" },
      s("path", { d: "M12 3.5 22 20.5H2L12 3.5Z", fill: "currentColor" }),
      s("rect", { x: 11, y: 9, width: 2, height: 6.5, rx: 1, fill: "var(--surface)" }),
      s("circle", { cx: 12, cy: 17.8, r: 1.2, fill: "var(--surface)" }));
  },
};
