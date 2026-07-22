<script>
  import { get } from "../lib/api.js";
  import { Alert, AlertDescription, AlertTitle } from "./ui/alert/index.js";
  import { Button } from "./ui/button/index.js";
  import * as Dialog from "./ui/dialog/index.js";
  import { Input } from "./ui/input/index.js";
  import { Skeleton } from "./ui/skeleton/index.js";

  let { me, onselect, title = "Choose media", description = "Choose a ready library item for this room.", action = "Use media" } = $props();
  let media = $state([]);
  let mediaState = $state("loading");
  let selectedId = $state(null);
  let filter = $state("");
  let error = $state("");
  let creating = $state(false);

  const selectedMedia = $derived(media.find((item) => item.id === selectedId));
  const shownMedia = $derived(media.filter((item) => item.title.toLowerCase().includes(filter.trim().toLowerCase())));

  function formatSize(bytes) {
    if (!bytes) return "0 bytes";
    const units = ["bytes", "KB", "MB", "GB", "TB"];
    const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1000)), units.length - 1);
    const value = bytes / 1000 ** index;
    return `${value >= 10 || index === 0 ? Math.round(value) : value.toFixed(1)} ${units[index]}`;
  }

  async function load() {
    mediaState = "loading";
    error = "";
    try {
      media = (await get("/api/media")).filter((item) => item.status === "ready");
      mediaState = "ready";
    } catch (err) {
      error = err.message;
      mediaState = "error";
    }
  }

  $effect(() => { load(); });

  async function select(event) {
    event.preventDefault();
    if (!selectedMedia) return;
    creating = true;
    error = "";
    try {
      await onselect(selectedMedia);
    } catch (err) {
      error = err.message;
    } finally {
      creating = false;
    }
  }
</script>

<Dialog.Content class="max-w-xl" showCloseButton={!creating}>
  <Dialog.Header>
    <Dialog.Title>{title}</Dialog.Title>
    <Dialog.Description>{description}</Dialog.Description>
  </Dialog.Header>
  <form onsubmit={select} class="flex flex-col gap-4">
    <div class="flex items-end justify-between gap-3">
      <span class="text-sm font-medium text-fg-strong">Pick media</span>
      <label class="w-48"><span class="sr-only">Filter media</span><Input class="h-11" bind:value={filter} placeholder="Filter media" disabled={creating} /></label>
    </div>
    {#if mediaState === "loading"}
      <div class="flex flex-col gap-2" aria-label="Loading media">{#each [1, 2, 3] as item (item)}<Skeleton class="h-14 w-full" />{/each}</div>
    {:else if mediaState === "error"}
      <Alert variant="destructive"><AlertTitle>Couldn't load the library.</AlertTitle><AlertDescription class="flex items-center justify-between gap-3"><span>{error}</span><Button variant="outline" class="h-11" onclick={load}>Retry</Button></AlertDescription></Alert>
    {:else if media.length === 0}
      <p class="py-6 text-center text-sm">Library is empty. {#if me.role === "admin"}<Button variant="link" class="h-auto px-0" onclick={() => location.hash = "/admin"}>Upload media in Admin.</Button>{:else}Ask your admin to upload something.{/if}</p>
    {:else}
      <fieldset class="flex max-h-64 flex-col gap-2 overflow-y-auto" disabled={creating}>
        <legend class="sr-only">Ready media</legend>
        {#each shownMedia as item (item.id)}
          <label class="flex min-h-11 cursor-pointer items-center gap-3 rounded-md border border-border bg-card p-3 transition-colors duration-fast hover:bg-input has-[:checked]:border-secondary">
            <input type="radio" name="media" value={item.id} bind:group={selectedId} />
            <span class="min-w-0 flex-1"><span class="block truncate font-medium text-fg-strong">{item.title}</span><span class="font-mono text-sm">{item.kind} · {formatSize(item.sizeBytes)}</span></span>
          </label>
        {:else}
          <p class="py-4 text-center text-sm">No ready items match that filter.</p>
        {/each}
      </fieldset>
    {/if}
    {#if error && mediaState === "ready"}<p class="text-sm text-error" role="alert">{error}</p>{/if}
    <Dialog.Footer>
      <Dialog.Close>
        {#snippet child({ props })}<Button variant="outline" class="h-11" disabled={creating} {...props}>Cancel</Button>{/snippet}
      </Dialog.Close>
      <Button class="h-11" type="submit" disabled={!selectedMedia || creating || mediaState !== "ready"}>{creating ? "Saving…" : action}</Button>
    </Dialog.Footer>
  </form>
</Dialog.Content>
