<script lang="ts">
  import { Modal, Select, SelectItem, TextInput, PasswordInput, InlineNotification } from "carbon-components-svelte";
  import { getPrefs, patchPrefs, type Prefs } from "../lib/prefs-api";
  import { FONTS, DEFAULT_FONT, applyFont } from "../lib/fonts";
  import { _ } from "../lib/i18n";

  export let open = false;

  let prefs: Prefs | null = null;
  let font: string = DEFAULT_FONT;
  let pushRemote = "";
  let pushToken = "";
  let saving = false;
  let error: string | null = null;

  $: if (open && !prefs) {
    void load();
  }

  async function load() {
    try {
      prefs = await getPrefs();
      font = prefs.font || DEFAULT_FONT;
      pushRemote = prefs.push_remote;
      pushToken = "";
      error = null;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function submit() {
    saving = true;
    error = null;
    try {
      const patch: Record<string, string> = { font, push_remote: pushRemote };
      // Leave the stored token untouched unless the user typed a new one.
      if (pushToken !== "") patch.push_token = pushToken;
      prefs = await patchPrefs(patch);
      applyFont(prefs.font);
      open = false;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }
</script>

<Modal
  bind:open
  modalHeading={$_("preferences.heading")}
  primaryButtonText={saving ? $_("preferences.saving") : $_("preferences.save")}
  secondaryButtonText={$_("preferences.cancel")}
  primaryButtonDisabled={saving}
  on:click:button--secondary={() => (open = false)}
  on:submit={submit}
  on:close={() => (open = false)}
>
  {#if error}
    <InlineNotification kind="error" title={$_("preferences.error")} subtitle={error} hideCloseButton />
  {/if}

  {#if prefs}
    <p class="field-info">
      <strong>{$_("preferences.content_dir")}</strong><br />
      <code>{prefs.content_dir}</code><br />
      <span class="hint">{$_("preferences.content_dir_hint")}</span>
    </p>

    <Select labelText={$_("preferences.font")} bind:selected={font}>
      {#each FONTS as f (f.key)}
        <SelectItem value={f.key} text={f.label} />
      {/each}
    </Select>

    <div class="spacer"></div>

    <TextInput
      labelText={$_("preferences.push_remote")}
      placeholder="https://github.com/you/archive.git"
      helperText={$_("preferences.push_remote_hint")}
      bind:value={pushRemote}
    />

    <div class="spacer"></div>

    <PasswordInput
      labelText={$_("preferences.push_token")}
      placeholder={prefs.push_token_set ? $_("preferences.push_token_set") : ""}
      helperText={$_("preferences.push_token_hint")}
      bind:value={pushToken}
    />
  {/if}
</Modal>

<style>
  .field-info {
    margin-bottom: 1rem;
  }
  .hint {
    color: #6f6a60;
    font-size: 0.75rem;
  }
  .spacer {
    height: 1rem;
  }
</style>
