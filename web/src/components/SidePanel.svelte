<script>
  import Participants from "./Participants.svelte";
  import Chat from "./Chat.svelte";

  let { open = $bindable(true), defaultOpen = true, users = [], messages = [], disabled = false, send = () => {} } = $props();

  const storageKey = "together-side-panel-open";

  $effect(() => {
    const saved = localStorage.getItem(storageKey);
    open = saved === null ? defaultOpen : saved === "true";
  });

  $effect(() => {
    localStorage.setItem(storageKey, String(open));
  });
</script>

<aside class={`shrink-0 overflow-hidden border-border bg-card transition-[width,height,border-color] duration-base ${open ? "w-full md:w-80 border-t md:border-l md:border-t-0" : "w-0 border-transparent"}`} aria-label="Room side panel" aria-hidden={!open}>
  <div class="h-full min-w-80 flex flex-col">
    <Participants {users} />
    <div class="min-h-0 flex-1"><Chat {messages} {disabled} {send} /></div>
  </div>
</aside>
