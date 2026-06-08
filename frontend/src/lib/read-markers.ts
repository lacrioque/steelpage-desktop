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
  // Source body (frontmatter-stripped) — line N's text is the comment anchor.
  markdown: string;
  // False while viewing a historical revision: no new comments there.
  canComment: boolean;
  onMarkerClick: (line: number) => void;
  onAddComment: (line: number, anchorText: string) => void;
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

  // Hover "+" affordance: a single button that follows the cursor in the
  // gutter and adds a comment on the block under it.
  let blocks: Block[] = [];
  const addBtn = document.createElement("button");
  addBtn.type = "button";
  addBtn.className = "sp-add-comment";
  addBtn.title = "Add a comment here";
  addBtn.textContent = "+";
  addBtn.style.display = "none";
  addBtn.addEventListener("click", () => {
    const line = parseInt(addBtn.dataset.line ?? "", 10);
    if (!Number.isFinite(line)) return;
    const lines = current.markdown.split(/\r?\n/);
    current.onAddComment(line, lines[line - 1] ?? "");
  });

  function blockUnderY(clientY: number): Block | null {
    let best: Block | null = null;
    let bestH = Infinity;
    for (const b of blocks) {
      const r = b.el.getBoundingClientRect();
      if (clientY >= r.top && clientY <= r.bottom && r.bottom - r.top < bestH) {
        best = b;
        bestH = r.bottom - r.top;
      }
    }
    return best;
  }

  let moveRaf = 0;
  function onMove(e: MouseEvent) {
    if (!current.canComment) {
      addBtn.style.display = "none";
      return;
    }
    // Don't reposition while the cursor is on the button itself, or it slips
    // away mid-click.
    if (e.target === addBtn) return;
    // Throttle the per-block hit-test (each reads a rect → forces layout) to
    // one pass per frame.
    const y = e.clientY;
    cancelAnimationFrame(moveRaf);
    moveRaf = requestAnimationFrame(() => {
      const blk = blockUnderY(y);
      if (!blk) {
        addBtn.style.display = "none";
        return;
      }
      const base = article.getBoundingClientRect();
      addBtn.dataset.line = String(blk.start);
      addBtn.style.top = `${y - base.top + article.scrollTop - 9}px`;
      addBtn.style.display = "";
    });
  }

  function onLeave() {
    cancelAnimationFrame(moveRaf);
    addBtn.style.display = "none";
  }

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

    // Cache the tagged blocks for marker placement and hover hit-testing.
    blocks = collectBlocks();

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

    // Keep the hover "+" in the layer (replaceChildren above removed it).
    addBtn.style.display = "none";
    layer.appendChild(addBtn);

    // The gutter overlay (and its absolute children) needs the article as a
    // positioning context whenever any affordance is active.
    article.classList.toggle("sp-has-markers", any || current.canComment);
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
  article.addEventListener("mousemove", onMove);
  article.addEventListener("mouseleave", onLeave);

  return {
    update(next: ReadMarkerOptions) {
      const htmlChanged = next.html !== current.html;
      const canChanged = next.canComment !== current.canComment;
      current = next;
      if (htmlChanged) {
        // Rebuild after Svelte has swapped in the new innerHTML.
        requestAnimationFrame(() => build(get(comments)));
      } else if (canChanged) {
        build(get(comments));
      }
    },
    destroy() {
      unsub();
      ro.disconnect();
      window.removeEventListener("resize", schedulePosition);
      article.removeEventListener("mousemove", onMove);
      article.removeEventListener("mouseleave", onLeave);
      cancelAnimationFrame(raf);
      cancelAnimationFrame(moveRaf);
      layer.remove();
      article.classList.remove("sp-has-markers");
    },
  };
}
