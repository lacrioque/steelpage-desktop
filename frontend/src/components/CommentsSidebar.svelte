<script lang="ts">
  import { Accordion, AccordionItem, InlineLoading } from "carbon-components-svelte";
  import { comments, commentsLoading, commentsError, requestedLine, groupByLine } from "../lib/comments-store";
  import CommentThread from "./CommentThread.svelte";
  import { _ } from "../lib/i18n";
  import { tick } from "svelte";

  $: grouped = groupByLine($comments);

  // When a read-view marker asks for a line, open that group and scroll it
  // into view with a brief highlight.
  let activeLine: number | null = null;
  $: void revealLine($requestedLine);

  async function revealLine(req: { line: number; nonce: number } | null) {
    if (!req) return;
    activeLine = req.line;
    await tick();
    const el = document.getElementById(`sp-comment-line-${req.line}`);
    if (!el) return;
    el.scrollIntoView({ behavior: "smooth", block: "center" });
    el.classList.add("flash");
    setTimeout(() => el.classList.remove("flash"), 1200);
  }
</script>

<aside class="comments">
  <header>
    <h3>{$_("comments.heading")}</h3>
    {#if $commentsLoading}
      <InlineLoading status="active" description={$_("comments.loading")} />
    {:else}
      <span class="count">{$comments.length}</span>
    {/if}
  </header>

  {#if $commentsError}
    <p class="error">{$commentsError}</p>
  {:else if $comments.length === 0 && !$commentsLoading}
    <p class="empty">{$_("comments.empty")}</p>
  {:else}
    <Accordion>
      {#each grouped as g (g.line)}
        <AccordionItem
          id={`sp-comment-line-${g.line}`}
          open={activeLine === g.line}
          title={$_("comments.line_group", { values: { line: g.line, count: g.items.length } })}
        >
          <CommentThread items={g.items} on:reply />
        </AccordionItem>
      {/each}
    </Accordion>
  {/if}
</aside>

<style>
  aside.comments {
    padding: 1rem;
    border-left: 1px solid #e0e0e0;
    background: #ffffff;
    height: 100%;
    overflow-y: auto;
  }
  aside.comments > header {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    margin-bottom: 0.5rem;
  }
  aside.comments h3 {
    margin: 0;
    font-size: 0.95rem;
    font-weight: 600;
    letter-spacing: 0.02em;
    text-transform: uppercase;
    color: #525252;
  }
  .count {
    color: #525252;
    font-size: 0.8rem;
    background: #f4f4f4;
    padding: 0.1rem 0.5rem;
    border-radius: 999px;
  }
  .empty {
    color: #6f6a60;
    font-size: 0.9rem;
    padding: 0.5rem 0;
  }
  .error {
    color: #9b1c1c;
    padding: 0.5rem 0;
  }
  :global(.comments .bx--accordion__item.flash) {
    animation: sp-flash 1.2s ease;
  }
  @keyframes sp-flash {
    0%,
    100% {
      background: transparent;
    }
    25% {
      background: rgba(15, 98, 254, 0.12);
    }
  }
</style>
