<script>
  import { ArrowLeft, Play } from "lucide-svelte";
  import { Button } from "./ui/button/index.js";
  import * as AlertDialog from "./ui/alert-dialog/index.js";
  import RoomMenu from "./RoomMenu.svelte";

  let { me = null, room, media, roomId, joinToken = "", isHost = false, playbackActive = false, resumeVisible = false, onleave = () => {}, onresume = () => {}, onregenerated = () => {}, onended = () => {} } = $props();
  let leaveOpen = $state(false);

  function leave() {
    leaveOpen = false;
    onleave();
  }
</script>

<header class={`min-h-12 shrink-0 border-b border-border bg-surface px-3 transition-opacity duration-base focus-within:opacity-100 hover:opacity-100 ${playbackActive ? "opacity-0" : "opacity-100"}`}>
  <div class="mx-auto flex min-h-12 max-w-[1320px] items-center gap-2">
    <Button variant="ghost" class="h-11" onclick={() => (leaveOpen = true)}><ArrowLeft /><span>Leave</span></Button>
    <div class="min-w-0 flex-1 truncate text-sm text-fg-strong">
      {room?.name ?? "Connecting to room…"}{#if me && media} <span class="text-fg"> · {media.title}</span>{/if}
    </div>
    {#if isHost}
      {#if resumeVisible}<Button variant="outline" class="h-11" onclick={onresume}><Play />Resume</Button>{/if}
      <span class="eyebrow rounded-md border border-primary px-2 py-1 text-primary">Host</span>
      <RoomMenu {roomId} {joinToken} {onregenerated} {onended} />
    {/if}
  </div>
</header>

<AlertDialog.Root bind:open={leaveOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>Leave this room?</AlertDialog.Title>
      <AlertDialog.Description>If this is your final tab, playback pauses for everyone still in the room.</AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel class="h-11">Stay</AlertDialog.Cancel>
      <AlertDialog.Action class="h-11" onclick={leave}>Leave room</AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
