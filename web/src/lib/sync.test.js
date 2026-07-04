import { test } from "node:test";
import assert from "node:assert/strict";
import { expectedPosition, correction } from "./sync.js";

test("paused state does not advance", () => {
  const s = { paused: true, position: 30, rate: 1, updatedAt: 0 };
  assert.equal(expectedPosition(s, 99_000), 30);
});

test("playing state advances with rate", () => {
  const s = { paused: false, position: 10, rate: 1, updatedAt: 5_000 };
  assert.equal(expectedPosition(s, 15_000), 20);
});

test("big drift → hard seek", () => {
  assert.deepEqual(correction(100, 105), { seek: 105 });
});

test("small drift behind → speed up", () => {
  assert.deepEqual(correction(100, 100.5), { rate: 1.05 });
});

test("small drift ahead → slow down", () => {
  assert.deepEqual(correction(100.5, 100), { rate: 0.95 });
});

test("in sync at nudged rate → reset rate", () => {
  assert.deepEqual(correction(100, 100.05, 1.05), { rate: 1 });
});

test("in sync at normal rate → nothing", () => {
  assert.equal(correction(100, 100.05, 1), null);
});
