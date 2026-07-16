<script>
  import { ApiError, get, post } from "../lib/api.js";
  import { go } from "../lib/router.svelte.js";
  import { Button } from "../components/ui/button/index.js";
  import { Input } from "../components/ui/input/index.js";

  let { token } = $props();
  let roomName = $state("");
  let name = $state("");
  let error = $state("");
  let state = $state("loading"); // loading | form | invalid | full
  let busy = $state(false);

  $effect(() => {
    let active = true;
    roomName = "";
    name = "";
    error = "";
    state = "loading";

    async function load() {
      try {
        const room = await get(`/api/rooms/join/${encodeURIComponent(token)}`);
        if (!active) return;
        roomName = room.roomName;
        try {
          const joined = await post("/api/rooms/join", { token, name: "" });
          if (active) go(`/room/${joined.roomId}`);
        } catch (err) {
          if (active && err instanceof ApiError && err.status === 400) state = "form";
          else if (active && err instanceof ApiError && err.status === 404) state = "invalid";
          else if (active) error = "Couldn't check this invite. Try again.";
        }
      } catch (err) {
        if (active && err instanceof ApiError && err.status === 404) state = "invalid";
        else if (active) error = "Couldn't check this invite. Try again.";
      }
    }
    load();
    return () => { active = false; };
  });

  async function join(event) {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      error = "Name can't be empty.";
      return;
    }
    if (trimmed.length > 32) {
      error = "Name too long (max 32).";
      return;
    }
    busy = true;
    error = "";
    try {
      const joined = await post("/api/rooms/join", { token, name: trimmed });
      go(`/room/${joined.roomId}`);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) state = "invalid";
      else if (err instanceof ApiError && err.status === 409) state = "full";
      else error = err.message;
    } finally {
      busy = false;
    }
  }
</script>

<main class="min-h-dvh grid place-items-center p-4 bg-void">
  <section class="w-full max-w-sm border border-border bg-card p-8 flex flex-col gap-4">
    <div>
      <span class="eyebrow">// together</span>
      {#if roomName}<h1 class="text-fg-strong text-2xl font-semibold tracking-tight mt-1">You're invited to “{roomName}”</h1>{/if}
    </div>

    {#if state === "loading"}
      <p class="text-fg">Checking invite…</p>
    {:else if state === "invalid"}
      <div class="flex flex-col gap-2" role="alert">
        <h2 class="text-fg-strong font-medium">This invite link isn't valid anymore.</h2>
        <p class="text-fg">Ask your host for a new one.</p>
      </div>
    {:else if state === "full"}
      <div class="flex flex-col gap-2" role="alert">
        <h2 class="text-fg-strong font-medium">This room is full.</h2>
        <p class="text-fg">Ask the host to try again when someone leaves.</p>
      </div>
    {:else}
      <form onsubmit={join} class="flex flex-col gap-4">
        <label class="flex flex-col gap-1">
          <span class="text-[13px] font-medium">Your name</span>
          <Input bind:value={name} autocomplete="name" maxlength="33" disabled={busy} />
        </label>
        {#if error}<p class="text-error text-[13px]" role="alert">{error}</p>{/if}
        <Button class="h-11" type="submit" disabled={busy}>{busy ? "Joining…" : "Join room"}</Button>
      </form>
    {/if}
    {#if error && state !== "form"}<p class="text-error text-[13px]" role="alert">{error}</p>{/if}
  </section>
</main>
