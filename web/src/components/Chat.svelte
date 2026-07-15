<script>
  let { messages, send, disabled = false } = $props();
  let body = $state("");
  let list;

  $effect(() => {
    messages.length;
    if (list) list.scrollTop = list.scrollHeight;
  });

  function submit(event) {
    event.preventDefault();
    const text = body.trim();
    if (!text || disabled) return;
    send(text);
    body = "";
  }

  const fmt = (seconds) => new Date(seconds * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
</script>

<div class="flex flex-col h-full min-h-0">
  <div class="px-4 pt-4"><h2 class="text-fg-strong font-medium">Chat</h2></div>
  <ul bind:this={list} class="flex-1 overflow-y-auto flex flex-col gap-3 p-4" aria-live="polite">
    {#each messages as message, index (`${message.name}-${message.createdAt}-${message.body}-${index}`)}
      <li class="text-[15px]">
        <span class="font-mono text-[11px] text-fg/60">{fmt(message.createdAt)}</span>
        <span class="text-secondary font-medium ml-1">{message.name}</span>
        <span class="text-fg-strong ml-1 break-words">{message.body}</span>
      </li>
    {:else}
      <li class="text-fg text-[15px]">No messages yet.</li>
    {/each}
  </ul>
  <form onsubmit={submit} class="p-3 border-t border-border flex gap-2">
    <input class="input" placeholder="Say something…" bind:value={body} maxlength="2000" disabled={disabled} aria-label="Chat message" />
    <button class="btn-primary shrink-0" disabled={disabled}>Send</button>
  </form>
</div>
