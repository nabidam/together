// ws.js — reconnecting room socket with an EMA clock offset.
// It deliberately reports failures as connection state so components never
// need to catch WebSocket errors.
export function connect(roomId, onMessage, onState = () => {}) {
  let sock;
  let closed = false;
  let delay = 1000;
  let offset = 0;
  let haveOffset = false;
  let reconnectTimer;
  let pingTimer;
  let opened = false;

  function report(state) {
    onState(state);
  }

  function open() {
    if (closed) return;
    const protocol = location.protocol === "https:" ? "wss" : "ws";
    try {
      sock = new WebSocket(`${protocol}://${location.host}/ws/${roomId}`);
    } catch {
      scheduleReconnect();
      return;
    }
    sock.onopen = () => {
      opened = true;
      delay = 1000;
      report("connected");
      clearTimeout(pingTimer);
      ping();
    };
    sock.onmessage = (event) => {
      let message;
      try {
        message = JSON.parse(event.data);
      } catch {
        return;
      }
      if (message.type === "pong") {
        const roundTrip = Date.now() - message.t;
        const sample = message.serverTime - (message.t + roundTrip / 2);
        offset = haveOffset ? offset * 0.8 + sample * 0.2 : sample;
        haveOffset = true;
        return;
      }
      onMessage(message);
    };
    sock.onclose = () => {
      if (!closed) scheduleReconnect();
    };
    sock.onerror = () => {};
  }

  function scheduleReconnect() {
    if (closed || reconnectTimer) return;
    report(opened ? "reconnecting" : "connecting");
    reconnectTimer = setTimeout(() => {
      reconnectTimer = undefined;
      open();
    }, delay);
    delay = Math.min(delay * 2, 30000);
  }

  function ping() {
    if (closed) return;
    if (sock?.readyState === WebSocket.OPEN) {
      sock.send(JSON.stringify({ type: "ping", t: Date.now() }));
    }
    pingTimer = setTimeout(ping, 10000);
  }

  report("connecting");
  open();
  return {
    send: (object) => {
      if (sock?.readyState !== WebSocket.OPEN) return false;
      sock.send(JSON.stringify(object));
      return true;
    },
    close: () => {
      closed = true;
      clearTimeout(reconnectTimer);
      clearTimeout(pingTimer);
      sock?.close();
    },
    offset: () => offset,
  };
}
