<script>
  import { get } from "../lib/api.js";
  import { go } from "../lib/router.svelte.js";
  import { connect } from "../lib/ws.js";
  import Chat from "../components/Chat.svelte";
  import Player from "../components/Player.svelte";
  import RoomClosed from "../components/RoomClosed.svelte";
  import { ArrowLeft, Circle, LoaderCircle } from "lucide-svelte";

  let { me = null, roomId } = $props();
  let messages = $state([]);
  let users = $state([]);
  let activity = $state(null);
  let room = $state(null);
  let media = $state(null);
  let connection = $state("connecting");
  let closed = $state(false);
  let loadError = $state("");
  let sock = $state(null);

  $effect(() => {
    let active = true;
    messages = [];
    users = [];
    activity = null;
    room = null;
    media = null;
    closed = false;
    loadError = "";
    connection = "connecting";

    get(`/api/rooms/${roomId}/meta`)
      .then((meta) => {
        if (!active) return;
        media = { ...meta.media, subtitles: meta.subtitles ?? [] };
      })
      .catch(() => {
        if (active) loadError = "Couldn't load this room.";
      });

    sock = connect(roomId, (message) => {
      if (!active) return;
      if (message.type === "hello") {
        room = message.room;
        users = message.users;
        activity = message.activity;
        messages = message.chat;
      } else if (message.type === "presence") {
        users = message.users;
      } else if (message.type === "chat") {
        messages = [...messages, message];
      } else if (message.type === "activity") {
        activity = message.activity;
      } else if (message.type === "room_closed") {
        closed = true;
        sock.close();
      }
    }, (state) => {
      if (active) connection = state;
    });

    return () => {
      active = false;
      sock?.close();
    };
  });

  const ready = $derived(room !== null && media !== null);
  const disconnected = $derived(connection !== "connected");
  const statusLabel = (status) => ({ downloading: "Downloading", file_ready: "File ready", in_sync: "In sync" })[status] ?? "Downloading";
</script>

{#if closed}
  <RoomClosed accountUser={Boolean(me)} />
{:else}
  <main class="h-dvh flex flex-col">
    <header class="min-h-14 border-b border-border flex items-center gap-3 px-4 shrink-0">
      {#if me}
        <button class="btn-ghost !h-11 !px-2" onclick={() => go("/")} aria-label="Back to rooms"><ArrowLeft size={16} /></button>
      {/if}
      <div class="min-w-0">
        <span class="eyebrow">// room</span>
        <h1 class="text-fg-strong font-medium truncate">{room?.name ?? "Connecting to room…"}{media ? ` · ${media.title}` : ""}</h1>
      </div>
      {#if connection === "reconnecting"}
        <div class="ml-auto text-warning text-[13px]" role="status">Reconnecting…</div>
      {/if}
    </header>

    {#if loadError}
      <div class="flex-1 grid place-items-center p-6"><p class="text-error" role="alert">{loadError}</p></div>
    {:else if !ready}
      <div class="flex-1 grid place-items-center gap-2 text-fg"><LoaderCircle class="animate-spin text-secondary" size={24} /><span>Joining room…</span></div>
    {:else}
      <div class="flex-1 min-h-0 flex flex-col md:flex-row">
        <section class="flex-1 min-h-0 relative">
          {#if activity}
            <Player {activity} {sock} {media} onend={() => sock.send({ type: "end" })} />
          {:else}
            <div class="h-full grid place-items-center p-6"><p class="text-fg">Waiting for playback to begin.</p></div>
          {/if}
        </section>
        <aside class="md:w-80 md:border-l border-t md:border-t-0 border-border h-[45%] md:h-auto shrink-0 flex flex-col">
          <section class="p-4 border-b border-border">
            <h2 class="text-fg-strong font-medium mb-3">Participants</h2>
            <ul class="flex flex-col gap-2" aria-live="polite">
              {#each users as user (`${user.name}-${user.isGuest}-${user.isHost}`)}
                <li class="flex items-center gap-2 text-[15px] text-fg-strong">
                  <Circle size={10} class={user.status === "in_sync" ? "fill-primary text-primary" : "text-secondary"} aria-hidden="true" />
                  <span>{user.name}</span>
                  {#if user.isHost}<span class="eyebrow text-primary">host</span>{/if}
                  <span class="text-fg text-[13px] ml-auto">{statusLabel(user.status)}</span>
                </li>
              {/each}
            </ul>
          </section>
          <div class="flex-1 min-h-0"><Chat {messages} disabled={disconnected} send={(body) => sock.send({ type: "chat", body })} /></div>
        </aside>
      </div>
    {/if}
  </main>
{/if}
