<script>
  let { messages, send } = $props();
  let body = $state("");
  let list;

  $effect(() => {
    messages.length;
    if (list) list.scrollTop = list.scrollHeight;
  });

  function submit(e) {
    e.preventDefault();
    if (!body.trim()) return;
    send(body.trim());
    body = "";
  }

  const fmt = (t) => new Date(t * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
</script>

<div class="flex flex-col h-full min-h-0">
  <ul bind:this={list} class="flex-1 overflow-y-auto flex flex-col gap-3 p-4" aria-live="polite">
    {#each messages as m (m.id ?? `${m.createdAt}-${m.userId}-${m.body}`)}
      <li class="text-[15px]">
        <span class="font-mono text-[11px] text-fg/60">{fmt(m.createdAt)}</span>
        <span class="text-secondary font-medium ml-1">{m.username}</span>
        <span class="text-fg-strong ml-1 break-words">{m.body}</span>
      </li>
    {/each}
  </ul>
  <form onsubmit={submit} class="p-3 border-t border-border flex gap-2">
    <input class="input" placeholder="Say something…" bind:value={body} maxlength="2000" aria-label="Chat message" />
    <button class="btn-primary shrink-0">Send</button>
  </form>
</div>
