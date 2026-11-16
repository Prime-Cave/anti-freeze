// App avatar: the real app icon when macOS can give us one, otherwise a
// letter on a neutral tile. Icons arrive as data: URLs (allowed by the CSP).
import { api } from "./api";
import { h } from "./dom";
import { initial } from "./format";
import type { AppRow } from "./types";

export function appAvatar(a: Pick<AppRow, "name" | "bundlePath">, cache: Map<string, string>): HTMLElement {
  const box = h("span", { class: "avatar", "aria-hidden": "true" }, initial(a.name));
  const path = a.bundlePath;
  if (!path) return box;
  const put = (url: string) => {
    if (!url) return;
    box.textContent = "";
    box.classList.add("has-icon");
    box.append(h("img", { src: url, alt: "", width: 20, height: 20 }));
  };
  const hit = cache.get(path);
  if (hit !== undefined) {
    put(hit);
  } else {
    cache.set(path, "");
    api.appIcon(path).then((url) => { cache.set(path, url); put(url); }).catch(() => {});
  }
  return box;
}
