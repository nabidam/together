// localfile.js keeps local-media handling free of DOM components and file reads.
// A blob URL points at the browser-managed File; it does not buffer its bytes.
export function checkFileSize(file, meta) {
  const expectedSize = meta.media.sizeBytes;
  const selectedSize = file.size;
  if (selectedSize === expectedSize) return { ok: true, selectedSize, expectedSize };
  return { ok: false, selectedSize, expectedSize };
}

export function createObjectURL(file, url = URL) {
  return url.createObjectURL(file);
}

export function revokeObjectURL(objectURL, url = URL) {
  if (objectURL?.startsWith("blob:")) url.revokeObjectURL(objectURL);
}

export function replaceObjectURL(previousURL, file, url = URL) {
  revokeObjectURL(previousURL, url);
  return createObjectURL(file, url);
}
