<script>
  import { untrack } from "svelte";
  import { get } from "../lib/api.js";
  import { connect } from "../lib/ws.js";
  import { revokeObjectURL } from "../lib/localfile.js";
  import AcquisitionPanel from "../components/AcquisitionPanel.svelte";
  import Chat from "../components/Chat.svelte";
  import Participants from "../components/Participants.svelte";
  import Player from "../components/Player.svelte";
  import RoomClosed from "../components/RoomClosed.svelte";
  import RoomStrip from "../components/RoomStrip.svelte";
  import { LoaderCircle } from "lucide-svelte";

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
  let playbackSource = $state("");
  let isHost = $state(false);
  let joinToken = $state("");

  $effect(() => {
    let active = true;
    messages = [];
    users = [];
    activity = null;
    room = null;
    media = null;
    revokeObjectURL(untrack(() => playbackSource));
    playbackSource = "";
    isHost = false;
    joinToken = "";
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
        isHost = message.you.isHost;
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
      revokeObjectURL(untrack(() => playbackSource));
    };
  });

  $effect(() => {
    if (!isHost || !roomId) return;
    let active = true;
    get(`/api/rooms/${roomId}/token`)
      .then(({ joinToken: token }) => {
        if (active) joinToken = token;
      })
      .catch(() => {
        if (active) joinToken = "";
      });
    return () => { active = false; };
  });

  function setPlaybackSource(source) {
    revokeObjectURL(playbackSource);
    playbackSource = source;
  }

  const ready = $derived(room !== null && media !== null);
  const disconnected = $derived(connection !== "connected");
  const playbackActive = $derived(Boolean(playbackSource && activity));

  // Re-announce the local edge after each socket connection. The Player will
  // advance file_ready to in_sync once its drift loop is running again.
  $effect(() => {
    if (sock && connection === "connected") {
      sock.send({ type: "status", state: playbackSource ? "file_ready" : "downloading" });
    }
  });
</script>

{#if closed}
  <RoomClosed accountUser={Boolean(me)} />
{:else}
  <main class="h-dvh flex flex-col">
    <RoomStrip {me} {room} {media} {roomId} {joinToken} {isHost} {playbackActive}
      onregenerated={(token) => (joinToken = token)} onended={() => (closed = true)} />
    {#if connection === "reconnecting"}
      <div class="border-b border-info bg-info/10 px-4 py-2 text-sm text-fg" role="status">Reconnecting…</div>
    {/if}

    {#if loadError}
      <div class="flex-1 grid place-items-center p-6"><p class="text-error" role="alert">{loadError}</p></div>
    {:else if !ready}
      <div class="flex-1 grid place-items-center gap-2 text-fg"><LoaderCircle class="animate-spin text-secondary" size={24} /><span>Joining room…</span></div>
    {:else}
      <div class="flex-1 min-h-0 flex flex-col md:flex-row">
        <section class="flex-1 min-h-0 relative">
          {#if playbackSource && activity}
            <Player {activity} {sock} {media} source={playbackSource} />
          {:else if activity}
            <AcquisitionPanel {media} kind={room.kind} onsource={setPlaybackSource} />
          {:else}
            <div class="h-full grid place-items-center p-6"><p class="text-fg">Waiting for playback to begin.</p></div>
          {/if}
        </section>
        <aside class="md:w-80 md:border-l border-t md:border-t-0 border-border h-[45%] md:h-auto shrink-0 flex flex-col">
          <Participants {users} />
          <div class="flex-1 min-h-0"><Chat {messages} disabled={disconnected} send={(body) => sock.send({ type: "chat", body })} /></div>
        </aside>
      </div>
    {/if}
  </main>
{/if}
