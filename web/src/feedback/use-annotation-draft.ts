"use client";

import { useCallback, useMemo, useState } from "react";
import {
  EMPTY_DRAWING,
  beginStroke,
  cancelStroke,
  clearDrawing,
  endStroke,
  extendStroke,
  undo,
  type Drawing,
  type InkColorId,
  type Point,
} from "./annotation-model";

// The marks on a sheet's screenshot, held in that sheet's own React state and nowhere else (#266): not in a module variable, not in
// storage, not in the outbox until Send. They belong to ONE account: when the account (the outbox scope) changes, or the sheet
// goes away (cancel, sign-out, a second sheet), they are gone, so another account's sheet can never show or attach them.
export interface AnnotationDraft {
  drawing: Drawing;
  begin(ink: { color: InkColorId; width: number; unit: number }, point: Point): void;
  extend(point: Point): void;
  end(): void;
  cancel(): void;
  undo(): void;
  clear(): void;
  /** Drops every mark now (the sheet is closing or its screenshot was un-ticked). */
  discard(): void;
}

export function useAnnotationDraft(scope: string | null): AnnotationDraft {
  const [held, setHeld] = useState<{ scope: string | null; drawing: Drawing }>({
    scope,
    drawing: EMPTY_DRAWING,
  });
  // A new scope means another account (or none): what was held is dropped for good, not just hidden, so going back to the first
  // account does not bring its marks back. (Resetting state during render is React's way to derive state from a changed prop.)
  if (held.scope !== scope) setHeld({ scope, drawing: EMPTY_DRAWING });
  const drawing = held.scope === scope ? held.drawing : EMPTY_DRAWING;

  const change = useCallback(
    (step: (current: Drawing) => Drawing) =>
      // A late call from an earlier account's render (a pointer event still in flight when the account changed) is dropped: it
      // must not put its mark into the new account's sheet.
      setHeld((now) => (now.scope === scope ? { scope, drawing: step(now.drawing) } : now)),
    [scope],
  );

  return useMemo(
    () => ({
      drawing,
      begin: (ink, point) => change((current) => beginStroke(current, ink, point)),
      extend: (point) => change((current) => extendStroke(current, point)),
      end: () => change(endStroke),
      cancel: () => change(cancelStroke),
      undo: () => change(undo),
      clear: () => change(clearDrawing),
      discard: () => setHeld({ scope, drawing: EMPTY_DRAWING }),
    }),
    [drawing, change, scope],
  );
}
