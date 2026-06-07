<script lang="ts">
  import {
    Accordion,
    AccordionItem,
    Tag,
    Button,
    InlineLoading,
  } from "carbon-components-svelte";
  import CheckmarkOutline from "carbon-icons-svelte/lib/CheckmarkOutline.svelte";
  import Reply from "carbon-icons-svelte/lib/Reply.svelte";
  import { comments, commentsLoading, commentsError, setStatus, requestedLine } from "../lib/comments-store";
  import { editing } from "../lib/document-store";
  import { focusLine } from "../lib/editor";
  import type { Comment } from "../lib/types";
  import { me } from "../lib/identity";
  import { _ } from "../lib/i18n";
  import { createEventDispatcher, tick } from "svelte";

  const dispatch = createEventDispatcher<{ reply: { parent: Comment } }>();

  $: grouped = group($comments);

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

  // Group by line, then sort within each group so a reply lands directly
  // under its parent. Replies pointing at parents that aren't on this line
  // (anchor diverged after a save) appear as roots at the bottom — they
  // still indent so the "reply" cue stays visible.
  function group(list: Comment[]): { line: number; items: Comment[] }[] {
    const byLine = new Map<number, Comment[]>();
    for (const c of list) {
      const arr = byLine.get(c.line_start) ?? [];
      arr.push(c);
      byLine.set(c.line_start, arr);
    }

    return [...byLine.entries()]
      .sort((a, b) => a[0] - b[0])
      .map(([line, items]) => {
        const byID = new Map(items.map((c) => [c.id, c]));
        const roots = items.filter((c) => !c.reply_to || !byID.has(c.reply_to));
        roots.sort((a, b) => a.created_at.localeCompare(b.created_at));
        const ordered: Comment[] = [];
        for (const root of roots) {
          ordered.push(root);
          const children = items
            .filter((c) => c.reply_to && c.reply_to === root.id)
            .sort((a, b) => a.created_at.localeCompare(b.created_at));
          ordered.push(...children);
        }
        return { line, items: ordered };
      });
  }

  function isReply(c: Comment): boolean {
    return c.reply_to != null;
  }

  function tagType(status: Comment["status"]): "blue" | "green" | "red" | "warm-gray" | "gray" {
    if (status === "open") return "blue";
    if (status === "resolved") return "green";
    if (status === "orphaned") return "red";
    if (status === "relocated") return "warm-gray";
    return "gray";
  }

  function statusLabel(status: Comment["status"]): string {
    return $_(`comments.status_${status}`);
  }

  async function resolve(c: Comment) {
    try {
      await setStatus(c.id, "resolved");
    } catch {
      // toast handled by caller in future; for now silently fail
    }
  }

  async function reopen(c: Comment) {
    try {
      await setStatus(c.id, "open");
    } catch {
      // ignore for now
    }
  }

  function jumpTo(line: number) {
    if ($editing) focusLine(line);
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
          {#each g.items as c (c.id)}
            <article class="comment" class:reply={isReply(c)}>
              <header>
                <strong>{c.author.display_name}</strong>
                <Tag type={tagType(c.status)} size="sm">{statusLabel(c.status)}</Tag>
                {#if isReply(c)}
                  <Tag type="cool-gray" size="sm">{$_("comments.reply_badge")}</Tag>
                {/if}
              </header>
              <p class="body">{c.body}</p>
              <footer>
                <button class="link" type="button" on:click={() => jumpTo(c.line_start)}>
                  {$_("comments.line_jump", { values: { line: c.line_start } })}
                </button>
                <span class="dim">·</span>
                <time class="dim">{new Date(c.created_at).toLocaleString()}</time>
                <span style="flex:1"></span>
                {#if $me && c.status !== "resolved"}
                  <Button kind="ghost" size="sm" icon={Reply} on:click={() => dispatch("reply", { parent: c })}>
                    {$_("comments.reply")}
                  </Button>
                {/if}
                {#if $me}
                  {#if c.status === "resolved"}
                    <Button kind="ghost" size="sm" on:click={() => reopen(c)}>
                      {$_("comments.reopen")}
                    </Button>
                  {:else}
                    <Button kind="ghost" size="sm" icon={CheckmarkOutline} on:click={() => resolve(c)}>
                      {$_("comments.resolve")}
                    </Button>
                  {/if}
                {/if}
              </footer>
            </article>
          {/each}
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
  .comment {
    padding: 0.5rem 0;
    border-bottom: 1px dashed #e0e0e0;
  }
  .comment.reply {
    padding-left: 1.25rem;
    border-left: 2px solid #cfc7b8;
    margin-left: 0.25rem;
  }
  .comment:last-child {
    border-bottom: 0;
  }
  .comment header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 0.25rem;
  }
  .comment .body {
    margin: 0 0 0.5rem 0;
    white-space: pre-wrap;
    color: #161616;
  }
  .comment footer {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.8rem;
  }
  .dim {
    color: #6f6a60;
  }
  .link {
    background: none;
    border: 0;
    color: #0f62fe;
    cursor: pointer;
    padding: 0;
    font: inherit;
  }
  .link:hover {
    text-decoration: underline;
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
