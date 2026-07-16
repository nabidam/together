<script>
  import { get, post } from "../lib/api.js";
  import { go } from "../lib/router.svelte.js";
  import { Clapperboard, DoorOpen, LogOut, Music2, Plus, Shield } from "lucide-svelte";
  import { Alert, AlertDescription, AlertTitle } from "../components/ui/alert/index.js";
  import { Button } from "../components/ui/button/index.js";
  import { Card, CardContent } from "../components/ui/card/index.js";
  import * as Dialog from "../components/ui/dialog/index.js";
  import { Skeleton } from "../components/ui/skeleton/index.js";
  import MediaPickerDialog from "../components/MediaPickerDialog.svelte";

  let { me, onlogout } = $props();
  let rooms = $state([]);
  let loadState = $state("loading");
  let error = $state("");
  let dialogOpen = $state(false);

  async function load() {
    loadState = "loading";
    error = "";
    try {
      rooms = await get("/api/rooms");
      loadState = "ready";
    } catch (err) {
      error = err.message;
      loadState = "error";
    }
  }

  $effect(() => { load(); });

  async function create({ mediaId, name }) {
    const room = await post("/api/rooms", { mediaId, name: name || undefined });
    dialogOpen = false;
    go(`/room/${room.id}`);
  }

  async function logout() {
    await post("/api/logout");
    onlogout();
  }
</script>

<main class="min-h-dvh mx-auto flex max-w-5xl flex-col gap-6 p-4 sm:p-6">
  <header class="flex items-center justify-between gap-4 border-b border-border pb-4">
    <div>
      <span class="eyebrow">// together</span>
      <h1 class="text-2xl font-semibold tracking-tight text-fg-strong">Live rooms</h1>
    </div>
    <div class="flex items-center gap-1">
      {#if me.role === "admin"}
        <Button variant="ghost" class="h-11" onclick={() => go("/admin")}><Shield /> Admin</Button>
      {/if}
      <Button variant="ghost" size="icon-lg" onclick={logout} aria-label="Sign out"><LogOut /></Button>
    </div>
  </header>

  <section class="flex flex-wrap items-center justify-between gap-3">
    <p>Rooms are live while people are watching together.</p>
    <Dialog.Root bind:open={dialogOpen}>
      <Dialog.Trigger>
        {#snippet child({ props })}
          <Button class="h-11" {...props}><Plus /> Create room</Button>
        {/snippet}
      </Dialog.Trigger>
      <MediaPickerDialog {me} oncreate={create} />
    </Dialog.Root>
  </section>

  {#if loadState === "loading"}
    <div class="flex flex-col gap-2" aria-label="Loading rooms">
      {#each [1, 2, 3] as item (item)}<Skeleton class="h-14 w-full" />{/each}
    </div>
  {:else if loadState === "error"}
    <Alert variant="destructive">
      <AlertTitle>Couldn't load rooms.</AlertTitle>
      <AlertDescription class="flex items-center justify-between gap-3"><span>{error}</span><Button variant="outline" class="h-11" onclick={load}>Retry</Button></AlertDescription>
    </Alert>
  {:else if rooms.length}
    <ul class="flex flex-col gap-2">
      {#each rooms as room (room.id)}
        <li>
          <button class="flex min-h-11 w-full items-center justify-between gap-4 rounded-md border border-border bg-card p-3 text-left transition-colors duration-fast hover:bg-input focus-visible:outline-2 focus-visible:outline-secondary focus-visible:outline-offset-2"
            onclick={() => go(`/room/${room.id}`)}>
            <span class="flex min-w-0 items-center gap-3">
              {#if room.kind === "audio"}<Music2 class="shrink-0 text-secondary" />{:else}<Clapperboard class="shrink-0 text-secondary" />{/if}
              <span class="min-w-0"><span class="block truncate font-medium text-fg-strong">{room.name}</span><span class="block truncate text-sm">{room.mediaTitle} · {room.kind}</span></span>
            </span>
            <span class="eyebrow shrink-0"><DoorOpen /> {room.participants} watching</span>
          </button>
        </li>
      {/each}
    </ul>
  {:else}
    <Card>
      <CardContent class="flex flex-col items-center gap-4 py-10 text-center">
        <p>No rooms right now.</p>
        <Button class="h-11" onclick={() => (dialogOpen = true)}><Plus /> Create room</Button>
      </CardContent>
    </Card>
  {/if}
</main>
