<script>
  import { go } from "../lib/router.svelte.js";
  import { Button } from "./ui/button/index.js";

  let { accountUser = false } = $props();
</script>

<main class="min-h-dvh grid place-items-center p-6 bg-void">
  <section class="w-full max-w-md border border-border bg-card p-8 text-center flex flex-col items-center gap-4">
    <span class="eyebrow">// room closed</span>
    <h1 class="text-fg-strong text-2xl font-semibold">This room has ended.</h1>
    <p class="text-fg">Chat and playback are gone — that's by design.</p>
    {#if accountUser}
      <Button class="mt-2 h-11" onclick={() => go("/")}>Back to rooms</Button>
    {/if}
  </section>
</main>
