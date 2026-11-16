// Timeline chart (section 7.6): memory in use as an area, overflow on disk
// stacked on top (hatched, so it never relies on colour alone), a dashed red
// "Slowdown line", red-wash bands where slowdowns happened, and a crosshair
// tooltip (mouse and keyboard). Plain SVG, no library.
import { add, clear, h, s } from "./dom";
import { bytes, capacity, dateTime, time } from "./format";
import type { Point, Timeline } from "./types";

const H = 220;
const PAD = { top: 14, right: 12, bottom: 24, left: 52 };

export interface ChartParts {
  root: HTMLElement;
  update(t: Timeline): void;
}

let uid = 0;

export function timelineChart(): ChartParts {
  const id = `c${++uid}`;
  const svgBox = h("div", { class: "chart-plot", tabindex: "0", role: "img" });
  const tip = h("div", { class: "chart-tip", role: "presentation", hidden: true });
  const legend = h("ul", { class: "chart-legend", "aria-hidden": "true" },
    h("li", null, h("span", { class: "sw used" }), "In use"),
    h("li", null, h("span", { class: "sw overflow" }), "Overflow on disk"),
    h("li", null, h("span", { class: "sw line" }), "Slowdown line"),
    h("li", null, h("span", { class: "sw band" }), "Slowdown"),
  );
  const table = h("table", { class: "chart-table" });
  const details = h("details", { class: "chart-details" }, h("summary", null, "Show as a table"), table);
  const root = h("div", { class: "chart" }, legend, h("div", { class: "chart-area" }, svgBox, tip), details);

  let data: Timeline | null = null;
  let pts: Point[] = [];
  let width = 600;
  let hover = -1;
  let x = (_t: number) => 0;
  let y = (_v: number) => 0;
  let cross: SVGElement | null = null;

  function draw() {
    clear(svgBox);
    if (!data) return;
    width = Math.max(320, Math.floor(svgBox.getBoundingClientRect().width) || 600);
    const t0 = new Date(data.from).getTime();
    const t1 = new Date(data.to).getTime();
    const max = Math.max(data.slowdownLine * 1.08, ...pts.map((p) => (p.memUsed + p.swapUsed) * 1.05), 1);
    const iw = width - PAD.left - PAD.right;
    const ih = H - PAD.top - PAD.bottom;
    x = (t: number) => PAD.left + ((t - t0) / (t1 - t0)) * iw;
    y = (v: number) => PAD.top + ih - (v / max) * ih;

    const svg = s("svg", { width, height: H, viewBox: `0 0 ${width} ${H}`, "aria-hidden": "true" });
    svg.append(s("defs", {},
      s("pattern", { id: `${id}-hatch`, width: 6, height: 6, patternUnits: "userSpaceOnUse", patternTransform: "rotate(135)" },
        s("rect", { width: 6, height: 6, class: "hatch-b" }),
        s("rect", { width: 3, height: 6, class: "hatch-a" })),
      s("clipPath", { id: `${id}-clip` }, s("rect", { x: PAD.left, y: PAD.top, width: iw, height: ih }))));

    // Grid: 0, memory size, slowdown line.
    const grid = s("g", { class: "grid" });
    for (const [v, label] of [[0, "0"], [data.memTotal, capacity(data.memTotal)]] as [number, string][]) {
      grid.append(s("line", { x1: PAD.left, x2: width - PAD.right, y1: y(v), y2: y(v) }),
        s("text", { x: PAD.left - 8, y: y(v) + 4, "text-anchor": "end" }, label));
    }
    // Time ticks at round steps (at most ~6).
    const steps = [15, 30, 60, 120, 180, 360, 720, 1440, 2880, 4320, 10080].map((m) => m * 60e3);
    const step = steps.find((st) => (t1 - t0) / st <= 6) ?? steps[steps.length - 1];
    const long = step >= 1440 * 60e3;
    const off = new Date(t0).getTimezoneOffset() * 60e3;
    for (let t = Math.ceil((t0 - off) / step) * step + off; t <= t1; t += step) {
      const px = x(t);
      if (px < PAD.left + 16 || px > width - PAD.right - 16) continue;
      grid.append(s("line", { class: "tick", x1: px, x2: px, y1: PAD.top + ih, y2: PAD.top + ih + 4 }),
        s("text", { x: px, y: H - 6, "text-anchor": "middle" },
          long ? new Date(t).toLocaleDateString(undefined, { day: "numeric", month: "short" }) : time(new Date(t))));
    }
    svg.append(grid);

    // Episode bands.
    const bands = s("g", { class: "bands", "clip-path": `url(#${id}-clip)` });
    for (const e of data.episodes ?? []) {
      const a = Math.max(t0, new Date(e.start).getTime());
      const b = Math.min(t1, new Date(e.end).getTime() || t1);
      if (b <= a) continue;
      bands.append(s("rect", { x: x(a), y: PAD.top, width: Math.max(2, x(b) - x(a)), height: ih }));
    }
    svg.append(bands);

    // Areas, broken where samples are missing.
    const runs = splitRuns(pts);
    const areas = s("g", { "clip-path": `url(#${id}-clip)` });
    for (const run of runs) {
      areas.append(s("path", { class: "area-used", d: areaPath(run, () => 0, (p) => p.memUsed) }));
      areas.append(s("path", { class: "area-over", fill: `url(#${id}-hatch)`, d: areaPath(run, (p) => p.memUsed, (p) => p.memUsed + p.swapUsed) }));
      areas.append(s("path", { class: "line-top", d: linePath(run, (p) => p.memUsed + p.swapUsed) }));
    }
    svg.append(areas);

    // Slowdown line.
    const sy = y(data.slowdownLine);
    svg.append(s("line", { class: "slowdown", x1: PAD.left, x2: width - PAD.right, y1: sy, y2: sy }),
      s("text", { class: "slowdown-label", x: width - PAD.right - 4, y: sy - 5, "text-anchor": "end" }, "Slowdown line"));

    cross = s("g", { class: "cross", visibility: "hidden" },
      s("line", { y1: PAD.top, y2: PAD.top + ih }),
      s("circle", { r: 4 }));
    svg.append(cross);
    svgBox.append(svg);
    if (hover >= 0) showHover(hover);
  }

  function splitRuns(p: Point[]): Point[][] {
    if (p.length < 2) return p.length ? [p] : [];
    const gaps: number[] = [];
    for (let i = 1; i < p.length; i++) gaps.push(tms(p[i]) - tms(p[i - 1]));
    const med = [...gaps].sort((a, b) => a - b)[Math.floor(gaps.length / 2)];
    const out: Point[][] = [[p[0]]];
    for (let i = 1; i < p.length; i++) {
      if (gaps[i - 1] > med * 4) out.push([]);
      out[out.length - 1].push(p[i]);
    }
    return out;
  }

  function areaPath(run: Point[], lo: (p: Point) => number, hi: (p: Point) => number): string {
    if (run.length === 1) run = [run[0], { ...run[0], time: new Date(tms(run[0]) + 1000).toISOString() }];
    const top = run.map((p, i) => `${i ? "L" : "M"}${x(tms(p)).toFixed(1)},${y(hi(p)).toFixed(1)}`).join("");
    const bottom = [...run].reverse().map((p) => `L${x(tms(p)).toFixed(1)},${y(lo(p)).toFixed(1)}`).join("");
    return top + bottom + "Z";
  }

  function linePath(run: Point[], v: (p: Point) => number): string {
    return run.map((p, i) => `${i ? "L" : "M"}${x(tms(p)).toFixed(1)},${y(v(p)).toFixed(1)}`).join("");
  }

  function nearest(px: number): number {
    let best = -1;
    let bd = Infinity;
    pts.forEach((p, i) => {
      const d = Math.abs(x(tms(p)) - px);
      if (d < bd) { bd = d; best = i; }
    });
    return best;
  }

  function showHover(i: number) {
    if (!cross || i < 0 || i >= pts.length) return;
    hover = i;
    const p = pts[i];
    const cx = x(tms(p));
    cross.setAttribute("visibility", "visible");
    const [line, dot] = Array.from(cross.children) as SVGElement[];
    line.setAttribute("x1", String(cx));
    line.setAttribute("x2", String(cx));
    dot.setAttribute("cx", String(cx));
    dot.setAttribute("cy", String(y(p.memUsed + p.swapUsed)));
    clear(tip);
    add(tip,
      h("div", { class: "tt-time" }, dateTime(p.time)),
      row("Memory in use", bytes(p.memUsed)),
      h("div", { class: "tt-row", title: "Swap" }, h("span", null, "Overflow on disk (swap)"), h("b", { class: "num" }, bytes(p.swapUsed))),
      row("Disk free", bytes(p.diskFree)),
      p.topApp ? row("Top app", p.topApp) : null,
    );
    tip.hidden = false;
    const tw = tip.offsetWidth;
    let left = cx + 12;
    if (left + tw > width - 4) left = cx - tw - 12;
    tip.style.left = Math.max(0, left) + "px";
    tip.style.top = PAD.top + "px";
  }

  function hideHover() {
    hover = -1;
    tip.hidden = true;
    cross?.setAttribute("visibility", "hidden");
  }

  svgBox.addEventListener("mousemove", (e) => {
    const r = svgBox.getBoundingClientRect();
    showHover(nearest(e.clientX - r.left));
  });
  svgBox.addEventListener("mouseleave", hideHover);
  svgBox.addEventListener("blur", hideHover);
  svgBox.addEventListener("keydown", (e) => {
    if (!pts.length) return;
    const step = Math.max(1, Math.round(pts.length / 40));
    if (e.key === "ArrowRight") showHover(Math.min(pts.length - 1, (hover < 0 ? -1 : hover) + step));
    else if (e.key === "ArrowLeft") showHover(Math.max(0, (hover < 0 ? pts.length : hover) - step));
    else if (e.key === "Home") showHover(0);
    else if (e.key === "End") showHover(pts.length - 1);
    else if (e.key === "Escape") { hideHover(); return; }
    else return;
    e.preventDefault();
  });

  new ResizeObserver(() => draw()).observe(svgBox);

  return {
    root,
    update(t: Timeline) {
      data = t;
      pts = t.points ?? [];
      draw();
      // Accessible summary and table.
      let peak = pts[0];
      for (const p of pts) if (p.memUsed + p.swapUsed > peak.memUsed + peak.swapUsed) peak = p;
      svgBox.setAttribute("aria-label",
        `Memory over time. ${t.summary}` + (peak ? ` Highest: ${bytes(peak.memUsed + peak.swapUsed)} including overflow on disk, at ${dateTime(peak.time)}.` : "") +
        " Use the arrow keys to read values.");
      clear(table);
      table.append(h("thead", null, h("tr", null, ...["Time", "Memory in use", "Overflow on disk", "Disk free", "Top app"].map((c) => h("th", { scope: "col" }, c)))));
      const body = h("tbody");
      const every = Math.max(1, Math.ceil(pts.length / 24));
      pts.filter((_, i) => i % every === 0).forEach((p) => body.append(h("tr", null,
        h("td", null, dateTime(p.time)), h("td", { class: "num" }, bytes(p.memUsed)), h("td", { class: "num" }, bytes(p.swapUsed)),
        h("td", { class: "num" }, bytes(p.diskFree)), h("td", null, p.topApp || "—"))));
      table.append(body);
    },
  };
}

function row(label: string, value: string) {
  return h("div", { class: "tt-row" }, h("span", null, label), h("b", { class: "num" }, value));
}

function tms(p: Point) {
  return new Date(p.time).getTime();
}
