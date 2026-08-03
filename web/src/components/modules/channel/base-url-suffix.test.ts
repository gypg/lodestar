import assert from "node:assert/strict";
import test from "node:test";

import { getEffectiveBaseUrlSuffixMode, isOpenAICompatBaseUrlSuffixMode } from "./base-url-suffix.ts";

test("base URL suffix defaults empty and auto modes to OpenAI compatibility", () => {
  assert.equal(getEffectiveBaseUrlSuffixMode(undefined), "openai_compat");
  assert.equal(getEffectiveBaseUrlSuffixMode(""), "openai_compat");
  assert.equal(getEffectiveBaseUrlSuffixMode("auto"), "openai_compat");
  assert.equal(getEffectiveBaseUrlSuffixMode("openai_compat"), "openai_compat");
});

test("base URL suffix keeps custom mode explicit", () => {
  assert.equal(getEffectiveBaseUrlSuffixMode("custom"), "custom");
  assert.equal(isOpenAICompatBaseUrlSuffixMode("custom"), false);
  assert.equal(isOpenAICompatBaseUrlSuffixMode(undefined), true);
  assert.equal(isOpenAICompatBaseUrlSuffixMode("auto"), true);
});
