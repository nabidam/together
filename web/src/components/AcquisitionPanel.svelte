<script>
  import { checkFileSize, createObjectURL } from "../lib/localfile.js";
  import { Download, FolderOpen, TriangleAlert } from "lucide-svelte";
  import { Button } from "./ui/button/index.js";
  import { Card, CardContent } from "./ui/card/index.js";
  import PlayFromServerDialog from "./PlayFromServerDialog.svelte";

  let { media, kind, onsource = () => {} } = $props();
  let picker;
  let mismatch = $state(null);

  const formatBytes = (bytes) => {
    if (bytes < 1024) return `${bytes} B`;
    const units = ["KB", "MB", "GB", "TB"];
    const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length);
    return `${(bytes / 1024 ** exponent).toFixed(exponent > 1 ? 1 : 0)} ${units[exponent - 1]}`;
  };

  function openPicker() {
    picker?.click();
  }

  function selectFile(event) {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = "";
    if (!file) return;
    const result = checkFileSize(file, { media });
    if (!result.ok) {
      mismatch = { ...result, name: file.name };
      return;
    }
    mismatch = null;
    onsource(createObjectURL(file));
  }

</script>

<section class="h-full w-full grid place-items-center p-6 bg-void">
  <Card class="w-full max-w-xl">
   <CardContent class="p-6 flex flex-col items-center text-center gap-5">
    <div>
      <p class="eyebrow">Media acquisition</p>
      <h2 class="text-fg-strong text-xl font-medium mt-2">{media.title}</h2>
      <p class="text-fg mt-1">{formatBytes(media.sizeBytes)} · {kind}</p>
    </div>

    <div class="w-full grid sm:grid-cols-2 gap-3">
      <Button class="h-11" href={`/media/${media.id}/download`}><Download />Download from server</Button>
      <Button class="h-11" onclick={openPicker}><FolderOpen />Load your copy</Button>
    </div>
    <input bind:this={picker} class="sr-only" type="file" onchange={selectFile} aria-label="Choose a local media file" />

    <p class="text-fg text-sm">After downloading, load the saved file here.</p>

    {#if mismatch}
      <div class="w-full border border-warning/60 bg-warning/10 p-4 text-left" role="alert">
        <div class="flex gap-2 text-warning"><TriangleAlert size={18} aria-hidden="true" /><strong>That file doesn't match.</strong></div>
        <p class="text-fg mt-3">Selected: {mismatch.name} · {formatBytes(mismatch.selectedSize)}</p>
        <p class="text-fg">Expected: {formatBytes(mismatch.expectedSize)}</p>
        <Button variant="ghost" class="mt-4 h-11" onclick={openPicker}>Choose a different file</Button>
      </div>
    {/if}

    <PlayFromServerDialog onstream={() => onsource(`/media/${media.id}/stream`)} />
   </CardContent>
  </Card>
</section>
