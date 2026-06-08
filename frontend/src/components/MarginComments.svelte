<script lang="ts">
  import { onMount, tick, createEventDispatcher } from "svelte";
  import ChevronDown from "carbon-icons-svelte/lib/ChevronDown.svelte";
  import ChevronUp from "carbon-icons-svelte/lib/ChevronUp.svelte";
  import { comments, groupByLine, requestedLine } from "../lib/comments-store";
  import {
    collectBlocks,
    blockForLine,
    lineTopFraction,
    lineCenterFraction,
  } from "../lib/block-geometry";
  import CommentThread from "./CommentThread.svelte";
  import type { Comment } from "../lib/types";
  import { _ } from "../lib/i18n";

  // The rendered article element to anchor against (carries data-source-line).
  export let article: HTMLElement | undefined = undefined;
  // Change trigger: re-layout when the rendered content is replaced.
  export let htmlVersion = "";

  const dispatch = createEventDispatcher<{ reply: { parent: Comment } }>();

  // GAP between stacked cards once collision pushes them apart.
  const GAP = 10;

  // Only draw a connector once the card is pushed this far from its anchor.
  const CONNECT_THRESHOLD = 8;

  type Placement = { line: number; items: Comment[]; idealTop: number; anchorY: number };
  type Connector = { line: number; d: string };
  let colEl: HTMLElement;
  let cardEls: HTMLElement[] = [];
  let placements: Placement[] = [];
  let connectors: Connector[] = [];
  let hoveredLine: number | null = null;
  let expanded = new Set<number>();

  $: groups = groupByLine($comments);

  // Re-layout whenever the inputs that affect anchoring change.
  $: void (groups, article, htmlVersion, expanded, scheduleLayout());

  let raf = 0;
  function scheduleLayout() {
    cancelAnimationFrame(raf);
    raf = requestAnimationFrame(layout);
  }

  // Pass 1: compute each card's ideal top (the anchored line position) and
  // render the cards there.
  async function layout() {
    if (!article || !colEl) {
      placements = [];
      return;
    }
    const blocks = collectBlocks(article);
    const colTop = colEl.getBoundingClientRect().top;
    placements = groups
      .map((g) => {
        const block = blockForLine(blocks, g.line);
        let idealTop = 0;
        let anchorY = 0;
        if (block) {
          const r = block.el.getBoundingClientRect();
          const blockTop = r.top - colTop;
          idealTop = blockTop + lineTopFraction(block, g.line) * r.height;
          // Match the in-text gutter dot, which sits at the line's centre.
          anchorY = blockTop + lineCenterFraction(block, g.line) * r.height;
        }
        return { line: g.line, items: g.items, idealTop, anchorY };
      })
      .sort((a, b) => a.idealTop - b.idealTop);

    // Pass 2: measure rendered heights and resolve collisions.
    await tick();
    place();
  }

  // Sweep top→bottom: each card sits at its ideal top, pushed down only as
  // far as needed to clear the previous card. Order is preserved. Then build
  // a connector from each displaced card back to its anchor dot.
  function place() {
    if (!article || !colEl) return;
    const colLeft = colEl.getBoundingClientRect().left;
    // x of the in-text gutter dot, in column-local coordinates.
    const anchorX = article.getBoundingClientRect().left - colLeft + 6;
    const cardRight = colEl.clientWidth; // cards span the column width

    const next: Connector[] = [];
    let prevBottom = -Infinity;
    placements.forEach((p, i) => {
      const el = cardEls[i];
      if (!el) return;
      const top = Math.max(p.idealTop, prevBottom + GAP);
      el.style.top = `${top}px`;
      prevBottom = top + el.offsetHeight;

      const cardY = top + 14; // aim at the card's first line
      if (Math.abs(cardY - p.anchorY) > CONNECT_THRESHOLD) {
        const xc = (cardRight + anchorX) / 2;
        next.push({
          line: p.line,
          d: `M ${cardRight} ${cardY} C ${xc} ${cardY} ${xc} ${p.anchorY} ${anchorX} ${p.anchorY}`,
        });
      }
    });
    connectors = next;
  }

  function toggle(line: number) {
    const next = new Set(expanded);
    next.has(line) ? next.delete(line) : next.add(line);
    expanded = next;
  }

  function visibleItems(p: Placement): Comment[] {
    return expanded.has(p.line) ? p.items : p.items.slice(0, 1);
  }

  function allResolved(p: Placement): boolean {
    return p.items.every((c) => c.status === "resolved");
  }

  // A marker dot (or another jump) asked for a line — scroll its card into
  // view and flash it.
  $: void reveal($requestedLine);
  async function reveal(req: { line: number; nonce: number } | null) {
    if (!req) return;
    await tick();
    const el = document.getElementById(`sp-margin-line-${req.line}`);
    if (!el) return;
    el.scrollIntoView({ behavior: "smooth", block: "center" });
    el.classList.add("flash");
    hoveredLine = req.line;
    setTimeout(() => {
      el.classList.remove("flash");
      if (hoveredLine === req.line) hoveredLine = null;
    }, 1200);
  }

  onMount(() => {
    const ro = new ResizeObserver(scheduleLayout);
    if (article) ro.observe(article);
    window.addEventListener("resize", scheduleLayout);
    scheduleLayout();
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", scheduleLayout);
      cancelAnimationFrame(raf);
    };
  });
</script>

<div class="margin-col" bind:this={colEl}>
  <svg class="connectors" aria-hidden="true">
    {#each connectors as c (c.line)}
      <path d={c.d} class:active={hoveredLine === c.line} />
    {/each}
  </svg>
  {#each placements as p, i (p.line)}
    <div
      class="margin-card"
      class:resolved={allResolved(p)}
      id={`sp-margin-line-${p.line}`}
      bind:this={cardEls[i]}
      style="top:{p.idealTop}px"
      on:mouseenter={() => (hoveredLine = p.line)}
      on:mouseleave={() => (hoveredLine = null)}
      role="group"
    >
      <CommentThread items={visibleItems(p)} showJump={false} on:reply />
      {#if p.items.length > 1}
        <button class="more" type="button" on:click={() => toggle(p.line)}>
          {#if expanded.has(p.line)}
            <ChevronUp size={16} /> {$_("comments.collapse")}
          {:else}
            <ChevronDown size={16} />
            {$_("comments.more", { values: { count: p.items.length - 1 } })}
          {/if}
        </button>
      {/if}
    </div>
  {/each}
</div>

<style>
  .margin-col {
    position: relative;
    height: 100%;
  }
  .connectors {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    overflow: visible;
    pointer-events: none;
  }
  .connectors path {
    fill: none;
    stroke: #c6c0b0;
    stroke-width: 1.5;
  }
  .connectors path.active {
    stroke: #0f62fe;
    stroke-width: 2;
  }
  .margin-card {
    position: absolute;
    right: 0;
    width: 100%;
    background: #ffffff;
    border: 1px solid #e0e0e0;
    border-radius: 0.4rem;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.06);
    padding: 0.4rem 0.6rem;
    font-size: 0.85rem;
    transition: box-shadow 120ms ease;
  }
  .margin-card:hover {
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12);
    border-color: #b9b3a4;
  }
  .margin-card.resolved {
    opacity: 0.6;
  }
  .margin-card.flash {
    animation: sp-card-flash 1.2s ease;
  }
  @keyframes sp-card-flash {
    0%,
    100% {
      box-shadow: 0 1px 3px rgba(0, 0, 0, 0.06);
    }
    25% {
      box-shadow: 0 0 0 2px #0f62fe;
    }
  }
  .more {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    margin-top: 0.25rem;
    background: none;
    border: 0;
    color: #0f62fe;
    cursor: pointer;
    font: inherit;
    font-size: 0.8rem;
    padding: 0;
  }
  .more:hover {
    text-decoration: underline;
  }
</style>
