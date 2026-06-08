import { readable } from "svelte/store";

// marginFits is true when the window is wide enough for the anchored
// comments margin column; below it, the read view falls back to the
// accordion sidebar.
const QUERY = "(min-width: 1100px)";

export const marginFits = readable(true, (set) => {
  if (typeof window === "undefined" || !window.matchMedia) {
    set(true);
    return;
  }
  const mql = window.matchMedia(QUERY);
  set(mql.matches);
  const onChange = () => set(mql.matches);
  mql.addEventListener("change", onChange);
  return () => mql.removeEventListener("change", onChange);
});
