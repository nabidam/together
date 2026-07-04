<script>
  import { get } from "../lib/api.js";
  import { go } from "../lib/router.svelte.js";
  import { connect } from "../lib/ws.js";
  import Chat from "../components/Chat.svelte";
  import Player from "../components/Player.svelte";
  import { ArrowLeft, Circle, Clapperboard } from "lucide-svelte";

  let { me, roomId } = $props();
  let messages = $state([]);
  let users = $state([]);
  let activity = $state(null);
  let movies = $state([]);
  let picking = $state(false);
  let sock;

  $effect(() => {
    get(`/api/rooms/${roomId}/messages`).then((h) => (messages = [...h, ...messages]));
    get("/api/media?kind=movie").then((m) => (movies = m));
    sock = connect(roomId, (m) => {
      if (m.type === "hello") { users = m.users; activity = m.activity; }
      else if (m.type === "presence") users = m.users;
      else if (m.type === "chat") messages = [...messages, { ...m, _k: crypto.randomUUID() }];
      else if (m.type === "activity") activity = m.activity;
    });
    return () => sock.close();
  });

  const activeMedia = $derived(activity && movies.find((m) => m.id === activity.state.mediaId));
</script>

<main class="h-dvh flex flex-col">
  <header class="h-14 border-b border-border flex items-center gap-3 px-4 shrink-0">
    <button class="btn-ghost !h-11 !px-2" onclick={() => go("/")} aria-label="Back to rooms"><ArrowLeft size={16} /></button>
    <span class="eyebrow">// room {roomId}</span>
    <div class="ml-auto flex items-center gap-3 min-w-0 overflow-x-auto">
      {#each users as u (u.id)}
        <span class="flex items-center gap-1.5 text-[13px] text-fg-strong shrink-0 break-words">
          <Circle size={8} class="fill-primary text-primary" aria-hidden="true" />
          {u.username}
        </span>
      {/each}
    </div>
  </header>

  <div class="flex-1 min-h-0 flex flex-col md:flex-row">
    <section class="flex-1 min-h-0 relative">
      {#if activity && activeMedia}
        <Player {activity} {sock} media={activeMedia} onend={() => sock.send({ type: "end" })} />
      {:else}
        <div class="h-full grid place-items-center p-6">
          {#if picking}
            <div class="card p-4 w-full max-w-md flex flex-col gap-2 max-h-[70%] overflow-y-auto">
              <span class="eyebrow">// pick a movie</span>
              {#each movies as m (m.id)}
                <button class="btn-ghost justify-start" onclick={() => { sock.send({ type: "start", mediaId: m.id }); picking = false; }}>
                  <Clapperboard size={16} /> {m.title}
                </button>
              {:else}
                <p class="text-fg p-2">Nothing uploaded yet — ask your admin.</p>
              {/each}
              <button class="text-secondary text-[13px] cursor-pointer hover:underline rounded-sm
              focus-visible:outline-2 focus-visible:outline-secondary focus-visible:outline-offset-2" onclick={() => (picking = false)}>Cancel</button>
            </div>
          {:else}
            <div class="text-center">
              <p class="eyebrow">// no activity</p>
              <button class="btn-primary mt-4" onclick={() => (picking = true)}><Clapperboard size={16} /> Watch a movie</button>
            </div>
          {/if}
        </div>
      {/if}
    </section>
    <aside class="md:w-80 md:border-l border-t md:border-t-0 border-border h-64 md:h-auto shrink-0">
      <Chat {messages} send={(b) => sock.send({ type: "chat", body: b })} />
    </aside>
  </div>
</main>
