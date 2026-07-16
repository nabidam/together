<script>
  import { expectedPosition, correction } from "../lib/sync.js";
  import { Play } from "lucide-svelte";
  import { Button } from "./ui/button/index.js";

  let { activity, sock, media, source } = $props();
  let video = $state(null);
  let armed = $state(false);
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

</script>

<div id="room-player" class="relative w-full h-full bg-void" role="presentation">
  <!-- svelte-ignore a11y_media_has_caption -->
  <video bind:this={video} class="w-full h-full object-contain" src={source} playsinline>
    {#each media.subtitles as subtitle (subtitle.id)}
      <track kind="subtitles" label={subtitle.label} src={`/media/${media.id}/subs/${subtitle.id}`} />
    {/each}
  </video>

  {#if !armed}
    <div class="absolute inset-0 grid place-items-center bg-void/80">
      <Button class="h-11" onclick={arm}><Play size={18} />Click to enable playback</Button>
    </div>
  {/if}
</div>
