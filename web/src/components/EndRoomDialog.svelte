<script>
  import { del } from "../lib/api.js";
  import { Button } from "./ui/button/index.js";
  import * as AlertDialog from "./ui/alert-dialog/index.js";

  let { roomId, onended = () => {}, open = $bindable(false) } = $props();
  let error = $state("");
  let ending = $state(false);

  async function endRoom() {
    ending = true;
    error = "";
    try {
      await del(`/api/rooms/${roomId}`);
      open = false;
      onended();
    } catch (err) {
      error = err.message;
    } finally {
      ending = false;
    }
  }
</script>

<AlertDialog.Root bind:open>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>End this room?</AlertDialog.Title>
      <AlertDialog.Description>This closes the room for everyone. Participants will see that the room has ended.</AlertDialog.Description>
    </AlertDialog.Header>
    {#if error}<p class="text-sm text-destructive" role="alert">{error}</p>{/if}
    <AlertDialog.Footer>
      <AlertDialog.Cancel class="h-11" disabled={ending}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action variant="destructive" class="h-11" onclick={endRoom} disabled={ending}>{ending ? "Ending…" : "End room"}</AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
