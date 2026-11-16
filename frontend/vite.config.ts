import { defineConfig } from "vite";
import { resolve } from "node:path";

// Two pages: the menu bar dropdown and the dashboard window.
// No inline scripts or styles are emitted, so the CSP can stay strict.
export default defineConfig({
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: "safari16",
    modulePreload: { polyfill: false },
    assetsInlineLimit: 0,
    rollupOptions: {
      input: {
        index: resolve(__dirname, "index.html"),
        dashboard: resolve(__dirname, "dashboard.html"),
      },
    },
  },
});
