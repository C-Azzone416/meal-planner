# Automatic feedback capture parity

Source: Packed Smart `origin/develop` c1a381e (read November 15, 2026).

The Meal Planner feature now uses the same flow: invitation connection in settings,
connected-only feedback entry, capture before opening the sheet, screenshot included
by default when available, preview plus four-color drawing/Undo/Clear, optional
text-only send, and compositing only after explicit Send. Existing Meal Planner
BFF enrollment, account-scoped durable outbox, verified complete-media receipt and
retry/reload protocol are unchanged. No manual attachment or native screen access.

Ported annotation model, compositor, final-image decision, draft hook, editor and
focused tests from Packed Smart. Button imports/CSS variables were adapted to the
Meal React/Vite shell. Object URLs are allocated per effect lifetime, not useMemo,
so React StrictMode cannot reuse a revoked URL in WebKit.

Capture matches Packed's documentElement/viewport, negative scroll margins,
pre-clone exclusion filter and pre/post readiness. It keeps actual cards, buttons,
nav and inert inline SVG. Capture attempts pixel ratios 1, .5, .25 within 8 MiB;
final reviewed image is limited to **4,000,000 bytes** on client and server, never
silently dropping annotations. Multipart body maximum is 4,020,480 bytes, below
Vercel's 4.5 MB request boundary.

App-specific safety adaptations:
- Known Meal routes including recipe forms/details and House rules are eligible;
  sign-in/auth and unknown routes are not. Metadata stores coarse screen codes,
  not recipe IDs or URLs.
- Existing `data-feedback-private`/`data-feedback-ui` regions are excluded, as are
  Packed's exclude/identity/sensitive-portal markers and password/email fields.
  House rules feedback connection (including invitation) is a private region.
  The current shell does not show account identity; any future identity region
  must be explicitly marked and excluded. Sensitive portal presence fails closed.
- Head/scripts/stylesheets, external media, embedded documents and URL-bearing
  CSS/pseudo-elements are not cloned. Fonts are not fetched/embedded. The existing
  app uses CSS/text/inert inline SVG rather than remote image content, so this
  preserves its real layout while avoiding new capture-time network requests.
  Unknown external resources are omitted, not downloaded with ambient credentials.
- Existing server decoded bounds remain 4096 per side / 12 million pixels. Captures
  above these dimensions and pinch-zoomed viewports return notes-only, rather than
  allocating an unbounded bitmap. These are explicit differences from Packed's
  larger generic compositor budget.
- Route/session/scope changes discard in-flight capture. Private data is excluded
  before cloning, not blurred after capture. This is an audited app DOM contract,
  not protection against hostile same-origin JavaScript.

Validation uses only local synthetic auth/database/Framebriar fixtures. Browser
checks compare real UI region pixels with capture pixels, verify excluded canaries,
inspect actual outgoing PNG annotation pixels, and exercise metadata-received /
image-502 or invalid-receipt recovery across close/reload. This is not evidence of
live Framebriar delivery or permission to activate the integration.

Build boundary: `feedback-images` resolves to a type-only disabled stub whenever
screenshots are disabled, alongside `feedback-capture`. Editor/compositor/drawing
implementation is absent from those artifacts; the artifact matrix requires
both active markers only for explicitly capture-enabled builds.
