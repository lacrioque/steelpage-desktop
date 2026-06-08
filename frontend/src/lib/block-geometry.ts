// Shared geometry for read-view comment anchoring. The Go renderer tags
// each rendered block with `data-source-line` (+ `-end`); these helpers map
// between source lines and vertical positions within a block's rendered
// height. Used by both the in-text gutter markers (read-markers.ts) and the
// anchored margin comments (MarginComments.svelte) so the two never drift.

export type Block = { el: HTMLElement; start: number; end: number };

export function collectBlocks(article: HTMLElement): Block[] {
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

// The block a source line belongs to: the tightest block whose range
// contains it, else the last block starting at or before it.
export function blockForLine(blocks: Block[], line: number): Block | null {
  if (blocks.length === 0) return null;
  let best: Block | null = null;
  for (const b of blocks) {
    if (line >= b.start && line <= b.end) {
      if (!best || b.end - b.start < best.end - best.start) best = b;
    }
  }
  if (best) return best;
  let fallback: Block | null = null;
  for (const b of blocks) {
    if (b.start <= line) fallback = b;
    else break;
  }
  return fallback ?? blocks[0];
}

function clampLine(block: Block, line: number): number {
  return Math.min(Math.max(line, block.start), block.end);
}

// Fraction (0..1) of the block's height for a line's TOP edge — used to
// anchor margin cards at the start of the line.
export function lineTopFraction(block: Block, line: number): number {
  const span = block.end - block.start + 1;
  return (clampLine(block, line) - block.start) / span;
}

// Fraction (0..1) for the CENTRE of a line's band — used by the gutter dots.
export function lineCenterFraction(block: Block, line: number): number {
  const span = block.end - block.start + 1;
  return (clampLine(block, line) - block.start + 0.5) / span;
}

// Inverse: which source line a fraction (0..1) of the block height falls on.
export function lineAtFraction(block: Block, frac: number): number {
  const span = block.end - block.start + 1;
  if (span <= 1) return block.start;
  const idx = Math.min(span - 1, Math.max(0, Math.floor(frac * span)));
  return block.start + idx;
}
