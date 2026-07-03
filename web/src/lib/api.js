async function req(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) throw new Error((await res.text()) || res.statusText);
  const text = await res.text();
  return text ? JSON.parse(text) : null;
}
export const get = (p) => req("GET", p);
export const post = (p, b) => req("POST", p, b);
export const del = (p) => req("DELETE", p);
