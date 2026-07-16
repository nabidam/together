<script>
  import { route, go } from "./lib/router.svelte.js";
  import { get } from "./lib/api.js";
  import Login from "./pages/Login.svelte";
  import Register from "./pages/Register.svelte";
  import Home from "./pages/Home.svelte";
  import JoinGuest from "./pages/JoinGuest.svelte";
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

  const joinMatch = $derived(route.path.match(/^\/join\/([^/]+)$/));
  const roomMatch = $derived(route.path.match(/^\/room\/([^/]+)$/));
</script>

{#if joinMatch}
  <JoinGuest token={joinMatch[1]} />
{:else if !checked}
  <div class="min-h-dvh grid place-items-center">
    <span class="eyebrow">// connecting…</span>
  </div>
{:else if roomMatch}
  <Room {me} roomId={roomMatch[1]} />
{:else if !me}
  {#if route.path === "/register"}
    <Register onregister={(u) => (me = u)} />
  {:else}
    <Login onlogin={(u) => (me = u)} />
  {/if}
{:else if route.path === "/admin" && me.role === "admin"}
  <Admin {me} />
{:else}
  <Home {me} onlogout={() => { me = null; go("/"); }} />
{/if}
