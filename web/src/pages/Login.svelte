<script>
  import { post } from "../lib/api.js";
  let { onlogin } = $props();
  let mode = $state("login"); // login | register
  let username = $state(""), password = $state(""), code = $state("");
  let error = $state(""), busy = $state(false);

  async function submit(e) {
    e.preventDefault();
    busy = true; error = "";
    try {
      onlogin(await post(`/api/${mode}`, { username, password, code }));
    } catch (err) { error = err.message; } finally { busy = false; }
  }
</script>

<main class="min-h-dvh grid place-items-center p-4 bg-void">
  <form onsubmit={submit} class="card w-full max-w-sm p-8 flex flex-col gap-4">
    <div>
      <span class="eyebrow">// together</span>
      <h1 class="text-fg-strong text-2xl font-semibold tracking-tight mt-1">
        {mode === "login" ? "Sign in" : "Join with invite"}
      </h1>
    </div>
    <label class="flex flex-col gap-1">
      <span class="text-[13px] font-medium">Username</span>
      <input class="input" bind:value={username} autocomplete="username" required minlength="2" />
    </label>
    <label class="flex flex-col gap-1">
      <span class="text-[13px] font-medium">Password</span>
      <input class="input" type="password" bind:value={password}
        autocomplete={mode === "login" ? "current-password" : "new-password"} required minlength={mode === "login" ? 1 : 8} />
    </label>
    {#if mode === "register"}
      <label class="flex flex-col gap-1">
        <span class="text-[13px] font-medium">Invite code</span>
        <input class="input font-mono" bind:value={code} required />
      </label>
    {/if}
    {#if error}<p class="text-error text-[13px]" role="alert">{error}</p>{/if}
    <button class="btn-primary" disabled={busy}>{busy ? "…" : mode === "login" ? "Sign in" : "Create account"}</button>
    <button type="button" class="text-secondary text-[13px] cursor-pointer text-left hover:underline rounded-sm
      focus-visible:outline-2 focus-visible:outline-secondary focus-visible:outline-offset-2"
      onclick={() => { mode = mode === "login" ? "register" : "login"; error = ""; }}>
      {mode === "login" ? "Have an invite code?" : "Already have an account?"}
    </button>
  </form>
</main>
