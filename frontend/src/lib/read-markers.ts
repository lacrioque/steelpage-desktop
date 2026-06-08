import { get } from "svelte/store";
import { comments } from "./comments-store";
import type { Comment } from "./types";
import {
  type Block,
  collectBlocks,
  blockForLine,
  lineCenterFraction,
  lineAtFraction,
} from "./block-geometry";

// commentMarkers is a Svelte action for the read-view `.document-body`
// article. The Go renderer tags rendered blocks with `data-source-line`
// (+ `-end`); this overlays a small dot in the left margin next to every
// block that has an active comment, mirroring the editor gutter. Clicking
// a dot calls onMarkerClick(line) so the shell can open the sidebar.
//
// A block can span several source lines (e.g. a soft-wrapped paragraph),
// so a marker's vertical position is interpolated within the block's
// rendered height from the line's offset in [start, end] — and the hover
// "+" reads back the line under the cursor the same way.

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

export function commentMarkers(article: HTMLElement, opts: ReadMarkerOptions) {
  let current = opts;
  const layer = document.createElement("div");
  layer.className = "sp-read-markers";
  // Each marker remembers the block + line it points at so reposition() can
  // re-interpolate as the layout reflows (fonts load, mermaid renders, resize).
  const markerInfo = new Map<HTMLElement, { block: Block; line: number }>();
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

  // top of `line` within `block`, in article-local coordinates: the vertical
  // centre of the line's proportional band across the block's rendered height.
  function lineTop(block: Block, line: number, base: DOMRect): number {
    const r = block.el.getBoundingClientRect();
    return r.top - base.top + article.scrollTop + lineCenterFraction(block, line) * r.height;
  }

  // inverse of lineTop: which source line does cursor Y fall on inside block.
  function lineAtY(block: Block, clientY: number): number {
    const r = block.el.getBoundingClientRect();
    if (r.height <= 0) return block.start;
    return lineAtFraction(block, (clientY - r.top) / r.height);
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
      // Anchor to the specific source line under the cursor, and snap the "+"
      // to that line's interpolated height so it lines up with the dot it'll
      // become.
      const line = lineAtY(blk, y);
      addBtn.dataset.line = String(line);
      addBtn.style.top = `${lineTop(blk, line, base) - 9}px`;
      addBtn.style.display = "";
    });
  }

  function onLeave() {
    cancelAnimationFrame(moveRaf);
    addBtn.style.display = "none";
  }

  function build(list: Comment[]) {
    // {@html} replaces innerHTML and removes our layer — re-attach it.
    if (layer.parentElement !== article) article.appendChild(layer);
    layer.replaceChildren();

    // Cache the tagged blocks for marker placement and hover hit-testing.
    blocks = collectBlocks(article);

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

    markerInfo.clear();
    let any = false;
    for (const [line, info] of byLine) {
      const block = blockForLine(blocks, line);
      if (!block) continue;
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
      markerInfo.set(btn, { block, line });
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
    // 10px dot → offset by 5 to centre it on the line's interpolated height.
    markerInfo.forEach(({ block, line }, btn) => {
      btn.style.top = `${lineTop(block, line, base) - 5}px`;
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
