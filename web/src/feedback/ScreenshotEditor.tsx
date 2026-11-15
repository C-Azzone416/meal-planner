"use client";

import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from "react";

import { paintStrokes, type DrawingSurface } from "./annotation-composite";
import {
  DEFAULT_INK,
  INK_COLORS,
  allStrokes,
  canUndo,
  fitScale,
  hasMarks,
  inkFor,
  strokeCount,
  toNaturalPoint,
  type InkColorId,
  type Size,
} from "./annotation-model";
import type { AnnotationDraft } from "./use-annotation-draft";

// The marking tools and preview inside the feedback sheet (#266, design's spec on the issue). The picture is shown whole and
// letterboxed; the person draws on a canvas laid over it. Marks are only kept as data (annotation-model.ts) until Send, when the
// sheet composites them onto the reviewed PNG. Drawing needs a pointer; the notes are the way to describe a problem without one.
export const EDITOR_COPY = {
  inkGroup: "Ink colour",
  undo: "Undo",
  clear: "Clear drawing",
  hint: "Draw on the screenshot with your finger or mouse.",
  cleared: "Drawing cleared. Undo brings it back.",
  canvasLabel:
    "Your screenshot. Marks you draw are sent with it. Drawing needs a finger, a pen or a mouse. You can describe the problem in the notes instead.",
} as const;

const marks = (count: number) => `${count} ${count === 1 ? "mark" : "marks"}.`;

type Note = "marked" | "undone" | "cleared" | "restored" | null;

export function ScreenshotEditor({
  blob,
  draft,
  disabled,
}: {
  blob: Blob;
  draft: AnnotationDraft;
  disabled: boolean;
}) {
  const { drawing } = draft;
  const [ink, setInk] = useState<InkColorId>(DEFAULT_INK);
  const [loaded, setLoaded] = useState<{ url: string; size: Size } | null>(null);
  const [note, setNote] = useState<Note>(null);
  // The picture is shown from a local object URL, released when the sheet goes or the picture changes.
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    // Allocate per effect lifetime: StrictMode's setup/cleanup/setup must not
    // reuse a URL already revoked by the first cleanup (notably WebKit).
    const created = URL.createObjectURL(blob);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- synchronizes an external browser resource lifetime
    setUrl(created);
    return () => URL.revokeObjectURL(created);
  }, [blob]);
  // Its size is only known once it has loaded, and belongs to this URL: a different picture starts without one.
  const natural = loaded && loaded.url === url ? loaded.size : null;
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const swatches = useRef<(HTMLButtonElement | null)[]>([]);
  const pointer = useRef<number | null>(null);
  const touches = useRef(new Set<number>());
  const blocked = useRef(false);

  // The live view and the saved picture are drawn by the same code, so what is seen is what is sent.
  useEffect(() => {
    const canvas = canvasRef.current;
    const context = canvas?.getContext("2d") as unknown as
      (DrawingSurface & { clearRect(x: number, y: number, w: number, h: number): void }) | null;
    if (!canvas || !context || !natural) return;
    context.clearRect(0, 0, natural.width, natural.height);
    paintStrokes(context, allStrokes(drawing));
  }, [drawing, natural]);

  function locate(event: ReactPointerEvent<HTMLCanvasElement>) {
    const canvas = canvasRef.current;
    if (!canvas || !natural) return null;
    const box = canvas.getBoundingClientRect();
    const rect = { left: box.left, top: box.top, width: box.width, height: box.height };
    const mapped = toNaturalPoint({ x: event.clientX, y: event.clientY }, rect, natural);
    const scale = fitScale(rect, natural);
    return mapped && scale !== null ? { ...mapped, scale } : null;
  }

  function down(event: ReactPointerEvent<HTMLCanvasElement>) {
    if (disabled) return;
    if (event.pointerType === "mouse" && event.button !== 0) return;
    if (event.pointerType === "touch") {
      touches.current.add(event.pointerId);
      // A second finger is a pinch or a scroll, never a mark: the stroke in progress is dropped and drawing waits for both to lift.
      if (touches.current.size > 1) {
        blocked.current = true;
        pointer.current = null;
        draft.cancel();
        return;
      }
    }
    if (blocked.current || pointer.current !== null) return;
    const where = locate(event);
    if (!where?.inside) return;
    event.preventDefault();
    pointer.current = event.pointerId;
    try {
      // Keeps the stroke going when the pointer leaves the canvas. It is a nicety: a browser that refuses (a pointer that is
      // already gone) must not stop the mark from being drawn.
      event.currentTarget.setPointerCapture?.(event.pointerId);
    } catch {
      // Drawing carries on without capture.
    }
    draft.begin(inkFor(ink, where.scale), where.point);
  }

  function move(event: ReactPointerEvent<HTMLCanvasElement>) {
    if (event.pointerId !== pointer.current) return;
    const where = locate(event);
    if (where) draft.extend(where.point);
  }

  function lift(event: ReactPointerEvent<HTMLCanvasElement>, ended: boolean) {
    touches.current.delete(event.pointerId);
    if (touches.current.size === 0) blocked.current = false;
    if (event.pointerId !== pointer.current) return;
    pointer.current = null;
    if (ended) {
      const where = locate(event);
      if (where) draft.extend(where.point);
      draft.end();
      setNote("marked");
    } else {
      draft.cancel();
    }
  }

  function pickByKey(event: ReactKeyboardEvent<HTMLButtonElement>, index: number) {
    const last = INK_COLORS.length - 1;
    const next =
      event.key === "ArrowRight" || event.key === "ArrowDown"
        ? (index + 1) % INK_COLORS.length
        : event.key === "ArrowLeft" || event.key === "ArrowUp"
          ? (index + last) % INK_COLORS.length
          : event.key === "Home"
            ? 0
            : event.key === "End"
              ? last
              : null;
    if (next === null) return;
    event.preventDefault();
    setInk(INK_COLORS[next].id);
    swatches.current[next]?.focus();
  }

  const count = strokeCount(drawing);
  const status =
    note === "cleared" && !hasMarks(drawing)
      ? EDITOR_COPY.cleared
      : count === 0
        ? EDITOR_COPY.hint
        : marks(count);
  const announcement =
    note === "marked"
      ? `Mark added. ${marks(count)}`
      : note === "undone"
        ? "Undid the last mark."
        : note === "cleared"
          ? "Drawing cleared."
          : note === "restored"
            ? `Drawing restored. ${marks(count)}`
            : "";

  return (
    <div className="shot-editor" data-capture="FRAMEBRIAR_ANNOTATION_ACTIVE">
      <div className="shot-tools">
        <div role="radiogroup" aria-label={EDITOR_COPY.inkGroup} className="ink-picker">
          {INK_COLORS.map((color, index) => {
            const selected = color.id === ink;
            return (
              <button
                key={color.id}
                ref={(node) => {
                  swatches.current[index] = node;
                }}
                type="button"
                role="radio"
                aria-checked={selected}
                aria-disabled={disabled || undefined}
                tabIndex={selected ? 0 : -1}
                className="ink-swatch"
                data-ink={color.id}
                onClick={() => !disabled && setInk(color.id)}
                onKeyDown={(event) => !disabled && pickByKey(event, index)}
              >
                <span className="ink-dot" style={{ background: color.value }} aria-hidden="true" />
                <span className="ink-name">{color.label}</span>
                {selected ? (
                  <span className="ink-check" aria-hidden="true">
                    ✓
                  </span>
                ) : null}
              </button>
            );
          })}
        </div>
        <div className="shot-actions">
          <button type="button"
            disabled={disabled || !canUndo(drawing)}
            onClick={() => {
              const restoring = !hasMarks(drawing) && drawing.cleared !== null;
              draft.undo();
              setNote(restoring ? "restored" : "undone");
            }}
          >
            {EDITOR_COPY.undo}
          </button>
          <button type="button"
            disabled={disabled || !hasMarks(drawing)}
            onClick={() => {
              draft.clear();
              setNote("cleared");
            }}
          >
            {EDITOR_COPY.clear}
          </button>
        </div>
      </div>
      <p className="tiny muted shot-status">{status}</p>
      <p className="sr-only" role="status" aria-live="polite">
        {announcement}
      </p>
      <div className="shot-preview">
        {url ? (
          <img
            className="shot-image"
            src={url}
            alt=""
            draggable={false}
            onLoad={(event) =>
              setLoaded({
                url: url!,
                size: {
                  width: event.currentTarget.naturalWidth,
                  height: event.currentTarget.naturalHeight,
                },
              })
            }
          />
        ) : null}
        <canvas
          ref={canvasRef}
          className="shot-canvas"
          width={natural?.width ?? 1}
          height={natural?.height ?? 1}
          role="img"
          aria-label={EDITOR_COPY.canvasLabel}
          tabIndex={-1}
          onPointerDown={down}
          onPointerMove={move}
          onPointerUp={(event) => lift(event, true)}
          onPointerCancel={(event) => lift(event, false)}
        />
      </div>
    </div>
  );
}
