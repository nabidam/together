<script>
  import { Button } from "./ui/button/index.js";
  import * as Dialog from "./ui/dialog/index.js";

  let { onstream = () => {} } = $props();
  let open = $state(false);

  function stream() {
    open = false;
    onstream();
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Trigger>
    {#snippet child({ props })}
      <Button variant="link" class="h-11 text-fg" {...props}>Play from server instead</Button>
    {/snippet}
  </Dialog.Trigger>
  <Dialog.Content>
    <Dialog.Header>
      <Dialog.Title>Stream from the server?</Dialog.Title>
      <Dialog.Description>Playback quality depends on the server's small connection. Local files are always smoother.</Dialog.Description>
    </Dialog.Header>
    <Dialog.Footer>
      <Dialog.Close>
        {#snippet child({ props })}<Button variant="outline" class="h-11" {...props}>Cancel</Button>{/snippet}
      </Dialog.Close>
      <Button class="h-11" onclick={stream}>Stream anyway</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
