// ws.js — reconnecting room socket with NTP-ish clock offset
export function connect(roomId, onmsg) {
  let sock, closed = false, delay = 1000, offset = 0, haveOffset = false, reconnectTimer, pingTimer;

  function open() {
    if (closed) return;
    const proto = location.protocol === "https:" ? "wss" : "ws";
    sock = new WebSocket(`${proto}://${location.host}/ws/${roomId}`);
    sock.onopen = () => {
      delay = 1000;
      clearTimeout(pingTimer);
      ping();
    };
    sock.onmessage = (e) => {
      const m = JSON.parse(e.data);
      if (m.type === "pong") {
        const rtt = Date.now() - m.t;
        const sample = m.serverTime - (m.t + rtt / 2);
        offset = haveOffset ? offset * 0.8 + sample * 0.2 : sample;
        haveOffset = true;
        return;
      }
      onmsg(m);
    };
    sock.onclose = () => {
      if (closed) return;
      reconnectTimer = setTimeout(open, delay);
      delay = Math.min(delay * 2, 8000);
    };
  }

  function ping() {
    if (closed) return;
    if (sock.readyState === 1) sock.send(JSON.stringify({ type: "ping", t: Date.now() }));
    pingTimer = setTimeout(ping, 10000);
  }

  open();
  return {
    send: (obj) => sock.readyState === 1 && sock.send(JSON.stringify(obj)),
    close: () => { clearTimeout(reconnectTimer); clearTimeout(pingTimer); closed = true; sock.close(); },
    offset: () => offset,
  };
}
