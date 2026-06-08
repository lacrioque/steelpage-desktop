<script lang="ts">
  import { onMount, tick, createEventDispatcher } from "svelte";
  import ChevronDown from "carbon-icons-svelte/lib/ChevronDown.svelte";
  import ChevronUp from "carbon-icons-svelte/lib/ChevronUp.svelte";
  import { comments, groupByLine } from "../lib/comments-store";
  import { collectBlocks, blockForLine, lineTopFraction } from "../lib/block-geometry";
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

  type Placement = { line: number; items: Comment[]; idealTop: number };
  let colEl: HTMLElement;
  let cardEls: HTMLElement[] = [];
  let placements: Placement[] = [];
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
        if (block) {
          const r = block.el.getBoundingClientRect();
          idealTop = r.top - colTop + lineTopFraction(block, g.line) * r.height;
        }
        return { line: g.line, items: g.items, idealTop };
      })
      .sort((a, b) => a.idealTop - b.idealTop);

    // Pass 2: measure rendered heights and resolve collisions.
    await tick();
    place();
  }

  // Sweep top→bottom: each card sits at its ideal top, pushed down only as
  // far as needed to clear the previous card. Order is preserved.
  function place() {
    let prevBottom = -Infinity;
    placements.forEach((p, i) => {
      const el = cardEls[i];
      if (!el) return;
      const top = Math.max(p.idealTop, prevBottom + GAP);
      el.style.top = `${top}px`;
      prevBottom = top + el.offsetHeight;
    });
  }

  function toggle(line: number) {
    const next = new Set(expanded);
    next.has(line) ? next.delete(line) : next.add(line);
    expanded = next;
  }

  function visibleItems(p: Placement): Comment[] {
    return expanded.has(p.line) ? p.items : p.items.slice(0, 1);
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
  {#each placements as p, i (p.line)}
    <div
      class="margin-card"
      id={`sp-margin-line-${p.line}`}
      bind:this={cardEls[i]}
      style="top:{p.idealTop}px"
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
