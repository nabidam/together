import { post } from "./api.js";

const CHUNK = 8 * 1024 * 1024;

export async function uploadMedia({ kind, title, file, subtitle, onProgress }) {
  const { id } = await post("/api/admin/media", { kind, title, origName: file.name });
  // ponytail: resume = ask server how much it has, start there
  const { size: start } = await (await fetch(`/api/admin/media/${id}/blob`)).json();
  for (let off = start; off < file.size; off += CHUNK) {
    const res = await fetch(`/api/admin/media/${id}/blob?offset=${off}`, {
      method: "PATCH",
      body: file.slice(off, off + CHUNK),
    });
    if (!res.ok) throw new Error(await res.text());
    onProgress?.(Math.min(off + CHUNK, file.size), file.size);
  }
  if (subtitle) {
    const label = encodeURIComponent(subtitle.name.replace(/\.(srt|vtt|ass)$/i, ""));
    const res = await fetch(`/api/admin/media/${id}/subtitle?label=${label}`, { method: "POST", body: subtitle });
    if (!res.ok) throw new Error(await res.text());
  }
  await post(`/api/admin/media/${id}/finish`);
  return id;
}
