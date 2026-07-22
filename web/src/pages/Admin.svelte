<script>
  import { get, post, del } from "../lib/api.js";
  import { go } from "../lib/router.svelte.js";
  import { uploadMedia } from "../lib/upload.js";
  import { ArrowLeft, Trash2, TicketPlus, RefreshCw } from "lucide-svelte";
  import { Button } from "../components/ui/button/index.js";
  import { Input } from "../components/ui/input/index.js";
  import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table/index.js";

  let { me } = $props();

  let media = $state([]);
  let invites = $state([]);
  let title = $state("");
  let file = $state(null), subtitle = $state(null);
  let progress = $state(null), error = $state("");

  function selectFile(event) {
    file = event.target.files[0] ?? null;
    if (file && !title.trim()) title = file.name.replace(/\.[^.]+$/, "");
  }

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
      await uploadMedia({ title, file, subtitle, onProgress: (s, t) => (progress = s / t) });
      title = ""; file = null; subtitle = null;
      e.target.reset();
      await load();
    } catch (err) { error = err.message; } finally { progress = null; }
  }

  const fmtGB = (b) => (b / 1e9).toFixed(2) + " GB";
  const bar = (p) => "#".repeat(Math.round(p * 20)).padEnd(20, "-");
  const totalBytes = $derived(media.reduce((total, item) => total + (item.sizeBytes || 0), 0));
</script>

<main class="min-h-dvh max-w-3xl mx-auto p-6 flex flex-col gap-6">
  <header class="flex items-center gap-3">
    <Button variant="ghost" size="icon-lg" onclick={() => go("/")} aria-label="Back"><ArrowLeft /></Button>
    <div>
      <span class="eyebrow">// admin</span>
      <h1 class="text-fg-strong text-2xl font-semibold tracking-tight">Platform</h1>
    </div>
  </header>

  <section class="border border-border bg-card p-6 flex flex-col gap-4">
    <span class="eyebrow">// upload media</span>
    <form onsubmit={submit} class="flex flex-col gap-3">
      <Input class="h-11" placeholder="Title" bind:value={title} required />
      <label class="flex flex-col gap-1 text-sm font-medium">Media file (.mp4 / .mkv / audio)
         <Input class="h-auto py-2" type="file" required onchange={selectFile} />
      </label>
      <label class="flex flex-col gap-1 text-sm font-medium">Subtitle (.srt, optional)
        <Input class="h-auto py-2" type="file" accept=".srt,.vtt,.ass" onchange={(e) => (subtitle = e.target.files[0])} />
      </label>
      {#if progress !== null}
        <p class="font-mono text-sm text-primary">[{bar(progress)}] {Math.round(progress * 100)}% uploading…</p>
      {:else}
        <Button class="h-11 self-start" type="submit">Upload</Button>
      {/if}
      {#if error}<p class="text-sm text-error" role="alert">{error}</p>{/if}
    </form>
  </section>

  <section class="border border-border bg-card p-6 flex flex-col gap-3">
    <div class="flex items-center justify-between">
       <span class="eyebrow">// library · {fmtGB(totalBytes)} used</span>
      <Button variant="ghost" size="icon-lg" onclick={load} aria-label="Refresh"><RefreshCw /></Button>
    </div>
    <Table>
      <TableHeader><TableRow><TableHead>Title</TableHead><TableHead>Kind</TableHead><TableHead>Status</TableHead><TableHead>Size</TableHead><TableHead><span class="sr-only">Actions</span></TableHead></TableRow></TableHeader>
      <TableBody>
      {#each media as m (m.id)}
        <TableRow>
          <TableCell class="text-fg-strong"><span class="mr-2 font-mono text-xs text-fg/60">#{m.id}</span>{m.title}</TableCell>
          <TableCell class="font-mono text-xs">{m.kind}</TableCell>
          <TableCell class={`font-mono text-xs ${m.status === "ready" ? "text-primary" : m.status === "failed" ? "text-error" : "text-warning"}`}>● {m.status}</TableCell>
          <TableCell class="font-mono text-xs text-fg/60">{#if m.sizeBytes}{fmtGB(m.sizeBytes)}{/if}</TableCell>
          <TableCell><Button variant="ghost" size="icon-lg" aria-label="Delete {m.title}"
                  onclick={() => confirm(`Delete "${m.title}"?`) && del(`/api/admin/media/${m.id}`).then(load)}>
            <Trash2 class="text-error" />
          </Button></TableCell>
        </TableRow>
        {#if m.status === "failed" && m.error}
          <TableRow><TableCell colspan="5" class="whitespace-pre-wrap font-mono text-xs text-error">{m.error}</TableCell></TableRow>
        {/if}
      {:else}
        <TableRow><TableCell colspan="5">Library is empty.</TableCell></TableRow>
      {/each}
      </TableBody>
    </Table>
  </section>

  <section class="border border-border bg-card p-6 flex flex-col gap-3">
    <span class="eyebrow">// invites</span>
    <Button variant="outline" class="h-11 self-start" onclick={() => post("/api/admin/invites").then(load)}><TicketPlus /> New invite code</Button>
    <ul class="flex flex-col gap-1">
      {#each invites as i (i.code)}
        <li class="font-mono text-sm" class:opacity-40={i.used}>{i.code} {i.used ? "· used" : ""}</li>
      {/each}
    </ul>
  </section>
</main>
