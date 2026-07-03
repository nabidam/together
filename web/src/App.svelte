<script>
  import { route, go } from "./lib/router.svelte.js";
  import { get } from "./lib/api.js";
  import Login from "./pages/Login.svelte";
  import Rooms from "./pages/Rooms.svelte";
  import Room from "./pages/Room.svelte";
  import Admin from "./pages/Admin.svelte";

  let me = $state(null);
  let checked = $state(false);

  $effect(() => {
    get("/api/me")
      .then((u) => (me = u))
      .catch(() => (me = null))
      .finally(() => (checked = true));
  });

  const roomMatch = $derived(route.path.match(/^\/rooms\/(\d+)$/));
</script>

{#if !checked}
  <div class="min-h-dvh grid place-items-center">
    <span class="eyebrow">// connecting…</span>
  </div>
{:else if !me}
  <Login onlogin={(u) => (me = u)} />
{:else if roomMatch}
  <Room {me} roomId={Number(roomMatch[1])} />
{:else if route.path === "/admin" && me.role === "admin"}
  <Admin {me} />
{:else}
  <Rooms {me} onlogout={() => { me = null; go("/"); }} />
{/if}
