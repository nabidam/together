<script>
  import { Circle } from "lucide-svelte";

  let { users = [] } = $props();
  const label = (status) => ({ downloading: "Downloading", file_ready: "File ready", in_sync: "In sync" })[status] ?? "Downloading";
  const dotClass = (status) => status === "in_sync"
    ? "fill-primary text-primary"
    : status === "file_ready" ? "fill-secondary text-secondary" : "text-fg";
</script>

<section class="p-4 border-b border-border">
  <h2 class="text-fg-strong font-medium mb-3">Participants</h2>
  <ul class="flex flex-col gap-2" aria-live="polite">
    {#each users as user (`${user.name}-${user.isGuest}-${user.isHost}`)}
      <li class="flex items-center gap-2 text-[15px] text-fg-strong">
        <span title={label(user.status)}><Circle size={10} class={dotClass(user.status)} aria-hidden="true" /></span>
        <span>{user.name}</span>
        {#if user.isHost}<span class="eyebrow text-primary">host</span>{/if}
      </li>
    {/each}
  </ul>
</section>
