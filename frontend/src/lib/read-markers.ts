import { get } from "svelte/store";
import { comments } from "./comments-store";
import type { Comment } from "./types";

// commentMarkers is a Svelte action for the read-view `.document-body`
// article. The Go renderer tags rendered blocks with `data-source-line`
// (+ `-end`); this overlays a small dot in the left margin next to every
// block that has an active comment, mirroring the editor gutter. Clicking
// a dot calls onMarkerClick(line) so the shell can open the sidebar.

export type ReadMarkerOptions = {
  // `html` is only a change trigger: when the rendered content is replaced
  // we rebuild the markers (the {@html} swap wipes our overlay layer).
  html: string;
  onMarkerClick: (line: number) => void;
};

type MarkerKind = "open" | "relocated";
type Block = { el: HTMLElement; start: number; end: number };

export function commentMarkers(article: HTMLElement, opts: ReadMarkerOptions) {
  let current = opts;
  const layer = document.createElement("div");
  layer.className = "sp-read-markers";
  // Each marker remembers the block it points at so reposition() can follow
  // it as the layout reflows (fonts load, mermaid renders, window resizes).
  const targets = new WeakMap<HTMLElement, HTMLElement>();
  let raf = 0;

  function collectBlocks(): Block[] {
    const blocks: Block[] = [];
    article.querySelectorAll<HTMLElement>("[data-source-line]").forEach((el) => {
      const start = parseInt(el.getAttribute("data-source-line") ?? "", 10);
      if (!Number.isFinite(start)) return;
      const endAttr = el.getAttribute("data-source-line-end");
      const end = endAttr ? parseInt(endAttr, 10) : start;
      blocks.push({ el, start, end: Number.isFinite(end) ? end : start });
    });
    blocks.sort((a, b) => a.start - b.start);
    return blocks;
  }

  function targetFor(line: number, blocks: Block[]): HTMLElement | null {
    if (blocks.length === 0) return null;
    // Prefer the tightest block whose source range contains the line.
    let best: Block | null = null;
    for (const b of blocks) {
      if (line >= b.start && line <= b.end) {
        if (!best || b.end - b.start < best.end - best.start) best = b;
      }
    }
    if (best) return best.el;
    // Otherwise the last block that starts at or before the line.
    let fallback: Block | null = null;
    for (const b of blocks) {
      if (b.start <= line) fallback = b;
      else break;
    }
    return (fallback ?? blocks[0]).el;
  }

  function build(list: Comment[]) {
    // {@html} replaces innerHTML and removes our layer — re-attach it.
    if (layer.parentElement !== article) article.appendChild(layer);
    layer.replaceChildren();

    const byLine = new Map<number, { kind: MarkerKind; count: number }>();
    for (const c of list) {
      if (c.status !== "open" && c.status !== "relocated") continue;
      const cur = byLine.get(c.line_start);
      if (!cur) byLine.set(c.line_start, { kind: c.status, count: 1 });
      else {
        cur.count += 1;
        if (c.status === "relocated") cur.kind = "relocated";
      }
    }

    const blocks = collectBlocks();
    let any = false;
    for (const [line, info] of byLine) {
      const target = targetFor(line, blocks);
      if (!target) continue;
      any = true;
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = `sp-read-marker sp-read-marker-${info.kind}`;
      btn.title =
        info.count === 1
          ? "1 comment on this line — click to open"
          : `${info.count} comments on this line — click to open`;
      btn.dataset.line = String(line);
      btn.addEventListener("click", () => current.onMarkerClick(line));
      targets.set(btn, target);
      layer.appendChild(btn);
    }

    article.classList.toggle("sp-has-markers", any);
    position();
  }

  function position() {
    const base = article.getBoundingClientRect();
    // Stack multiple markers that resolve to the same block.
    const seen = new Map<HTMLElement, number>();
    layer.querySelectorAll<HTMLElement>(".sp-read-marker").forEach((btn) => {
      const target = targets.get(btn);
      if (!target) return;
      const r = target.getBoundingClientRect();
      const top = r.top - base.top + article.scrollTop;
      const idx = seen.get(target) ?? 0;
      seen.set(target, idx + 1);
      btn.style.top = `${top + 6 + idx * 14}px`;
    });
  }

  function schedulePosition() {
    cancelAnimationFrame(raf);
    raf = requestAnimationFrame(position);
  }

  const unsub = comments.subscribe((list) => build(list));
  const ro = new ResizeObserver(schedulePosition);
  ro.observe(article);
  window.addEventListener("resize", schedulePosition);

  return {
    update(next: ReadMarkerOptions) {
      const htmlChanged = next.html !== current.html;
      current = next;
      if (htmlChanged) {
        // Rebuild after Svelte has swapped in the new innerHTML.
        requestAnimationFrame(() => build(get(comments)));
      }
    },
    destroy() {
      unsub();
      ro.disconnect();
      window.removeEventListener("resize", schedulePosition);
      cancelAnimationFrame(raf);
      layer.remove();
      article.classList.remove("sp-has-markers");
    },
  };
}
