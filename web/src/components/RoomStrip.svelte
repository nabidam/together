<script>
  import { ArrowLeft } from "lucide-svelte";
  import { go } from "../lib/router.svelte.js";
  import { Button } from "./ui/button/index.js";
  import RoomMenu from "./RoomMenu.svelte";

  let { me = null, room, media, roomId, joinToken = "", isHost = false, playbackActive = false, onregenerated = () => {}, onended = () => {} } = $props();

  const leave = () => go("/");
</script>

<header class={`min-h-12 shrink-0 border-b border-border bg-surface px-3 transition-opacity duration-base focus-within:opacity-100 hover:opacity-100 ${playbackActive ? "opacity-0" : "opacity-100"}`}>
  <div class="mx-auto flex min-h-12 max-w-[1320px] items-center gap-2">
    {#if me}<Button variant="ghost" class="h-11" onclick={leave}><ArrowLeft /><span>Leave</span></Button>{/if}
    <div class="min-w-0 flex-1 truncate text-sm text-fg-strong">
      {room?.name ?? "Connecting to room…"}{#if me && media} <span class="text-fg"> · {media.title}</span>{/if}
    </div>
    {#if isHost}
      <span class="eyebrow rounded-md border border-primary px-2 py-1 text-primary">Host</span>
      <RoomMenu {roomId} {joinToken} {onregenerated} {onended} />
    {/if}
  </div>
</header>
