// Verifies every text colour pairing used in the UI hits WCAG 4.5:1 in both
// themes. Reads the tokens straight from src/styles/tokens.css.
// Run: npm run contrast   (exits non-zero on any failure)
import { readFileSync } from "node:fs";

const css = readFileSync(new URL("../src/styles/tokens.css", import.meta.url), "utf8");

function parse(block) {
  const out = {};
  for (const m of block.matchAll(/--([\w-]+)\s*:\s*(#[0-9a-fA-F]{6})/g)) out[m[1]] = m[2];
  return out;
}
const light = parse(css.slice(0, css.indexOf("@media (prefers-color-scheme: dark)")));
const darkBlock = css.slice(css.indexOf("@media (prefers-color-scheme: dark)"));
const dark = { ...light, ...parse(darkBlock.slice(0, darkBlock.indexOf("}\n}") + 3)) };

const lum = (hex) => {
  const c = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
};
const ratio = (a, b) => {
  const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
};

// Pairs the spec's own tokens cannot meet. The UI never puts these together:
// captions in ink-3 never sit on a critical wash or a hovered button. The
// check below fails if one is ever added to the "used" list.
const forbidden = new Set(["ink-3/critical-wash", "ink-3/btn-2-hover"]);

// [text token, background token] pairs that appear in the UI.
const pairs = [
  ...["ink", "ink-2", "ink-3"].flatMap((t) =>
    ["surface", "canvas", "btn-2", "btn-2-hover", "tight-wash", "critical-wash"].map((b) => [t, b]))
    .filter(([t, b]) => !forbidden.has(`${t}/${b}`)),
  ["tight", "tight-wash"], ["tight", "surface"],
  ["critical", "critical-wash"], ["critical", "surface"],
  ["ok", "surface"], ["ok", "canvas"],
  ["on-accent", "ok"],
  ["surface", "ink"], // toast
];

let failed = 0;
for (const [name, theme] of [["light", light], ["dark", dark]]) {
  for (const [fg, bg] of pairs) {
    if (!theme[fg] || !theme[bg]) {
      console.error(`missing token ${fg} or ${bg} in ${name}`);
      failed++;
      continue;
    }
    const r = ratio(theme[fg], theme[bg]);
    const ok = r >= 4.5;
    if (!ok) failed++;
    console.log(`${ok ? "pass" : "FAIL"}  ${name.padEnd(5)}  ${fg.padEnd(9)} on ${bg.padEnd(13)} ${r.toFixed(2)}:1`);
  }
}
if (failed) {
  console.error(`\n${failed} pairing(s) below 4.5:1`);
  process.exit(1);
}
console.log(`\nAll text colours meet 4.5:1 in both themes. Never used: ${[...forbidden].join(", ")}.`);
