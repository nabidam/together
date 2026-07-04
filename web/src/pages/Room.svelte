<script>
  import { get } from "../lib/api.js";
  import { go } from "../lib/router.svelte.js";
  import { connect } from "../lib/ws.js";
  import Chat from "../components/Chat.svelte";
  import { ArrowLeft, Circle } from "lucide-svelte";

  let { me, roomId } = $props();
  let messages = $state([]);
  let users = $state([]);
  let activity = $state(null);
  let sock;

  $effect(() => {
    get(`/api/rooms/${roomId}/messages`).then((h) => (messages = [...h, ...messages]));
    sock = connect(roomId, (m) => {
      if (m.type === "hello") { users = m.users; activity = m.activity; }
      else if (m.type === "presence") users = m.users;
      else if (m.type === "chat") messages = [...messages, { ...m, _k: crypto.randomUUID() }];
      else if (m.type === "activity") activity = m.activity;
    });
    return () => sock.close();
  });
</script>

<main class="h-dvh flex flex-col">
  <header class="h-14 border-b border-border flex items-center gap-3 px-4 shrink-0">
    <button class="btn-ghost !h-9 !px-2" onclick={() => go("/")} aria-label="Back to rooms"><ArrowLeft size={16} /></button>
    <span class="eyebrow">// room {roomId}</span>
    <div class="ml-auto flex items-center gap-3">
      {#each users as u (u.id)}
        <span class="flex items-center gap-1.5 text-[13px] text-fg-strong">
          <Circle size={8} class="fill-primary text-primary" aria-hidden="true" />
          {u.username}
        </span>
      {/each}
    </div>
  </header>

  <div class="flex-1 min-h-0 flex flex-col md:flex-row">
    <section class="flex-1 min-h-0 grid place-items-center p-6">
      <!-- Player mounts here in Task 11 -->
      <div class="text-center">
        <p class="eyebrow">// no activity</p>
        <p class="text-fg mt-2">Start watching something together from here soon.</p>
      </div>
    </section>
    <aside class="md:w-80 md:border-l border-t md:border-t-0 border-border h-64 md:h-auto shrink-0">
      <Chat {messages} send={(b) => sock.send({ type: "chat", body: b })} />
    </aside>
  </div>
</main>
