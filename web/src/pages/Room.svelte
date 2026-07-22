<script>
  import { untrack } from "svelte";
  import { get } from "../lib/api.js";
  import { connect } from "../lib/ws.js";
  import { revokeObjectURL } from "../lib/localfile.js";
  import { expectedPosition } from "../lib/sync.js";
  import AcquisitionPanel from "../components/AcquisitionPanel.svelte";
  import AudioPlayer from "../components/AudioPlayer.svelte";
  import Player from "../components/Player.svelte";
  import RoomClosed from "../components/RoomClosed.svelte";
  import RoomStrip from "../components/RoomStrip.svelte";
  import SidePanel from "../components/SidePanel.svelte";
  import { Button } from "../components/ui/button/index.js";
  import { Slider } from "../components/ui/slider/index.js";
  import * as Dialog from "../components/ui/dialog/index.js";
  import { go } from "../lib/router.svelte.js";
  import { Captions, LoaderCircle, Maximize, PanelRightClose, PanelRightOpen, Pause, Play } from "lucide-svelte";

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
  let panelOpen = $state(false);
  let scrubPosition = $state(0);
  let toast = $state("");
  let toastTimer;
  let myName = $state("");
  let playConfirmOpen = $state(false);

  function showToast(message) {
    toast = message;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => (toast = ""), 3000);
  }

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
    toast = "";
    myName = "";
    clearTimeout(toastTimer);

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
        myName = message.you.name;
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
        if (message.by && message.action) {
          const actor = message.by === myName ? "You" : message.by;
          const action = message.action === "seek" ? "changed position" : message.action === "pause" ? "paused playback" : "started playback";
          showToast(`${actor} ${action}.`);
        }
      } else if (message.type === "user_left") {
        showToast("User Left.");
      } else if (message.type === "user_rejoined") {
        showToast("Partner rejoined.");
      } else if (message.type === "left") {
        go("/");
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
      clearTimeout(toastTimer);
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
  const isAudio = $derived(room?.kind === "audio");

  $effect(() => {
    if (!activity?.state) {
      scrubPosition = 0;
      return;
    }
    const update = () => {
      scrubPosition = Math.min(media?.duration ?? 0, expectedPosition(activity.state, Date.now() + sock.offset()));
    };
    update();
    const timer = setInterval(update, 250);
    return () => clearInterval(timer);
  });

  // Re-announce the local edge after each socket connection. The Player will
  // advance file_ready to in_sync once its drift loop is running again.
  $effect(() => {
    if (sock && connection === "connected") {
      sock.send({ type: "status", state: playbackSource ? "file_ready" : "downloading" });
    }
  });

  const intent = (action, position = 0) => sock?.send({ type: "intent", action, position });
  const leave = () => {
    if (!sock?.send({ type: "leave" })) go("/");
  };
  const requestPlay = () => {
    if (isHost && users.some((user) => user.status === "downloading")) {
      playConfirmOpen = true;
      return;
    }
    intent("play");
  };
  const togglePlayback = () => activity?.state.paused ? requestPlay() : intent("pause");
  const timecode = (seconds) => {
    const value = Math.max(0, Math.floor(seconds ?? 0));
    const hours = String(Math.floor(value / 3600)).padStart(2, "0");
    const minutes = String(Math.floor((value % 3600) / 60)).padStart(2, "0");
    return `${hours}:${minutes}:${String(value % 60).padStart(2, "0")}`;
  };

  function toggleCaptions() {
    const tracks = document.querySelectorAll("#room-player track");
    const active = [...tracks].some((track) => track.track.mode === "showing");
    tracks.forEach((track) => (track.track.mode = active ? "disabled" : "showing"));
  }

  function toggleFullscreen() {
    const player = document.getElementById("room-player");
    if (document.fullscreenElement) document.exitFullscreen();
    else player?.requestFullscreen();
  }
</script>

{#if closed}
  <RoomClosed accountUser={Boolean(me)} />
{:else}
  {#if toast}
    <div class="fixed right-4 bottom-4 z-50 rounded-md border border-border bg-card px-4 py-3 text-sm text-fg-strong shadow-lg" role="status">{toast}</div>
  {/if}
  <main class="h-dvh flex flex-col">
    <RoomStrip {me} {room} {media} {roomId} {joinToken} {isHost} {playbackActive} resumeVisible={isHost && activity?.state.paused} onleave={leave} onresume={requestPlay}
      onregenerated={(token) => (joinToken = token)} onended={() => (closed = true)} />
    {#if connection === "reconnecting"}
      <div class="border-b border-info bg-info/10 px-4 py-2 text-sm text-fg" role="status">Reconnecting…</div>
    {/if}

    {#if loadError}
      <div class="flex-1 grid place-items-center p-6"><p class="text-error" role="alert">{loadError}</p></div>
    {:else if !ready}
      <div class="flex-1 grid place-items-center gap-2 text-fg"><LoaderCircle class="animate-spin text-secondary" size={24} /><span>Joining room…</span></div>
    {:else}
      <div class="flex-1 min-h-0 flex flex-col md:flex-row relative">
        <section class="flex-1 min-h-0 flex flex-col bg-void">
          <div class="flex-1 min-h-0 relative">
            {#if playbackSource && activity}
              {#if isAudio}
                <AudioPlayer {activity} {sock} {media} source={playbackSource} />
              {:else}
                <Player {activity} {sock} {media} source={playbackSource} />
              {/if}
            {:else if activity}
              <AcquisitionPanel {media} kind={room.kind} onsource={setPlaybackSource} />
            {:else}
              <div class="h-full grid place-items-center p-6"><p class="text-fg">Waiting for playback to begin.</p></div>
            {/if}
          </div>
          <div class="min-h-16 shrink-0 border-t border-border bg-void px-3 py-2 flex items-center gap-2">
            <Button variant="ghost" size="icon-lg" disabled={disconnected || !activity} onclick={togglePlayback} aria-label={activity?.state.paused ? "Play" : "Pause"}>
              {#if activity?.state.paused}<Play />{:else}<Pause />{/if}
            </Button>
            <Slider value={[scrubPosition]} min={0} max={media.duration ?? 0} step={0.1} disabled={disconnected || !activity} onValueCommit={(value) => intent("seek", value[0])} aria-label="Seek playback" />
            <span class="hidden sm:inline whitespace-nowrap font-mono text-sm text-fg-strong">{timecode(scrubPosition)} <span class="text-fg/50">/ {timecode(media.duration)}</span></span>
            {#if !isAudio}
              <Button variant="ghost" size="icon-lg" disabled={disconnected || !activity} onclick={toggleCaptions} aria-label="Toggle captions"><Captions /></Button>
              <Button variant="ghost" size="icon-lg" onclick={toggleFullscreen} aria-label="Fullscreen"><Maximize /></Button>
            {/if}
            <Button variant="ghost" size="icon-lg" onclick={() => (panelOpen = !panelOpen)} aria-label={panelOpen ? "Hide side panel" : "Show side panel"} aria-expanded={panelOpen}>
              {#if panelOpen}<PanelRightClose />{:else}<PanelRightOpen />{/if}
            </Button>
          </div>
        </section>
        <SidePanel bind:open={panelOpen} defaultOpen={room.kind === "audio"} {users} {messages} disabled={disconnected} send={(body) => sock.send({ type: "chat", body })} />
      </div>
    {/if}
  </main>
  <Dialog.Root bind:open={playConfirmOpen}>
    <Dialog.Content>
      <Dialog.Header><Dialog.Title>Partner still downloading</Dialog.Title><Dialog.Description>Start playback anyway? They will sync when their file is ready.</Dialog.Description></Dialog.Header>
      <Dialog.Footer>
        <Dialog.Close>{#snippet child({ props })}<Button variant="outline" class="h-11" {...props}>Wait</Button>{/snippet}</Dialog.Close>
        <Button class="h-11" onclick={() => { playConfirmOpen = false; intent("play"); }}>Play anyway</Button>
      </Dialog.Footer>
    </Dialog.Content>
  </Dialog.Root>
{/if}
