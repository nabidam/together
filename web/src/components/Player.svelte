<script>
  import { expectedPosition, correction } from "../lib/sync.js";
  import { Play, Pause, RotateCcw, RotateCw, Download, X, Maximize } from "lucide-svelte";

  let { activity, sock, media, onend } = $props();
  // media: {id, title, duration, subtitles:[{id,label}]} — looked up by Room from catalog
  let video = $state(null);
  let curTime = $state(0);
  let showControls = $state(true);
  let hideTimer;

  const st = $derived(activity.state);

  // server echo drives the element — single source of truth
  $effect(() => {
    if (!video) return;
    st.version; // track
    const now = Date.now() + sock.offset();
    const exp = expectedPosition(st, now);
    if (Math.abs(video.currentTime - exp) > 1) video.currentTime = exp;
    if (st.paused && !video.paused) video.pause();
    if (!st.paused && video.paused) video.play().catch(() => {});
  });

  // drift correction loop
  $effect(() => {
    if (!video) return;
    const iv = setInterval(() => {
      if (st.paused || video.paused) return;
      const exp = expectedPosition(st, Date.now() + sock.offset());
      const c = correction(video.currentTime, exp, video.playbackRate);
      if (c?.seek !== undefined) video.currentTime = c.seek;
      else if (c?.rate) video.playbackRate = c.rate;
    }, 500);
    return () => clearInterval(iv);
  });

  function poke() {
    showControls = true;
    clearTimeout(hideTimer);
    hideTimer = setTimeout(() => (showControls = false), 3000);
  }
  $effect(() => { poke(); return () => clearTimeout(hideTimer); });

  const intent = (action, position = 0) => sock.send({ type: "intent", action, position });

  function seekBy(d) { intent("seek", Math.max(0, video.currentTime + d)); }
  function scrub(e) {
    const rect = e.currentTarget.getBoundingClientRect();
    intent("seek", ((e.clientX - rect.left) / rect.width) * (media.duration || 0));
  }
  const tc = (s) => {
    s = Math.max(0, Math.floor(s));
    const h = String(Math.floor(s / 3600)).padStart(2, "0");
    const m = String(Math.floor((s % 3600) / 60)).padStart(2, "0");
    return `${h}:${m}:${String(s % 60).padStart(2, "0")}`;
  };
</script>

<!-- theater mode: fills its container, near-void background, auto-hiding controls -->
<div class="relative w-full h-full bg-void" role="presentation" onpointermove={poke} ontouchstart={poke}>
  <!-- svelte-ignore a11y_media_has_caption -->
  <video bind:this={video} class="w-full h-full object-contain" src={`/media/${media.id}/stream`} playsinline crossorigin="use-credentials" ontimeupdate={() => (curTime = video?.currentTime ?? 0)}>
    {#each media.subtitles as s (s.id)}
      <track kind="subtitles" label={s.label} src={`/media/${media.id}/subs/${s.id}`} />
    {/each}
  </video>

  <div class="absolute inset-x-0 bottom-0 p-4 flex flex-col gap-2 bg-gradient-to-t from-void/90 to-transparent
              transition-opacity duration-[360ms]" style="opacity: {showControls ? 1 : 0}; pointer-events: {showControls ? 'auto' : 'none'}">
    <div class="h-11 flex items-center cursor-pointer rounded-sm focus-visible:outline-2 focus-visible:outline-secondary focus-visible:outline-offset-2"
         onclick={scrub} role="slider" tabindex="0"
         aria-label="Seek" aria-valuemin="0" aria-valuemax={media.duration} aria-valuenow={curTime}
         onkeydown={(e) => { if (e.key === "ArrowRight") seekBy(10); if (e.key === "ArrowLeft") seekBy(-10); }}>
      <div class="h-px w-full bg-border relative">
        <div class="absolute inset-y-0 left-0 bg-primary h-px glow-green"
             style="width: {(curTime / (media.duration || 1)) * 100}%"></div>
      </div>
    </div>
    <div class="flex items-center gap-2">
      <button class="btn-ghost !h-11 !w-11 !px-0" onclick={() => intent(st.paused ? "play" : "pause")} aria-label={st.paused ? "Play" : "Pause"}>
        {#if st.paused}<Play size={18} />{:else}<Pause size={18} />{/if}
      </button>
      <button class="btn-ghost !h-11 !w-11 !px-0" onclick={() => seekBy(-10)} aria-label="Back 10 seconds"><RotateCcw size={16} /></button>
      <button class="btn-ghost !h-11 !w-11 !px-0" onclick={() => seekBy(10)} aria-label="Forward 10 seconds"><RotateCw size={16} /></button>
      <span class="font-mono text-[13px] text-fg-strong ml-2">{tc(curTime)} <span class="text-fg/50">/ {tc(media.duration)}</span></span>
      <span class="eyebrow ml-auto hidden sm:inline">● synced</span>
      <a class="btn-ghost !h-11 !w-11 !px-0" href={`/media/${media.id}/download`} aria-label="Download for offline"><Download size={16} /></a>
      <button class="btn-ghost !h-11 !w-11 !px-0" onclick={() => document.fullscreenElement ? document.exitFullscreen() : video?.parentElement?.requestFullscreen()} aria-label="Fullscreen"><Maximize size={16} /></button>
      <button class="btn-ghost !h-11 !w-11 !px-0" onclick={onend} aria-label="End activity"><X size={16} /></button>
    </div>
  </div>
</div>
<!-- ponytail: "download then watch local" mode deferred — download button ships now; local-file playback source toggle is V2 per spec. -->
