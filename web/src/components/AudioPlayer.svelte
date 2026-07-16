<script>
  import { expectedPosition, correction } from "../lib/sync.js";
  import { Music2, Play } from "lucide-svelte";
  import { Button } from "./ui/button/index.js";

  let { activity, sock, media, source } = $props();
  let audio = $state(null);
  let armed = $state(false);
  let lastStatus = "";

  const state = $derived(activity.state);

  const timecode = (seconds) => {
    const value = Math.max(0, Math.floor(seconds ?? 0));
    const hours = Math.floor(value / 3600);
    const minutes = String(Math.floor((value % 3600) / 60)).padStart(2, "0");
    const rest = String(value % 60).padStart(2, "0");
    return hours > 0 ? `${hours}:${minutes}:${rest}` : `${Number(minutes)}:${rest}`;
  };

  function reportStatus(status) {
    if (status !== lastStatus && sock.send({ type: "status", state: status })) {
      lastStatus = status;
    }
  }

  function project() {
    if (!audio || !armed) return;
    const expected = expectedPosition(state, Date.now() + sock.offset());
    if (Math.abs(audio.currentTime - expected) > 1) audio.currentTime = expected;
    if (state.paused && !audio.paused) audio.pause();
    if (!state.paused && audio.paused) audio.play().catch(() => {});
  }

  // Keep the audio element on the same echo-driven contract as Player: local
  // controls only send intents in Room; broadcasts own every media mutation.
  $effect(() => {
    state.version;
    project();
  });

  $effect(() => {
    if (!audio || !armed) return;
    const interval = setInterval(() => {
      if (state.paused || audio.paused) return;
      const expected = expectedPosition(state, Date.now() + sock.offset());
      const adjustment = correction(audio.currentTime, expected, audio.playbackRate);
      if (adjustment?.seek !== undefined) audio.currentTime = adjustment.seek;
      else if (adjustment?.rate) audio.playbackRate = adjustment.rate;
      if (Math.abs(audio.currentTime - expected) <= 1) reportStatus("in_sync");
    }, 500);
    return () => clearInterval(interval);
  });

  $effect(() => {
    source;
    armed = false;
    lastStatus = "";
    reportStatus("file_ready");
  });

  function arm() {
    armed = true;
    project();
    if (state.paused) reportStatus("in_sync");
  }
</script>

<div id="room-audio-player" class="relative grid h-full w-full place-items-center overflow-hidden bg-void p-6">
  <div class="flex max-w-xl flex-col items-center gap-4 text-center">
    <div class="grid size-20 place-items-center rounded-lg border border-border bg-card text-secondary" aria-hidden="true">
      <Music2 class="size-8" />
    </div>
    <div>
      <p class="eyebrow">Now playing</p>
      <h2 class="mt-2 text-2xl font-semibold tracking-tight text-fg-strong">{media.title}</h2>
      <p class="mt-2 font-mono text-sm text-fg">{timecode(media.duration)}</p>
    </div>
  </div>

  <audio bind:this={audio} src={source} preload="metadata"></audio>

  {#if !armed}
    <div class="absolute inset-0 grid place-items-center bg-void/80">
      <Button class="h-11" onclick={arm}><Play class="size-4" />Click to enable playback</Button>
    </div>
  {/if}
</div>
