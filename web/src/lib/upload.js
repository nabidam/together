import { post } from "./api.js";

const CHUNK = 8 * 1024 * 1024;

// ponytail: resume = ask server how much it has, start there
async function uploadChunks(id, file, onProgress) {
  const { size: start } = await (await fetch(`/api/admin/media/${id}/blob`)).json();
  for (let off = start; off < file.size; off += CHUNK) {
    const res = await fetch(`/api/admin/media/${id}/blob?offset=${off}`, {
      method: "PATCH",
      headers: { "Upload-Length": String(file.size) },
      body: file.slice(off, off + CHUNK),
    });
    if (!res.ok) {
      const err = new Error(await res.text());
      err.status = res.status;
      throw err;
    }
    onProgress?.(Math.min(off + CHUNK, file.size), file.size);
  }
}

async function finish(id) {
  const res = await fetch(`/api/admin/media/${id}/finish`, { method: "POST" });
  if (!res.ok) {
    const err = new Error(await res.text());
    err.status = res.status;
    throw err;
  }
}

export async function uploadMedia({ kind, title, file, subtitle, onProgress }) {
  // ponytail: filename+size as identity; hash-based dedupe never (2-user instance)
  const key = "together.upload." + file.name + "." + file.size;
  let id = localStorage.getItem(key);
  let resumed = !!id;

  if (id) {
    try {
      await uploadChunks(id, file, onProgress);
    } catch (err) {
      if (err.status !== 409) throw err;
      // stale token: row finished/deleted server-side, one retry via fresh create
      // ponytail: resuming a row the worker already processed re-uploads under a new id (blob was moved); duplicate row possible in that narrow window — admin deletes it. Server-side blob-status probe never (2-user instance).
      localStorage.removeItem(key);
      id = null;
      resumed = false;
    }
  }

  if (!id) {
    ({ id } = await post("/api/admin/media", { kind, title, origName: file.name, sizeBytes: file.size }));
    localStorage.setItem(key, id);
    await uploadChunks(id, file, onProgress);
  }

  if (subtitle) {
    const label = encodeURIComponent(subtitle.name.replace(/\.(srt|vtt|ass)$/i, ""));
    const res = await fetch(`/api/admin/media/${id}/subtitle?label=${label}`, { method: "POST", body: subtitle });
    if (!res.ok) throw new Error(await res.text());
  }

  try {
    await finish(id);
  } catch (err) {
    // finish 409 is only reachable on the resume path, and means the row already finished earlier - treat as success
    if (err.status !== 409 || !resumed) throw err;
  }
  localStorage.removeItem(key);
  return id;
}
