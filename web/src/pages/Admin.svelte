<script>
  import { get, post, del } from "../lib/api.js";
  import { go } from "../lib/router.svelte.js";
  import { uploadMedia } from "../lib/upload.js";
  import { ArrowLeft, Trash2, TicketPlus, RefreshCw } from "lucide-svelte";

  let { me } = $props();

  let media = $state([]);
  let invites = $state([]);
  let kind = $state("movie"), title = $state("");
  let file = $state(null), subtitle = $state(null);
  let progress = $state(null), error = $state("");

  async function load() {
    media = await get("/api/media");
    invites = await get("/api/admin/invites");
  }
  $effect(() => {
    load();
    const iv = setInterval(load, 5000); // poll: shows processing → ready
    return () => clearInterval(iv);
  });

  async function submit(e) {
    e.preventDefault(); error = "";
    try {
      progress = 0;
      await uploadMedia({ kind, title, file, subtitle, onProgress: (s, t) => (progress = s / t) });
      title = ""; file = null; subtitle = null;
      e.target.reset();
      await load();
    } catch (err) { error = err.message; } finally { progress = null; }
  }

  const fmtGB = (b) => (b / 1e9).toFixed(2) + " GB";
  const bar = (p) => "#".repeat(Math.round(p * 20)).padEnd(20, "-");
</script>

<main class="min-h-dvh max-w-3xl mx-auto p-6 flex flex-col gap-6">
  <header class="flex items-center gap-3">
    <button class="btn-ghost !h-11 !px-2" onclick={() => go("/")} aria-label="Back"><ArrowLeft size={16} /></button>
    <div>
      <span class="eyebrow">// admin</span>
      <h1 class="text-fg-strong text-2xl font-semibold tracking-tight">Platform</h1>
    </div>
  </header>

  <section class="card p-6 flex flex-col gap-4">
    <span class="eyebrow">// upload media</span>
    <form onsubmit={submit} class="flex flex-col gap-3">
      <div class="flex gap-2">
        <select class="input !w-32" bind:value={kind} aria-label="Media kind">
          <option value="movie">movie</option>
          <option value="music">music</option>
        </select>
        <input class="input" placeholder="Title" bind:value={title} required />
      </div>
      <label class="text-[13px] font-medium flex flex-col gap-1">Media file (.mp4 / .mkv / audio)
        <input class="input !h-auto py-2" type="file" required onchange={(e) => (file = e.target.files[0])} />
      </label>
      <label class="text-[13px] font-medium flex flex-col gap-1">Subtitle (.srt, optional)
        <input class="input !h-auto py-2" type="file" accept=".srt,.vtt,.ass" onchange={(e) => (subtitle = e.target.files[0])} />
      </label>
      {#if progress !== null}
        <p class="font-mono text-[13px] text-primary">[{bar(progress)}] {Math.round(progress * 100)}% uploading…</p>
      {:else}
        <button class="btn-primary self-start">Upload</button>
      {/if}
      {#if error}<p class="text-error text-[13px]" role="alert">{error}</p>{/if}
    </form>
  </section>

  <section class="card p-6 flex flex-col gap-3">
    <div class="flex items-center justify-between">
      <span class="eyebrow">// library</span>
      <button class="btn-ghost !h-11 !px-2" onclick={load} aria-label="Refresh"><RefreshCw size={14} /></button>
    </div>
    <ul class="flex flex-col gap-2">
      {#each media as m (m.id)}
        <li class="flex items-center gap-3 border-b border-border pb-2 last:border-0">
          <span class="font-mono text-[11px] text-fg/60 w-8">#{m.id}</span>
          <span class="text-fg-strong flex-1 min-w-0 truncate">{m.title}</span>
          <span class="font-mono text-[11px]"
                class:text-primary={m.status === "ready"}
                class:text-warning={m.status === "processing" || m.status === "uploading"}
                class:text-error={m.status === "failed"}>● {m.status}</span>
          {#if m.sizeBytes}<span class="font-mono text-[11px] text-fg/60">{fmtGB(m.sizeBytes)}</span>{/if}
          <button class="btn-ghost !h-11 !w-11 !px-0" aria-label="Delete {m.title}"
                  onclick={() => confirm(`Delete "${m.title}"?`) && del(`/api/admin/media/${m.id}`).then(load)}>
            <Trash2 size={14} class="text-error" />
          </button>
        </li>
        {#if m.status === "failed" && m.error}
          <li class="font-mono text-[11px] text-error whitespace-pre-wrap pl-11">{m.error}</li>
        {/if}
      {:else}
        <li class="text-fg text-[13px]">Library is empty.</li>
      {/each}
    </ul>
  </section>

  <section class="card p-6 flex flex-col gap-3">
    <span class="eyebrow">// invites</span>
    <button class="btn-ghost self-start" onclick={() => post("/api/admin/invites").then(load)}>
      <TicketPlus size={16} /> New invite code
    </button>
    <ul class="flex flex-col gap-1">
      {#each invites as i (i.code)}
        <li class="font-mono text-[13px]" class:opacity-40={i.used}>{i.code} {i.used ? "· used" : ""}</li>
      {/each}
    </ul>
  </section>
</main>
