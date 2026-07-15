import assert from "node:assert/strict";
import test from "node:test";
import { connect } from "./ws.js";

class FakeSocket {
  static OPEN = 1;
  static instances = [];

  constructor(url) {
    this.url = url;
    this.readyState = 0;
    FakeSocket.instances.push(this);
  }

  open() {
    this.readyState = FakeSocket.OPEN;
    this.onopen();
  }

  message(value) {
    this.onmessage({ data: JSON.stringify(value) });
  }

  close() {
    this.readyState = 3;
    this.onclose();
  }

  send(value) {
    this.sent = [...(this.sent ?? []), JSON.parse(value)];
  }
}

test("connect reports connection changes and forwards V2 frames", () => {
  const oldWebSocket = globalThis.WebSocket;
  const oldLocation = globalThis.location;
  globalThis.WebSocket = FakeSocket;
  globalThis.location = { protocol: "http:", host: "together.test" };
  FakeSocket.instances = [];

  const frames = [];
  const states = [];
  const socket = connect("room-id", (frame) => frames.push(frame), (state) => states.push(state));
  const fake = FakeSocket.instances[0];
  fake.open();
  fake.message({ type: "hello", users: [], chat: [], room: { name: "Movie Night" } });
  fake.message({ type: "pong", t: Date.now(), serverTime: Date.now() });
  fake.close();
  socket.close();

  assert.equal(fake.url, "ws://together.test/ws/room-id");
  assert.deepEqual(frames, [{ type: "hello", users: [], chat: [], room: { name: "Movie Night" } }]);
  assert.deepEqual(states, ["connecting", "connected", "reconnecting"]);

  globalThis.WebSocket = oldWebSocket;
  globalThis.location = oldLocation;
});
