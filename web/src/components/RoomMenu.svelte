<script>
  import { Check, Copy, MoreHorizontal, RefreshCw, X } from "lucide-svelte";
  import { Button } from "./ui/button/index.js";
  import * as DropdownMenu from "./ui/dropdown-menu/index.js";
  import EndRoomDialog from "./EndRoomDialog.svelte";
  import RegenerateLinkDialog from "./RegenerateLinkDialog.svelte";

  let { roomId, joinToken = "", onregenerated = () => {}, onended = () => {} } = $props();
  let copied = $state(false);
  let regenerateOpen = $state(false);
  let endOpen = $state(false);

  async function copyLink() {
    const link = `${window.location.origin}${window.location.pathname}#/join/${joinToken}`;
    try {
      await navigator.clipboard.writeText(link);
    } catch {
      const input = document.createElement("textarea");
      input.value = link;
      input.style.position = "fixed";
      document.body.append(input);
      input.select();
      document.execCommand("copy");
      input.remove();
    }
    copied = true;
    window.setTimeout(() => (copied = false), 2000);
  }
</script>

<DropdownMenu.Root>
  <DropdownMenu.Trigger>
    {#snippet child({ props })}
      <Button variant="ghost" class="h-11" {...props}><MoreHorizontal /><span>Room</span></Button>
    {/snippet}
  </DropdownMenu.Trigger>
  <DropdownMenu.Content align="end">
    <DropdownMenu.Label>Room controls</DropdownMenu.Label>
    <DropdownMenu.Item onclick={copyLink} disabled={!joinToken}><span>{copied ? "Link copied" : "Copy invite link"}</span>{#if copied}<Check />{:else}<Copy />{/if}</DropdownMenu.Item>
    <DropdownMenu.Item onclick={() => (regenerateOpen = true)}><RefreshCw />Regenerate link</DropdownMenu.Item>
    <DropdownMenu.Separator />
    <DropdownMenu.Item variant="destructive" onclick={() => (endOpen = true)}><X />End room</DropdownMenu.Item>
  </DropdownMenu.Content>
</DropdownMenu.Root>

<RegenerateLinkDialog {roomId} {onregenerated} bind:open={regenerateOpen} />
<EndRoomDialog {roomId} {onended} bind:open={endOpen} />
