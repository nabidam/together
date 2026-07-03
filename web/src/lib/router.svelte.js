// ponytail: hash router in 8 lines; add a real router never (3 pages)
export const route = $state({ path: location.hash.slice(1) || "/" });

window.addEventListener("hashchange", () => {
  route.path = location.hash.slice(1) || "/";
});

export function go(path) {
  location.hash = path;
}
