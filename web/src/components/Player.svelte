<script>
  import { expectedPosition, correction } from "../lib/sync.js";
  import { Play, Pause, RotateCcw, RotateCw, Maximize } from "lucide-svelte";

  let { activity, sock, media, source } = $props();
  let video = $state(null);
  let armed = $state(false);
  let curTime = $state(0);
  let showControls = $state(true);
  let hideTimer;
  let lastStatus = "";

  const state = $derived(activity.state);

  function reportStatus(status) {
    if (status !== lastStatus && sock.send({ type: "status", state: status })) {
      lastStatus = status;
    }
  }

  function project() {
    if (!video || !armed) return;
    const expected = expectedPosition(state, Date.now() + sock.offset());
    if (Math.abs(video.currentTime - expected) > 1) video.currentTime = expected;
    if (state.paused && !video.paused) video.pause();
    if (!state.paused && video.paused) video.play().catch(() => {});
  }

  // The broadcast state is the sole source of media mutations. A local click
  // only sends an intent; it cannot optimistically change the element.
  $effect(() => {
    state.version;
    project();
  });

  $effect(() => {
    if (!video || !armed) return;
    const interval = setInterval(() => {
      if (state.paused || video.paused) return;
      const expected = expectedPosition(state, Date.now() + sock.offset());
      const adjustment = correction(video.currentTime, expected, video.playbackRate);
      if (adjustment?.seek !== undefined) video.currentTime = adjustment.seek;
      else if (adjustment?.rate) video.playbackRate = adjustment.rate;
      if (Math.abs(video.currentTime - expected) <= 1) reportStatus("in_sync");
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

  function poke() {
    showControls = true;
    clearTimeout(hideTimer);
    hideTimer = setTimeout(() => (showControls = false), 3000);
  }

  $effect(() => {
    poke();
    return () => clearTimeout(hideTimer);
  });

  const intent = (action, position = 0) => sock.send({ type: "intent", action, position });
  const seekBy = (delta) => intent("seek", Math.max(0, (video?.currentTime ?? 0) + delta));
  const timecode = (seconds) => {
    const value = Math.max(0, Math.floor(seconds));
    const hours = String(Math.floor(value / 3600)).padStart(2, "0");
    const minutes = String(Math.floor((value % 3600) / 60)).padStart(2, "0");
    return `${hours}:${minutes}:${String(value % 60).padStart(2, "0")}`;
  };

  function scrub(event) {
    const rect = event.currentTarget.getBoundingClientRect();
    intent("seek", ((event.clientX - rect.left) / rect.width) * (media.duration || 0));
  }
</script>

<div class="relative w-full h-full bg-void" role="presentation" onpointermove={poke} ontouchstart={poke}>
  <!-- svelte-ignore a11y_media_has_caption -->
  <video bind:this={video} class="w-full h-full object-contain" src={source} playsinline ontimeupdate={() => (curTime = video?.currentTime ?? 0)}>
    {#each media.subtitles as subtitle (subtitle.id)}
      <track kind="subtitles" label={subtitle.label} src={`/media/${media.id}/subs/${subtitle.id}`} />
    {/each}
  </video>

  {#if !armed}
    <div class="absolute inset-0 grid place-items-center bg-void/80">
      <button class="btn-primary" onclick={arm}><Play size={18} />Click to enable playback</button>
    </div>
  {/if}

  <div class="absolute inset-x-0 bottom-0 p-4 flex flex-col gap-2 bg-gradient-to-t from-void/90 to-transparent transition-opacity duration-[360ms]" style="opacity: {showControls ? 1 : 0}; pointer-events: {showControls ? 'auto' : 'none'}">
    <div class="h-11 flex items-center cursor-pointer rounded-sm focus-visible:outline-2 focus-visible:outline-secondary focus-visible:outline-offset-2" onclick={scrub} role="slider" tabindex="0" aria-label="Seek" aria-valuemin="0" aria-valuemax={media.duration} aria-valuenow={curTime} onkeydown={(event) => { if (event.key === "ArrowRight") seekBy(10); if (event.key === "ArrowLeft") seekBy(-10); }}>
      <div class="h-px w-full bg-border relative"><div class="absolute inset-y-0 left-0 bg-primary h-px glow-green" style="width: {(curTime / (media.duration || 1)) * 100}%"></div></div>
    </div>
    <div class="flex items-center gap-2">
      <button class="btn-ghost !h-11 !w-11 !px-0" onclick={() => intent(state.paused ? "play" : "pause")} aria-label={state.paused ? "Play" : "Pause"}>{#if state.paused}<Play size={18} />{:else}<Pause size={18} />{/if}</button>
      <button class="btn-ghost !h-11 !w-11 !px-0" onclick={() => seekBy(-10)} aria-label="Back 10 seconds"><RotateCcw size={16} /></button>
      <button class="btn-ghost !h-11 !w-11 !px-0" onclick={() => seekBy(10)} aria-label="Forward 10 seconds"><RotateCw size={16} /></button>
      <span class="font-mono text-[13px] text-fg-strong ml-2">{timecode(curTime)} <span class="text-fg/50">/ {timecode(media.duration)}</span></span>
      <button class="btn-ghost !h-11 !w-11 !px-0 ml-auto" onclick={() => document.fullscreenElement ? document.exitFullscreen() : video?.parentElement?.requestFullscreen()} aria-label="Fullscreen"><Maximize size={16} /></button>
    </div>
  </div>
</div>
