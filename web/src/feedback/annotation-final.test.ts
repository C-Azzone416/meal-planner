import { describe, expect, it, vi } from "vitest";
import type { CompositeDeps } from "./annotation-composite";
import { finalScreenshot } from "./annotation-final";
import { EMPTY_DRAWING, beginStroke, endStroke, type Drawing } from "./annotation-model";
import { SCREENSHOT_SEND_LIMIT_BYTES } from "./screenshot-limit";

const png = (bytes: number) => new Blob([new Uint8Array(bytes)], { type: "image/png" });

const marked: Drawing = endStroke(
  beginStroke(EMPTY_DRAWING, { color: "red", width: 6, unit: 1 }, { x: 5, y: 5 }),
);

function composing(output: Blob | null) {
  const decode = vi.fn(async () => ({
    image: "i",
    width: 100,
    height: 100,
    close: () => undefined,
  }));
  const deps: CompositeDeps = {
    decode,
    createCanvas: () => ({
      context: () => ({
        lineCap: "",
        lineJoin: "",
        lineWidth: 0,
        strokeStyle: "",
        fillStyle: "",
        save: () => undefined,
        restore: () => undefined,
        beginPath: () => undefined,
        moveTo: () => undefined,
        lineTo: () => undefined,
        quadraticCurveTo: () => undefined,
        arc: () => undefined,
        stroke: () => undefined,
        fill: () => undefined,
        drawImage: () => undefined,
      }),
      toPng: async () => output,
    }),
  };
  return { deps, decode };
}

describe("what is sent as the screenshot", () => {
  it("is nothing when the screenshot is unticked, whatever was drawn, and nothing is composited", async () => {
    const { deps, decode } = composing(png(10));
    const original = png(10);
    expect(await finalScreenshot({ include: false, original, drawing: marked, deps })).toEqual({
      kind: "none",
    });
    expect(decode).not.toHaveBeenCalled();
  });

  it("is nothing when there is no screenshot to send", async () => {
    expect(await finalScreenshot({ include: true, original: null, drawing: marked })).toEqual({
      kind: "none",
    });
  });

  it("is the very picture that was reviewed when nothing was drawn, with no work done on it", async () => {
    const { deps, decode } = composing(png(10));
    const original = png(1000);
    const result = await finalScreenshot({
      include: true,
      original,
      drawing: EMPTY_DRAWING,
      deps,
    });
    expect(result.kind).toBe("ready");
    expect(result.kind === "ready" && result.blob).toBe(original);
    expect(result.kind === "ready" && result.annotated).toBe(false);
    expect(decode).not.toHaveBeenCalled();
  });

  it("is the composited picture when marks were drawn", async () => {
    const output = png(2000);
    const result = await finalScreenshot({
      include: true,
      original: png(1000),
      drawing: marked,
      deps: composing(output).deps,
    });
    expect(result).toEqual({ kind: "ready", blob: output, annotated: true });
  });

  it("includes a stroke that is still being drawn when Send is pressed", async () => {
    const open = beginStroke(EMPTY_DRAWING, { color: "red", width: 6, unit: 1 }, { x: 1, y: 1 });
    const output = png(2000);
    const result = await finalScreenshot({
      include: true,
      original: png(1000),
      drawing: open,
      deps: composing(output).deps,
    });
    expect(result.kind === "ready" && result.annotated).toBe(true);
  });

  it("accepts a result of exactly the limit and refuses one byte more", async () => {
    const under = await finalScreenshot({
      include: true,
      original: png(10),
      drawing: marked,
      deps: composing(png(SCREENSHOT_SEND_LIMIT_BYTES)).deps,
    });
    expect(under.kind).toBe("ready");
    const at = await finalScreenshot({
      include: true,
      original: png(10),
      drawing: marked,
      deps: composing(png(SCREENSHOT_SEND_LIMIT_BYTES + 1)).deps,
    });
    expect(at).toEqual({
      kind: "too-large",
      bytes: SCREENSHOT_SEND_LIMIT_BYTES + 1,
      annotated: true,
    });
  });

  it("says too large for an annotated picture over the limit: no shrunk copy, no crop, not the unmarked picture", async () => {
    const original = png(1000);
    const result = await finalScreenshot({
      include: true,
      original,
      drawing: marked,
      deps: composing(png(SCREENSHOT_SEND_LIMIT_BYTES + 500)).deps,
    });
    expect(result).toEqual({
      kind: "too-large",
      bytes: SCREENSHOT_SEND_LIMIT_BYTES + 500,
      annotated: true,
    });
    expect(result).not.toHaveProperty("blob");
  });

  it("says too large for an unmarked picture that is already over the limit", async () => {
    const result = await finalScreenshot({
      include: true,
      original: png(SCREENSHOT_SEND_LIMIT_BYTES + 1),
      drawing: EMPTY_DRAWING,
    });
    expect(result).toEqual({
      kind: "too-large",
      bytes: SCREENSHOT_SEND_LIMIT_BYTES + 1,
      annotated: false,
    });
  });

  it("says failed, without falling back to the unmarked picture, when the marks cannot be drawn", async () => {
    const result = await finalScreenshot({
      include: true,
      original: png(1000),
      drawing: marked,
      deps: composing(null).deps,
    });
    expect(result).toEqual({ kind: "failed" });
  });
});
