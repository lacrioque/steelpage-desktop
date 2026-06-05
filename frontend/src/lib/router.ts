import { writable, derived } from "svelte/store";

const DEFAULT_DOC = "README.md";

export type RouteKind = "doc";

function pathFromLocation(): string {
  return new URL(window.location.href).pathname;
}

function searchFromLocation(): string {
  return new URL(window.location.href).search;
}

export const path = writable<string>(pathFromLocation());
export const search = writable<string>(searchFromLocation());

export const currentRef = derived(search, ($s) => {
  const params = new URLSearchParams($s);
  return params.get("ref");
});

// Desktop build: every route renders the doc view.
export const routeKind = derived<typeof path, RouteKind>(path, () => "doc");

export const currentDoc = derived(path, ($p) => {
  if ($p.startsWith("/docs/")) {
    const rest = $p.slice("/docs/".length);
    return decodeURIComponent(rest || DEFAULT_DOC);
  }
  return DEFAULT_DOC;
});

export function navigate(href: string, replace = false): void {
  const url = new URL(href, window.location.origin);
  const target = url.pathname + url.search + url.hash;
  if (replace) {
    window.history.replaceState({}, "", target);
  } else {
    window.history.pushState({}, "", target);
  }
  path.set(url.pathname);
  search.set(url.search);
}

// navigateToRef pins the current doc to a historical revision (or clears it
// when ref === null). Keeps the same path so reload/share-link works.
export function navigateToRef(ref: string | null): void {
  const url = new URL(window.location.href);
  if (ref) url.searchParams.set("ref", ref);
  else url.searchParams.delete("ref");
  navigate(url.pathname + url.search);
}

export function navigateToDoc(docPath: string): void {
  const clean = docPath.replace(/^\/+/, "");
  navigate(`/docs/${clean.split("/").map(encodeURIComponent).join("/")}`);
}

window.addEventListener("popstate", () => {
  path.set(pathFromLocation());
  search.set(searchFromLocation());
});
