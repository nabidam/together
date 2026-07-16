<script>
  import { post } from "../lib/api.js";
  import { go } from "../lib/router.svelte.js";
  import { Button } from "../components/ui/button/index.js";
  import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card/index.js";
  import { Input } from "../components/ui/input/index.js";

  let { onregister } = $props();
  let code = $state("");
  let username = $state("");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);

  async function submit(event) {
    event.preventDefault();
    busy = true;
    error = "";
    try {
      onregister(await post("/api/register", { code, username, password }));
    } catch (err) {
      error = err.message;
    } finally {
      busy = false;
    }
  }
</script>

<main class="min-h-dvh grid place-items-center bg-void p-4">
  <Card class="w-full max-w-sm">
    <CardHeader>
      <span class="eyebrow">// together</span>
      <CardTitle class="text-2xl">Create account</CardTitle>
    </CardHeader>
    <CardContent>
      <form onsubmit={submit} class="flex flex-col gap-4">
        <label class="flex flex-col gap-1.5">
          <span class="text-sm font-medium text-fg-strong">Invite code</span>
          <Input class="h-11 font-mono" bind:value={code} autocomplete="off" required autofocus disabled={busy} />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-sm font-medium text-fg-strong">Username</span>
          <Input class="h-11" bind:value={username} autocomplete="username" required minlength="2" disabled={busy} />
        </label>
        <label class="flex flex-col gap-1.5">
          <span class="text-sm font-medium text-fg-strong">Password</span>
          <Input class="h-11" type="password" bind:value={password} autocomplete="new-password" required minlength="8" disabled={busy} />
        </label>
        {#if error}<p class="text-sm text-error" role="alert">{error}</p>{/if}
        <Button class="h-11" type="submit" disabled={busy}>{busy ? "Creating account…" : "Create account"}</Button>
        <Button variant="link" class="h-11 self-start px-0" onclick={() => go("/")}>Already have an account? Sign in</Button>
      </form>
    </CardContent>
  </Card>
</main>
