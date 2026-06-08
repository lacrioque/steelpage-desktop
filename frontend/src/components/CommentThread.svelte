<script lang="ts">
  import { Tag, Button } from "carbon-components-svelte";
  import CheckmarkOutline from "carbon-icons-svelte/lib/CheckmarkOutline.svelte";
  import Reply from "carbon-icons-svelte/lib/Reply.svelte";
  import { setStatus } from "../lib/comments-store";
  import { editing } from "../lib/document-store";
  import { focusLine } from "../lib/editor";
  import type { Comment } from "../lib/types";
  import { me } from "../lib/identity";
  import { _ } from "../lib/i18n";
  import { createEventDispatcher } from "svelte";

  // The ordered comments to render (roots followed by their replies). The
  // parent decides how many to pass — the margin card slices for its
  // collapsed preview, the accordion passes all.
  export let items: Comment[] = [];
  // Show the "jump to line" link (useful in the editor; no-op in read view).
  export let showJump = true;

  const dispatch = createEventDispatcher<{ reply: { parent: Comment } }>();

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
      // surfaced elsewhere; ignore for now
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

{#each items as c (c.id)}
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
      {#if showJump}
        <button class="link" type="button" on:click={() => jumpTo(c.line_start)}>
          {$_("comments.line_jump", { values: { line: c.line_start } })}
        </button>
        <span class="dim">·</span>
      {/if}
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

<style>
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
</style>
