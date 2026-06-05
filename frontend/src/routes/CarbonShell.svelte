<script lang="ts">
  import { onMount } from "svelte";
  import {
    Header,
    HeaderUtilities,
    HeaderGlobalAction,
    SideNav,
    Content,
    SkipToContent,
    InlineLoading,
    Breadcrumb,
    BreadcrumbItem,
  } from "carbon-components-svelte";
  import Edit from "carbon-icons-svelte/lib/Edit.svelte";
  import View from "carbon-icons-svelte/lib/View.svelte";
  import Save from "carbon-icons-svelte/lib/Save.svelte";
  import Printer from "carbon-icons-svelte/lib/Printer.svelte";
  import Bot from "carbon-icons-svelte/lib/Bot.svelte";
  import ChatLaunch from "carbon-icons-svelte/lib/ChatLaunch.svelte";
  import AddComment from "carbon-icons-svelte/lib/AddComment.svelte";
  import SearchIcon from "carbon-icons-svelte/lib/Search.svelte";

  import ArchiveTree from "../components/ArchiveTree.svelte";
  import DocumentView from "./DocumentView.svelte";
  import CommentsSidebar from "../components/CommentsSidebar.svelte";
  import AddCommentModal from "../components/AddCommentModal.svelte";
  import LocaleToggle from "../components/LocaleToggle.svelte";
  import SearchOverlay from "../components/SearchOverlay.svelte";
  import VersionMenu from "../components/VersionMenu.svelte";
  import { currentDoc, navigateToDoc } from "../lib/router";
  import {
    doc as docStore,
    editing,
    saveState,
    saveError,
    toggleEdit,
    save,
  } from "../lib/document-store";
  import {
    getCurrentLine,
    focusLine,
    setOnMarkerClick,
    setOnEmptyGutterClick,
  } from "../lib/editor";
  import { _ } from "../lib/i18n";

  let isSideNavOpen = false;
  let showComments = false;
  let addCommentOpen = false;
  let addCommentLine = 1;
  let addCommentAnchor = "";
  let addCommentReplyTo: number | null = null;
  let addCommentReplyAuthor = "";
  let searchOpen = false;

  // Replies start from the parent comment's anchor — same line + same
  // captured text — so they re-anchor along with the conversation.
  function startReply(event: CustomEvent<{ parent: import("../lib/types").Comment }>) {
    const p = event.detail.parent;
    addCommentLine = p.line_start;
    addCommentAnchor = p.anchor_text;
    addCommentReplyTo = p.id;
    addCommentReplyAuthor = p.author.display_name;
    addCommentOpen = true;
  }

  function onKeydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
      e.preventDefault();
      searchOpen = true;
    }
  }

  onMount(() => {
    window.addEventListener("keydown", onKeydown);

    setOnMarkerClick((line: number) => {
      showComments = true;
      focusLine(line);
    });

    setOnEmptyGutterClick((line: number, text: string) => {
      addCommentLine = line;
      addCommentAnchor = text;
      addCommentReplyTo = null;
      addCommentReplyAuthor = "";
      addCommentOpen = true;
    });

    return () => {
      window.removeEventListener("keydown", onKeydown);
      setOnMarkerClick(null);
      setOnEmptyGutterClick(null);
    };
  });

  function openAddComment() {
    const current = getCurrentLine();
    if (current) {
      addCommentLine = current.number;
      addCommentAnchor = current.text;
    } else {
      addCommentLine = 1;
      addCommentAnchor = "";
    }
    addCommentReplyTo = null;
    addCommentReplyAuthor = "";
    addCommentOpen = true;
  }

  function segments(path: string): { name: string; path: string }[] {
    const parts = path.split("/").filter(Boolean);
    return parts.map((name, idx) => ({
      name,
      path: parts.slice(0, idx + 1).join("/"),
    }));
  }
</script>

<Header platformName={$_("shell.platform_name")} href="/" bind:isSideNavOpen>
  <svelte:fragment slot="skip-to-content">
    <SkipToContent />
  </svelte:fragment>

  <HeaderUtilities>
    {#if $docStore}
      {#if $editing && !$docStore.viewing_ref}
        <HeaderGlobalAction
          iconDescription={$_("shell.add_comment_current_line")}
          icon={AddComment}
          on:click={openAddComment}
        />
      {/if}

      {#if !$docStore.viewing_ref}
        <HeaderGlobalAction
          iconDescription={$editing ? $_("shell.switch_to_read") : $_("shell.switch_to_edit")}
          icon={$editing ? View : Edit}
          on:click={toggleEdit}
        />
      {/if}

      {#if $editing && !$docStore.viewing_ref}
        <HeaderGlobalAction
          iconDescription={$_("shell.save")}
          icon={Save}
          on:click={save}
          isActive={$saveState === "saving"}
        />
      {/if}

      <HeaderGlobalAction
        iconDescription={$_("shell.print")}
        icon={Printer}
        on:click={() => window.print()}
      />

      <HeaderGlobalAction
        iconDescription={$_("shell.toggle_comments")}
        icon={ChatLaunch}
        isActive={showComments}
        on:click={() => (showComments = !showComments)}
      />

      <HeaderGlobalAction
        iconDescription={$_("shell.bot_ready")}
        icon={Bot}
        on:click={() => window.open(`/docs/${$docStore.path}?botready=1`, "_blank")}
      />
    {/if}

    <HeaderGlobalAction
      iconDescription={$_("search.open")}
      icon={SearchIcon}
      on:click={() => (searchOpen = true)}
    />

    <LocaleToggle />
  </HeaderUtilities>
</Header>

<SideNav bind:isOpen={isSideNavOpen} aria-label={$_("shell.archive_nav_label")}>
  <ArchiveTree />
</SideNav>

<Content id="main-content">
  <div class="bread">
    <Breadcrumb noTrailingSlash>
      <BreadcrumbItem
        href="/docs/README.md"
        on:click={(e) => {
          e.preventDefault();
          navigateToDoc("README.md");
        }}
      >
        {$_("shell.breadcrumb_root")}
      </BreadcrumbItem>
      {#each segments($currentDoc) as seg, i (seg.path)}
        <BreadcrumbItem
          href={`/docs/${seg.path}`}
          isCurrentPage={i === segments($currentDoc).length - 1}
          on:click={(e) => {
            e.preventDefault();
            navigateToDoc(seg.path);
          }}
        >
          {seg.name}
        </BreadcrumbItem>
      {/each}
    </Breadcrumb>

    <div style="flex:1"></div>

    {#if $saveState === "saving"}
      <InlineLoading status="active" description={$_("shell.save_status_saving")} />
    {:else if $saveState === "saved"}
      <InlineLoading status="finished" description={$_("shell.save_status_saved")} />
    {:else if $saveState === "error"}
      <InlineLoading status="error" description={$saveError || $_("shell.save_status_failed")} />
    {/if}

    {#if $docStore}
      <VersionMenu
        path={$docStore.path}
        version={($docStore.frontmatter.version ?? "?") as string | number}
        sha={$docStore.sha}
        viewingRef={$docStore.viewing_ref}
      />
    {/if}
  </div>

  <div class="layout" class:with-comments={showComments && $docStore}>
    <section class="doc">
      <DocumentView />
    </section>
    {#if showComments && $docStore}
      <CommentsSidebar on:reply={startReply} />
    {/if}
  </div>
</Content>

{#if $docStore}
  <AddCommentModal
    bind:open={addCommentOpen}
    path={$docStore.path}
    lineNumber={addCommentLine}
    anchorText={addCommentAnchor}
    replyTo={addCommentReplyTo}
    replyToAuthor={addCommentReplyAuthor}
  />
{/if}

<SearchOverlay bind:open={searchOpen} />

<style>
  .bread {
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0.5rem 0 1rem;
    flex-wrap: wrap;
  }
  .layout {
    display: grid;
    grid-template-columns: 1fr;
    gap: 0;
    min-height: calc(100vh - 200px);
  }
  .layout.with-comments {
    grid-template-columns: 1fr 320px;
  }
  .doc {
    min-width: 0;
  }
  @media (max-width: 1000px) {
    .layout.with-comments {
      grid-template-columns: 1fr;
    }
  }
</style>
