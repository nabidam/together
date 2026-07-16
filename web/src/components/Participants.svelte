<script>
  import { Circle } from "lucide-svelte";
  import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip/index.js";

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
      <li class="flex items-center gap-2 text-fg-strong">
        <Tooltip>
          <TooltipTrigger class="inline-flex size-11 items-center justify-center -m-4 focus-visible:outline-2 focus-visible:outline-secondary focus-visible:outline-offset-2" aria-label={label(user.status)}>
            <Circle size={10} class={`${dotClass(user.status)} ${user.status === "in_sync" ? "glow-green rounded-full" : ""}`} aria-hidden="true" />
          </TooltipTrigger>
          <TooltipContent>{label(user.status)}</TooltipContent>
        </Tooltip>
        <span>{user.name}</span>
        {#if user.isHost}<span class="eyebrow text-primary">host</span>{/if}
      </li>
    {/each}
  </ul>
</section>
