<script>
  import { get, post } from "../lib/api.js";
  import { go } from "../lib/router.svelte.js";
  import { Plus, LogOut, Shield, DoorOpen } from "lucide-svelte";

  let { me, onlogout } = $props();
  let rooms = $state([]);
  let name = $state("");
  let error = $state("");

  async function load() { rooms = await get("/api/rooms"); }
  $effect(() => { load(); });

  async function create(e) {
    e.preventDefault(); error = "";
    try {
      const r = await post("/api/rooms", { name });
      name = "";
      go(`/rooms/${r.id}`);
    } catch (err) { error = err.message; }
  }

  async function logout() { await post("/api/logout"); onlogout(); }
</script>

<main class="min-h-dvh max-w-3xl mx-auto p-6 flex flex-col gap-6">
  <header class="flex items-center justify-between gap-4">
    <div>
      <span class="eyebrow">// together</span>
      <h1 class="text-fg-strong text-2xl font-semibold tracking-tight">Rooms</h1>
    </div>
    <div class="flex gap-2">
      {#if me.role === "admin"}
        <button class="btn-ghost" onclick={() => go("/admin")}><Shield size={16} /> Admin</button>
      {/if}
      <button class="btn-ghost" onclick={logout} aria-label="Sign out"><LogOut size={16} /></button>
    </div>
  </header>

  <form onsubmit={create} class="flex gap-2">
    <input class="input" placeholder="New room name" bind:value={name} required maxlength="60" />
    <button class="btn-primary shrink-0"><Plus size={16} /> Create</button>
  </form>
  {#if error}<p class="text-error text-[13px]" role="alert">{error}</p>{/if}

  <ul class="flex flex-col gap-2">
    {#each rooms as r (r.id)}
      <li>
        <button class="card w-full p-4 flex items-center justify-between text-left cursor-pointer
                       transition-colors duration-200 hover:border-fg/30
                       focus-visible:outline-2 focus-visible:outline-secondary"
          onclick={() => go(`/rooms/${r.id}`)}>
          <span class="text-fg-strong font-medium">{r.name}</span>
          <span class="eyebrow flex items-center gap-2"><DoorOpen size={14} /> enter</span>
        </button>
      </li>
    {:else}
      <li class="card p-8 text-center">
        <p class="text-fg">No rooms yet.</p>
        <p class="eyebrow mt-2">// create one above and invite your favorite person</p>
      </li>
    {/each}
  </ul>
</main>
