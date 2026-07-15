export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}

async function req(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  if (!res.ok) {
    let message = res.statusText;
    try {
      message = JSON.parse(text).error || message;
    } catch {
      message = text || message;
    }
    throw new ApiError(message, res.status);
  }
  return text ? JSON.parse(text) : null;
}
export const get = (p) => req("GET", p);
export const post = (p, b) => req("POST", p, b);
export const del = (p) => req("DELETE", p);
