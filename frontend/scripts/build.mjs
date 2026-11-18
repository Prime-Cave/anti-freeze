// Builds the three UI pages with esbuild (which has no npm dependencies of its
// own). Run through npm:
//
//   npm run build   production build into dist/ (embedded into the app)
//   npm run dev     browser preview with mock data on http://127.0.0.1:5173
//
// Each page's HTML is copied with its /src/… references pointed at the built
// files. Nothing is inlined, so the pages' strict Content-Security-Policy
// (script-src 'self'; style-src 'self') keeps working.
import * as esbuild from "esbuild";
import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const dev = process.argv.includes("--serve");
const out = join(root, dev ? ".dev" : "dist");
const pages = ["index", "dashboard", "safety"];
const scripts = ["dropdown", "dashboard", "safety"];
const styles = ["tokens", "base", "dropdown", "dashboard", "safety"];

// Empty the output folder, keeping dist/.gitkeep so go:embed always has a file.
mkdirSync(out, { recursive: true });
for (const name of readdirSync(out)) {
  if (name !== ".gitkeep") rmSync(join(out, name), { recursive: true, force: true });
}

// Rewrite the source references in the HTML to the built files.
function writePages() {
  for (const page of pages) {
    let html = readFileSync(join(root, `${page}.html`), "utf8");
    html = html
      .replace(/"\/src\/styles\/([a-z]+)\.css"/g, '"./assets/$1.css"')
      .replace(/"\/src\/([a-z]+)\.ts"/g, '"./assets/$1.js"');
    if (/\/src\//.test(html)) throw new Error(`${page}.html: unrewritten /src/ reference`);
    writeFileSync(join(out, `${page}.html`), html);
  }
}

const common = {
  absWorkingDir: root,
  bundle: true,
  outdir: join(out, "assets"),
  target: "safari16",
  logLevel: "info",
  legalComments: "none",
  // The mock backend (synthetic data) is only reachable when DEV is true.
  // In production the branch is dead code and esbuild drops it and the mock.
  define: { "import.meta.env.DEV": dev ? "true" : "false" },
  // The one package bundled into the app, resolved straight from this
  // folder's node_modules. That keeps a stray Yarn Plug'n'Play file in a
  // parent folder (e.g. ~/.pnp.cjs) from changing what gets bundled.
  alias: { "@wailsio/runtime": join(root, "node_modules/@wailsio/runtime/dist/index.js") },
  minify: !dev,
  sourcemap: dev ? "inline" : false,
};

const js = {
  ...common,
  entryPoints: scripts.map((s) => `src/${s}.ts`),
  format: "esm",
  splitting: true,
  chunkNames: "chunk-[hash]",
};
const css = { ...common, entryPoints: styles.map((s) => `src/styles/${s}.css`) };

writePages();
if (!dev) {
  await Promise.all([esbuild.build(js), esbuild.build(css)]);
  const bundle = readdirSync(join(out, "assets"))
    .filter((f) => f.endsWith(".js"))
    .map((f) => readFileSync(join(out, "assets", f), "utf8"))
    .join("\n");
  // Belt and braces: fail the build if mock data ever ends up in the app.
  // Both strings are data that exists only in src/mock.ts.
  if (bundle.includes("proc:WindowServer") || bundle.includes("/Users/demo/")) {
    throw new Error("mock code found in the production bundle");
  }
} else {
  const [jsCtx, cssCtx] = await Promise.all([esbuild.context(js), esbuild.context(css)]);
  await Promise.all([jsCtx.watch(), cssCtx.watch()]);
  // Bound to this Mac only; never reachable from the network.
  const { port } = await jsCtx.serve({ servedir: out, host: "127.0.0.1", port: 5173 });
  console.log(`\nPreview: http://127.0.0.1:${port}/?state=tight  (also ok, critical, loading, na)`);
  console.log(`          http://127.0.0.1:${port}/dashboard.html  ·  /safety.html   Ctrl-C to stop`);
}
