<script>
  import { post } from "../lib/api.js";
  import * as AlertDialog from "./ui/alert-dialog/index.js";

  let { roomId, onregenerated = () => {}, open = $bindable(false) } = $props();
  let error = $state("");
  let regenerating = $state(false);

  async function regenerate() {
    regenerating = true;
    error = "";
    try {
      const { joinToken } = await post(`/api/rooms/${roomId}/token`, {});
      open = false;
      onregenerated(joinToken);
    } catch (err) {
      error = err.message;
    } finally {
      regenerating = false;
    }
  }
</script>

<AlertDialog.Root bind:open>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>Regenerate invite link?</AlertDialog.Title>
      <AlertDialog.Description>The old link will stop working for new guests. People already in the room can stay.</AlertDialog.Description>
    </AlertDialog.Header>
    {#if error}<p class="text-sm text-destructive" role="alert">{error}</p>{/if}
    <AlertDialog.Footer>
      <AlertDialog.Cancel class="h-11" disabled={regenerating}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action class="h-11" onclick={regenerate} disabled={regenerating}>{regenerating ? "Regenerating…" : "Regenerate link"}</AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
