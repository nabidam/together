<script>
  import { get, post } from "../lib/api.js";
  import { go } from "../lib/router.svelte.js";
  import { Clapperboard, DoorOpen, LogOut, Music2, Plus, Shield, X } from "lucide-svelte";

  let { me, onlogout } = $props();
  let rooms = $state([]);
  let media = $state([]);
  let loadState = $state("loading"); // loading | ready | error
  let dialogOpen = $state(false);
  let mediaId = $state(null);
  let name = $state("");
  let error = $state("");
  let creating = $state(false);

  async function load() {
    loadState = "loading";
    try {
      rooms = await get("/api/rooms");
      loadState = "ready";
    } catch (err) {
      error = err.message;
      loadState = "error";
    }
  }

  $effect(() => { load(); });

  async function openDialog() {
    dialogOpen = true;
    error = "";
    mediaId = null;
    name = "";
    try {
      media = (await get("/api/media")).filter((item) => item.status === "ready");
    } catch (err) {
      error = err.message;
    }
  }

  async function create(event) {
    event.preventDefault();
    if (!mediaId) return;
    creating = true;
    error = "";
    try {
      const room = await post("/api/rooms", { mediaId, name: name.trim() || undefined });
      go(`/room/${room.id}`);
    } catch (err) {
      error = err.message;
    } finally {
      creating = false;
    }
  }

  async function logout() {
    await post("/api/logout");
    onlogout();
  }

  const selectedMedia = $derived(media.find((item) => item.id === mediaId));
</script>

<main class="min-h-dvh max-w-3xl mx-auto p-6 flex flex-col gap-6">
  <header class="flex items-center justify-between gap-4">
    <div>
      <span class="eyebrow">// together</span>
      <h1 class="text-fg-strong text-2xl font-semibold tracking-tight">Live rooms</h1>
    </div>
    <div class="flex gap-2">
      {#if me.role === "admin"}
        <button class="btn-ghost" onclick={() => go("/admin")}><Shield size={16} /> Admin</button>
      {/if}
      <button class="btn-ghost" onclick={logout} aria-label="Sign out"><LogOut size={16} /></button>
    </div>
  </header>

  <div class="flex items-center justify-between gap-3">
    <p class="text-fg">Rooms are live while people are watching together.</p>
    <button class="btn-primary shrink-0" onclick={openDialog}><Plus size={16} /> Create room</button>
  </div>

  {#if loadState === "loading"}
    <div class="flex flex-col gap-2" aria-label="Loading rooms">
      {#each [1, 2, 3] as item (item)}<div class="card h-12 animate-pulse"></div>{/each}
    </div>
  {:else if loadState === "error"}
    <div class="card p-4 flex items-center justify-between gap-4" role="alert">
      <span class="text-error">Couldn't load rooms.</span>
      <button class="btn-ghost" onclick={load}>Retry</button>
    </div>
  {:else if rooms.length}
    <ul class="flex flex-col gap-2">
      {#each rooms as room (room.id)}
        <li>
          <button class="card w-full min-h-11 p-3 flex items-center justify-between gap-4 text-left cursor-pointer transition-colors duration-200 hover:border-fg/30 focus-visible:outline-2 focus-visible:outline-secondary"
            onclick={() => go(`/room/${room.id}`)}>
            <span class="min-w-0 flex items-center gap-3">
              {#if room.kind === "audio"}<Music2 size={18} class="shrink-0 text-secondary" />{:else}<Clapperboard size={18} class="shrink-0 text-secondary" />{/if}
              <span class="min-w-0"><span class="block text-fg-strong font-medium truncate">{room.name}</span><span class="text-[13px] text-fg truncate block">{room.mediaTitle} · {room.kind}</span></span>
            </span>
            <span class="eyebrow shrink-0"><DoorOpen size={14} /> {room.participants} watching</span>
          </button>
        </li>
      {/each}
    </ul>
  {:else}
    <section class="card p-8 text-center flex flex-col items-center gap-4">
      <p class="text-fg">No rooms right now.</p>
      <button class="btn-primary" onclick={openDialog}><Plus size={16} /> Create room</button>
    </section>
  {/if}

  {#if dialogOpen}
    <div class="fixed inset-0 bg-void/80 grid place-items-center p-4" role="presentation">
      <div class="card w-full max-w-xl p-6 flex flex-col gap-4" role="dialog" aria-modal="true" aria-labelledby="create-room-title">
        <div class="flex items-center justify-between gap-4"><h2 id="create-room-title" class="text-fg-strong text-xl font-semibold">Create room</h2><button class="btn-ghost !h-11 !w-11 !px-0" onclick={() => (dialogOpen = false)} aria-label="Close"><X size={16} /></button></div>
        <form onsubmit={create} class="flex flex-col gap-4">
          <label class="flex flex-col gap-1"><span class="text-[13px] font-medium">Room name <span class="text-fg">(optional)</span></span><input class="input" bind:value={name} maxlength="64" disabled={creating} placeholder={selectedMedia?.title || "Defaults to media title"} /></label>
          <fieldset class="flex flex-col gap-2" disabled={creating}><legend class="text-[13px] font-medium mb-1">Pick media</legend>
            {#each media as item (item.id)}
              <label class="card min-h-11 p-3 flex items-center gap-3 cursor-pointer hover:border-fg/30"><input type="radio" name="media" value={item.id} bind:group={mediaId} /><span class="min-w-0 flex-1"><span class="text-fg-strong block truncate">{item.title}</span><span class="text-[13px] text-fg">{item.kind} · {item.sizeBytes.toLocaleString()} bytes</span></span></label>
            {:else}
              <p class="text-fg">Library is empty. {me.role === "admin" ? "Upload media in Admin first." : "Ask your admin to upload something."}</p>
            {/each}
          </fieldset>
          {#if error}<p class="text-error text-[13px]" role="alert">{error}</p>{/if}
          <div class="flex justify-end gap-2"><button type="button" class="btn-ghost" onclick={() => (dialogOpen = false)} disabled={creating}>Cancel</button><button class="btn-primary" disabled={!mediaId || creating}>{creating ? "Creating…" : "Create"}</button></div>
        </form>
      </div>
    </div>
  {/if}
</main>
