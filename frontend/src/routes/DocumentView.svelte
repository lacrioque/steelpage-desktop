<script lang="ts">
  import { tick, createEventDispatcher } from "svelte";
  import mermaid from "mermaid";
  import { codeMirror } from "../lib/editor";
  import { commentMarkers } from "../lib/read-markers";
  import { currentDoc, currentRef } from "../lib/router";
  import { _ } from "../lib/i18n";
  import {
    doc,
    draft,
    previewHtml,
    loading,
    error,
    notFoundPath,
    editing,
    setDraft,
    load,
    forceReload,
  } from "../lib/document-store";
  import NotFound from "./NotFound.svelte";
  import FileActions from "../components/FileActions.svelte";
  import { me } from "../lib/identity";

  const dispatch = createEventDispatcher<{
    markerclick: { line: number };
    addcomment: { line: number; anchorText: string };
  }>();

  mermaid.initialize({ startOnLoad: false });

  $: void load($currentDoc, $currentRef);

  $: void hydrate($previewHtml, $doc?.html);

  async function hydrate(_preview: string, _docHtml: string | undefined) {
    await tick();
    const nodes = document.querySelectorAll(".document-body .mermaid, .preview-body .mermaid");
    for (const node of nodes) {
      const el = node as HTMLElement;
      if (el.dataset.processed) continue;
      try {
        const id = `mermaid-${crypto.randomUUID()}`;
        const source = el.textContent ?? "";
        const result = await mermaid.render(id, source);
        el.innerHTML = result.svg;
        el.dataset.processed = "true";
      } catch {
        // Keep the original Mermaid text visible if rendering fails.
      }
    }
  }
</script>

{#if $loading}
  <div class="skeleton">
    <div class="bar" style="width:55%"></div>
    <div class="bar" style="width:90%"></div>
    <div class="bar" style="width:80%"></div>
    <div class="bar" style="width:70%"></div>
  </div>
{:else if $error}
  <section class="page-error">
    <h1>{$_("document.broke_title")}</h1>
    <p>{$error}</p>
    <button type="button" on:click={() => forceReload($currentDoc)}>{$_("document.retry")}</button>
  </section>
{:else if $notFoundPath}
  <NotFound path={$notFoundPath} onCreated={() => forceReload($currentDoc)} />
{:else if $doc}
  {#if $editing}
    {#if $me}
      <div class="editor-toolbar">
        <FileActions path={$doc.path} />
      </div>
    {/if}
    <section class="editor-grid">
      <div class="editor" use:codeMirror={{ value: $draft, onChange: setDraft }}></div>
      <article class="document-body preview-body">
        {@html $previewHtml}
      </article>
    </section>
  {:else}
    <article
      class="document-body"
      use:commentMarkers={{
        html: $doc.html,
        markdown: $doc.markdown,
        canComment: !$doc.viewing_ref,
        onMarkerClick: (line) => dispatch("markerclick", { line }),
        onAddComment: (line, anchorText) => dispatch("addcomment", { line, anchorText }),
      }}
    >
      {@html $doc.html}
    </article>
  {/if}
{/if}

<style>
  .skeleton {
    max-width: 820px;
    margin: 4rem auto;
    padding: 0 1.25rem;
  }
  .skeleton .bar {
    height: 1.2rem;
    margin-bottom: 0.6rem;
    border-radius: 0.4rem;
    background: linear-gradient(
      90deg,
      rgba(0, 0, 0, 0.06) 25%,
      rgba(0, 0, 0, 0.1) 50%,
      rgba(0, 0, 0, 0.06) 75%
    );
    background-size: 200% 100%;
    animation: pulse 1.4s ease-in-out infinite;
  }
  @keyframes pulse {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }
  .page-error {
    max-width: 540px;
    margin: 6rem auto;
    padding: 0 1.25rem;
    text-align: center;
  }
  .page-error button {
    margin-top: 1rem;
    border: 1px solid #cfc7b8;
    border-radius: 999px;
    background: white;
    padding: 0.4rem 1rem;
    cursor: pointer;
    font: inherit;
  }
  .editor-toolbar {
    display: flex;
    justify-content: flex-end;
    padding: 0.3rem 0.5rem;
    border-bottom: 1px solid #e0e0e0;
    background: #f4f4f4;
  }
  .editor-grid {
    display: grid;
    grid-template-columns: minmax(320px, 1fr) minmax(320px, 1fr);
    min-height: calc(100vh - 220px);
  }
  .editor {
    border-right: 1px solid #ded8cc;
    overflow: hidden;
  }
  @media (max-width: 860px) {
    .editor-grid {
      grid-template-columns: 1fr;
    }
    .editor {
      min-height: 45vh;
      border-right: 0;
      border-bottom: 1px solid #ded8cc;
    }
  }
</style>
