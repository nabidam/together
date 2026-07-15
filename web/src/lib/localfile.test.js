import assert from "node:assert/strict";
import test from "node:test";
import { checkFileSize, replaceObjectURL, revokeObjectURL } from "./localfile.js";

test("checkFileSize accepts a file whose bytes match room media", () => {
  assert.deepEqual(checkFileSize({ size: 8192 }, { media: { sizeBytes: 8192 } }), {
    ok: true,
    selectedSize: 8192,
    expectedSize: 8192,
  });
});

test("checkFileSize reports the selected and expected byte counts on mismatch", () => {
  assert.deepEqual(checkFileSize({ size: 4096 }, { media: { sizeBytes: 8192 } }), {
    ok: false,
    selectedSize: 4096,
    expectedSize: 8192,
  });
});

test("replaceObjectURL revokes the previous blob URL before creating the replacement", () => {
  const calls = [];
  const url = {
    createObjectURL: (file) => {
      calls.push(["create", file.name]);
      return "blob:new";
    },
    revokeObjectURL: (value) => calls.push(["revoke", value]),
  };

  assert.equal(replaceObjectURL("blob:old", { name: "movie.mkv" }, url), "blob:new");
  revokeObjectURL("/media/1/stream", url);
  assert.deepEqual(calls, [["revoke", "blob:old"], ["create", "movie.mkv"]]);
});
