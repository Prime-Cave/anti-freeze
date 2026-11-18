// Set at build time by scripts/build.mjs: true only in the browser preview.
interface ImportMeta {
  readonly env: { readonly DEV: boolean };
}
