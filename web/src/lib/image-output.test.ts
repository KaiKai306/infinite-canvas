import assert from "node:assert/strict";
import test from "node:test";
import { imageOutputParameters, prepareImageOutputConfig, supportsGifOutput } from "./image-output";
import type { AiConfig } from "@/stores/use-config-store";

test("GIF requests carry explicit animation parameters and disable intermediate images", () => {
    const config = { model: "gpt-image-2.6-flare", apiMode: "images", imageOutputFormat: "gif", imageFrameCount: "4", imageFrameDurationMs: "120", streamImages: "1", responseFormatB64Json: "1" } as AiConfig;
    assert.deepEqual(imageOutputParameters(config), { output_format: "gif", frame_count: 4, frame_duration_ms: 120 });
    const prepared = prepareImageOutputConfig(config);
    assert.equal(prepared.streamImages, "");
    assert.equal(prepared.responseFormatB64Json, "");
    assert.equal(config.streamImages, "1");
});

test("existing PNG requests keep their configuration and send no new fields", () => {
    const config = { model: "gemini-image", apiMode: "responses", streamImages: "1" } as AiConfig;
    assert.deepEqual(imageOutputParameters(config), {});
    assert.equal(prepareImageOutputConfig(config), config);
});

test("incompatible models, API modes and animation limits fail before submission", () => {
    const base = { model: "gpt-image-2.6-flare", apiMode: "images", imageOutputFormat: "gif" };
    assert.equal(supportsGifOutput("gpt-image-2.5-flare"), false);
    assert.equal(supportsGifOutput("GPT-Image-2.6-Sunburst-2026-11-01"), true);
    assert.deepEqual(imageOutputParameters(base), { output_format: "gif", frame_count: 4, frame_duration_ms: 200 });
    for (const patch of [{ model: "gpt-image-2" }, { apiMode: "responses" }, { imageFrameCount: "17" }, { imageFrameCount: "2.5" }, { imageFrameDurationMs: "19" }, { imageFrameDurationMs: "2001" }]) {
        assert.throws(() => imageOutputParameters({ ...base, ...patch }));
    }
});
