<script lang="ts">
  import {
    Modal,
    Select,
    SelectItem,
    TextInput,
    PasswordInput,
    RadioButtonGroup,
    RadioButton,
    Button,
    InlineNotification,
  } from "carbon-components-svelte";
  import { getPrefs, patchPrefs, testConnection, type Prefs } from "../lib/prefs-api";
  import { FONTS, DEFAULT_FONT, applyFont } from "../lib/fonts";
  import { _ } from "../lib/i18n";

  export let open = false;

  let prefs: Prefs | null = null;
  let font: string = DEFAULT_FONT;
  let mode: "local" | "server" = "local";
  let pushRemote = "";
  let pushToken = "";
  let serverUrl = "";
  let serverToken = "";
  let saving = false;
  let error: string | null = null;

  let testState: "idle" | "testing" | "ok" | "error" = "idle";
  let testMsg = "";

  $: if (open && !prefs) {
    void load();
  }

  async function load() {
    try {
      prefs = await getPrefs();
      font = prefs.font || DEFAULT_FONT;
      pushRemote = prefs.push_remote;
      pushToken = "";
      serverUrl = prefs.server_url;
      serverToken = "";
      mode = prefs.server_url ? "server" : "local";
      testState = "idle";
      testMsg = "";
      error = null;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function test() {
    testState = "testing";
    testMsg = "";
    try {
      const r = await testConnection(serverUrl.trim(), serverToken);
      if (r.ok) {
        testState = "ok";
        testMsg = r.user?.display_name ?? "";
      } else {
        testState = "error";
        testMsg = r.error ?? "failed";
      }
    } catch (e) {
      testState = "error";
      testMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function submit() {
    saving = true;
    error = null;
    try {
      const patch: Record<string, string> = { font };
      if (mode === "server") {
        patch.server_url = serverUrl.trim();
        if (serverToken !== "") patch.server_token = serverToken;
      } else {
        // Clearing the server URL switches back to the local archive.
        patch.server_url = "";
        patch.server_token = "";
        patch.push_remote = pushRemote;
        if (pushToken !== "") patch.push_token = pushToken;
      }
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
    <Select labelText={$_("preferences.font")} bind:selected={font}>
      {#each FONTS as f (f.key)}
        <SelectItem value={f.key} text={f.label} />
      {/each}
    </Select>

    <div class="spacer"></div>

    <RadioButtonGroup
      legendText={$_("preferences.connection")}
      bind:selected={mode}
      on:change={() => (testState = "idle")}
    >
      <RadioButton labelText={$_("preferences.connection_local")} value="local" />
      <RadioButton labelText={$_("preferences.connection_server")} value="server" />
    </RadioButtonGroup>
    <p class="hint">{$_("preferences.connection_hint")}</p>

    {#if mode === "server"}
      <TextInput
        labelText={$_("preferences.server_url")}
        placeholder="https://docs.example.com"
        bind:value={serverUrl}
      />
      <div class="spacer"></div>
      <PasswordInput
        labelText={$_("preferences.server_token")}
        placeholder={prefs.server_token_set ? $_("preferences.server_token_set") : "spt_…"}
        helperText={$_("preferences.server_token_hint")}
        bind:value={serverToken}
      />
      <div class="test-row">
        <Button
          kind="tertiary"
          size="small"
          disabled={testState === "testing" || !serverUrl.trim() || !serverToken}
          on:click={test}
        >
          {testState === "testing" ? $_("preferences.testing") : $_("preferences.test")}
        </Button>
        {#if testState === "ok"}
          <span class="test-ok">✓ {$_("preferences.test_ok", { values: { name: testMsg } })}</span>
        {:else if testState === "error"}
          <span class="test-error">✕ {testMsg}</span>
        {/if}
      </div>
    {:else}
      <p class="field-info">
        <strong>{$_("preferences.content_dir")}</strong><br />
        <code>{prefs.content_dir}</code><br />
        <span class="hint">{$_("preferences.content_dir_hint")}</span>
      </p>

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
  {/if}
</Modal>

<style>
  .field-info {
    margin: 1rem 0;
  }
  .hint {
    color: #6f6a60;
    font-size: 0.75rem;
    margin: 0.25rem 0 0;
  }
  .spacer {
    height: 1rem;
  }
  .test-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-top: 0.75rem;
  }
  .test-ok {
    color: #198038;
    font-size: 0.85rem;
  }
  .test-error {
    color: #da1e28;
    font-size: 0.85rem;
  }
</style>
