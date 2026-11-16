// The runway bar: the app's signature element. Memory on the left (60%),
// disk on the right (40%), overflow ("swap") spilling across the gap as a
// hatched segment.
import { h } from "./dom";
import { bytes, capacity, NOT_AVAILABLE } from "./format";
import type { Runway } from "./types";

export interface RunwayParts {
  root: HTMLElement;
  update(r: Runway): void;
}

const pct = (v: number, of: number) => (of > 0 ? Math.max(0, Math.min(100, (v / of) * 100)) : 0);

export function runway(opts: { tall?: boolean; legend?: boolean } = {}): RunwayParts {
  const capMem = h("span", { class: "cap-mem" });
  const capDisk = h("span", { class: "cap-disk" });
  const segUsed = h("span", { class: "seg used" });
  const segSq = h("span", { class: "seg squeezed" });
  const segSpare = h("span", { class: "seg spare" });
  const bridge = h("span", { class: "bridge", "aria-hidden": "true" });
  const segOver = h("span", { class: "seg overflow" });
  const segFree = h("span", { class: "seg free" });
  const memNA = h("span", { class: "na", hidden: true }, NOT_AVAILABLE);
  const diskNA = h("span", { class: "na", hidden: true }, NOT_AVAILABLE);

  const bar = h("div", { class: "runway" + (opts.tall ? " tall" : ""), role: "img" },
    h("div", { class: "half mem" }, segUsed, segSq, segSpare, memNA),
    bridge,
    h("div", { class: "half disk" }, segOver, segFree, diskNA),
  );

  const lv = (cls: string, label: string, title?: string) => {
    const v = h("span", { class: "lv num" });
    const li = h("li", { class: cls, title }, h("span", { class: "sw", "aria-hidden": "true" }), h("span", { class: "ll" }, label), v);
    return { li, v };
  };
  const lUsed = lv("used", "In use");
  const lSq = lv("squeezed", "Squeezed", "Compressed memory: macOS squeezes idle memory to make room.");
  const lSpare = lv("spare", "Spare");
  const lOver = lv("overflow", "Overflow on disk", "Swap: memory macOS has moved to disk because RAM is full.");
  const legend = h("ul", { class: "legend" }, lUsed.li, lSq.li, lSpare.li, lOver.li);

  const root = h("div", { class: "runway-wrap" },
    h("div", { class: "runway-caps", "aria-hidden": "true" }, capMem, capDisk),
    bar,
    opts.legend === false ? null : legend,
  );

  return {
    root,
    update(r: Runway) {
      capMem.textContent = r.memKnown ? `Memory, ${capacity(r.memTotal)}` : "Memory";
      capDisk.textContent = r.diskKnown ? `Disk, ${bytes(r.diskFree)} free` : "Disk";

      memNA.hidden = r.memKnown;
      for (const el of [segUsed, segSq, segSpare]) el.hidden = !r.memKnown;
      if (r.memKnown) {
        segUsed.style.width = pct(r.inUse, r.memTotal) + "%";
        segSq.style.width = pct(r.squeezed, r.memTotal) + "%";
        segSpare.style.width = pct(r.spare, r.memTotal) + "%";
      }

      diskNA.hidden = r.diskKnown;
      segOver.hidden = segFree.hidden = !r.diskKnown;
      const overflow = r.swapKnown ? r.overflow : 0;
      const right = overflow + r.diskFree;
      if (r.diskKnown) {
        segOver.style.width = pct(overflow, right) + "%";
        segFree.style.width = pct(r.diskFree, right) + "%";
      }
      bar.classList.toggle("spilling", overflow > 0 && r.diskKnown);

      lUsed.v.textContent = r.memKnown ? bytes(r.inUse) : "—";
      lSq.v.textContent = r.memKnown ? bytes(r.squeezed) : "—";
      lSpare.v.textContent = r.memKnown ? bytes(r.spare) : "—";
      lOver.v.textContent = r.swapKnown ? bytes(r.overflow) : "—";

      // VoiceOver reads all four values (section 7.9).
      const parts = r.memKnown
        ? [`Memory ${capacity(r.memTotal)}: in use ${bytes(r.inUse)}`, `squeezed ${bytes(r.squeezed)}`, `spare ${bytes(r.spare)}`]
        : [`Memory: ${NOT_AVAILABLE}`];
      parts.push(r.swapKnown ? `overflow on disk ${bytes(r.overflow)}` : `overflow on disk: ${NOT_AVAILABLE}`);
      parts.push(r.diskKnown ? `disk ${bytes(r.diskFree)} free` : `disk: ${NOT_AVAILABLE}`);
      bar.setAttribute("aria-label", parts.join(", ") + ".");
    },
  };
}
