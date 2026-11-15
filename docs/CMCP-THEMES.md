# CMCP cell themes: design note

Status: **implemented**; this is the design it was built from, kept for the reasoning, and the code (`internal/cmcpui`: `theme.go`,
`frame.go`, `cellframe.go`) is authoritative where the two differ. `tng` has a hand-built frame; the other themes are drawn by the
interpreter from the shared core's reference theme packs (their `cell` rendition). With nothing chosen or suggested the theme is `tng`;
`plain` is the no-frills choice. `/cmcp themes` shows what is embedded and active. Written after studying the hand-built `/lcars` (gtflib/gtf/examples/lcars.tf, drawn with `/canvas` rounded
elbows and pill buttons plus the `{radar}` widget) and SquatClient's themes (`theme_*.lua`, `docs/gfx-shots/themes/*.png`).
The current CMCP screens in gtf are generic: text widgets on a flat background. They lose to both, for one reason: **they draw
data, not a console**. A console has a frame, a palette, shaped controls and pictures; the layout follows from the frame.

## 1. What the references do that gtf's CMCP screens do not

| | `/lcars` (gtf) | SquatClient themes | CMCP renderer today |
|---|---|---|---|
| colour | full 24-bit: gradients, glow, soft lights | per-theme palette tokens, bloom | flat token colours, a few 24-bit pictures |
| frame | rounded elbows, coloured pill bars with the title in them | per-empire frame art (LCARS elbows, jagged plates, sweeping curves, double filigree frame, riveted grey bezel) | none (a header row) |
| controls | pill buttons, filled, big | per-theme nav and buttons (skewed slabs, rounded pills, chunky primaries) | `[ TEXT ]` in the accent colour |
| radar | the `{radar}` dish: Braille rings, fading sweep, glowing blips | dish, hexagonal tactical plot, deflector rings: **a different radar per empire** | one pixel dish for all |
| arrangement | viewscreen top left, radar right, readouts below, output at the bottom | each empire arranges the same widgets differently | the pack's arrangement, restacked |

Constraint carried over from the spec and the owner: the game's text pane is the main thing. It keeps the whole left side (or at
least a quarter of the height and 8 rows), the input line is never covered, and everything is keyboard-operable.

## 2. Reuse the quality widgets inside the CMCP renderer

`internal/layout` already has the good drawing code (`{radar}` with sweep and blips, `{scope}`, gradient `{bar}` with
`gradient=#from,#to`, `{gauge}`, `{canvas}` with `rrect`/`arc`/`glow`/`grad`, `{sprite}`). It is template- and TF-variable-driven,
not data-driven. The plan is to lift the **drawing functions**, not the template language:

* Move `radar`, `scope`, gradient bar, arc and rounded-rect drawing out of `internal/layout` into plain functions that take values
  (`DrawRadar(canvas, rect, []Contact{az, range, kind, selected}, sweep, palette)`), leaving the template widgets as thin callers.
  `internal/cmcpui` imports `internal/layout` already (arch table allows it), so this is a refactor of one package, no new
  dependency.
* `cmcpui`'s `bar`, `gauge`, `radar`, `viewscreen` call those functions with the active theme's palette. The pixel pictures I added
  experimentally (gfx.go, pics.go) become the shared half-block canvas (`layout.Canvas`) so there is one pixel surface in gtf.
* Contacts and zones come from the bound props, exactly as now; nothing about the protocol changes.

## 3. A cell theme is a package of five things

```
type CellTheme struct {
    Name    string
    Palette Palette          // tokens: bg, panel, text, dim, accent..accent3, ok, warn, alert, plus ramps (gradients)
    Frame   FrameStyle       // how the screen is framed: elbows, plates, double frame, bezel; header and footer strips
    Control ControlStyle     // button, toggle, slider, bar, gauge shapes and fills
    Radar   RadarStyle       // dish | hex | rings | blade: the sensor picture differs per empire
    Arrange Arrangement      // default panel arrangement when the pack gives no layout.cell hints
}
```

Themes are Go data plus drawing functions in `internal/cmcpui/theme_*.go`; the palette tokens stay the protocol's (`color.*`), so a
pack's `@token` colours keep working and `suggest_theme` / "use the ship's theme" (spec 6.6) select a theme by the same names as
SquatClient (`tng`, `klingon`, `romulan`, `ferengi`, `tos`, `soft`, `ops`; `plain` stays the no-frills fallback for 16-colour
terminals).

### 3.1 Per-empire looks, in characters

All drawn with block, quadrant, half-block, box and wedge characters (`▀▄█▌▐▘▝▖▗▛▜▙▟ ◢◣◤◥ ◖◗ ▔▁ ╭╮╰╯ ═║`) and 24-bit foreground and
background pairs; gradients come free because each cell carries two colours (two vertical pixels).

* **tng (LCARS).** Rounded elbow at the top left and bottom left (a quarter disc in `#ffcc33`, built from `◢`/`◖` and filled blocks),
  coloured pill bars along the top and bottom carrying title, stardate and alert text on bars of orange, violet, blue and pink,
  a left column of stacked pill buttons in different colours (the console switch, which is only a visual of `Cobra.Console`, not
  a command). Controls: filled pills with dark bold text, rounded `◖ ◗` caps. Bars: gradient fill in a rounded track. Radar: the
  `/lcars` dish (rings, sweep trail, glowing blips) in a gold ring.
* **klingon.** Angular. Top and bottom plates with a **jagged edge** (`◣◢` teeth alternating with `▀▄`), a three-spike blade motif in
  dull gold at the top left, red outline panels, skewed buttons (`◢ SCAN ◤`). Black ground, blood red `#c01020`, gold `#d4b86a`.
  Radar: hexagonal rings, a turning blade, contacts as diamonds (`◆`); no dish. Damage "scratches" flicker on the frame at low rate
  (honouring reduced motion).
* **romulan.** Sweeping curves: a thin green double line that bends around the top left corner (`╭` and `─` runs with a second,
  dimmer line), angular amber nameplate top right, slanted buttons in green and amber, rounded pill action buttons. Radar:
  concentric deflector rings in segments (`▀▄▌▐` arcs). Greens `#40c070`, amber `#f0b040` on near-black.
* **ferengi.** Ornate: a **double frame**, outer gold, inner bronze, filigree sweeps in the four corners, a band of glyph-like
  characters along an edge, glyph meters instead of bars (`▁▂▃▄▅▆▇` ladders). Palette gold `#e0b020`, maroon, bronze. Radar: a
  small round dish inside an ornate viewscreen frame.
* **tos.** Grey panels with a rivet row (`•`), chunky primary-colour buttons (red, yellow, blue) as filled rectangles with bevels
  (`▀` highlight row, `▄` shadow row), analogue dials built from `◜◝◟◞` plus a needle glyph, lamp rows. Radar: a plain scope.
* **soft.** Muted: rounded boxes (`╭╮╰╯`), low-contrast pastel palette, no glow; the quiet theme for long sessions.
* **ops.** Dense wall: small panels in a grid, thin sharp frame, numbers over decoration, everything visible at once; this is also
  the natural theme for 80x24.
* **plain.** No frame, terminal colours, today's look; always available and the default (spec: the player's own choice wins).

Every theme draws its frame **outside** the widget rectangles it is given (it owns row 0, the outer ring and the strips) and never
inside the text pane rectangle.

## 4. How the arrangement per empire is driven

Three layers, in the order spec 6.13 gives:

1. **The pack's `layout.cell` hints** (order, hidden, dir, weight, size per node) are the primary source. The packs agent authors them
   per empire so the klingon `tactical` screen really is a different arrangement from the federation one (sensor plot top right,
   weapon bars below, output left), not just a different colour. cmcpui already receives them through the core
   (`pack.Caps{Class:"cell"}` applies them in `Bind`).
2. **The theme's default arrangement** for screens whose pack gives no hints (the built-in `std.*` screens): a small table
   `Arrange` per theme, keyed by screen id: *pictures* (viewscreen, radar/plot, schematic) go in the theme's picture slots, *data*
   (tables, bars) in the data slots, *controls* in the control strip. The slots are defined by the frame (e.g. LCARS: picture area
   top left, dish top right, readouts under them, buttons in a strip above the output), so every theme is arranged around its own
   frame instead of one generic column.
3. **Automatic reflow** (today's code) as the last step: stack rows that do not fit, wrap button rows, scroll lists, keep the text
   pane floor.

Where the picture goes is a layout concern of the theme, so the **text pane rectangle** is computed by the theme's frame: LCARS
and ops give the output the whole left side with the pictures on the right; ferengi and klingon, whose frames are heavy, give it a
wide band under the viewscreen. In every theme it is at least max(8, h/4) rows and 30 columns, otherwise the screen steps aside.

## 5. Spec rules kept

* **Keyboard first** (6.12): the shared `focus` model is unchanged; themes only change how the focused control is drawn (reverse
  video plus a bright outline in the theme's accent). Mnemonic letters stay underlined in the label (`[A]CK`). A theme's
  decorative nav buttons are not focusable: they are drawing, not controls.
* **The text pane and input line are never covered or imitated** (10.2 rule 7): frames are drawn in a ring around widget rectangles;
  a drawn "input box" is not allowed; the status line and input line stay the UI's.
* **Colour depth**: all drawing is 24-bit and quantised by the UI's `Depth`; on 16-colour terminals a theme falls back to its
  `plain`-style palette and keeps the arrangement (frames degrade to line-drawing, gradients to two-colour bars). `%cmcp_ascii`
  stays: every glyph has an ASCII twin (`#`, `+`, `-`, `<>`).
* **Motion** is slow (sweep, flicker) and off with `%cmcp_motion off` / reduced-motion.
* **Performance**: panels re-render on data change only, as now; the pixel pictures are cached per rect and quantised data.

## 6. Plan, once approved (nothing here is started)

1. Extract the drawing primitives from `internal/layout` into value-driven functions; point cmcpui at them (radar, bars, gauge, arcs).
2. `CellTheme` plumbing and the frame/arrangement hook in `Draw`; `plain` first (no visible change), then **tng** as the reference theme
   against the `/lcars` PNGs.
3. klingon, romulan, ferengi, tos, soft, ops, one at a time, each with goldens and PNGs checked against SquatClient's
   `docs/gfx-shots/themes/*.png`.
4. Ask the packs agent for per-empire `layout.cell` hints on the six console screens plus transporter; agree the slot names
   (`picture`, `plot`, `data`, `controls`, `log`) so hints and theme slots line up.
5. Tests: goldens per theme and screen at 120x40 and 80x24, keyboard-only walk per theme, text-pane floor per theme.

## 7. Open questions for the owner

* Should the console-switch column (SCAN / POWER / WEAPONS ...) be real (a click or key = the `show` action / `man`) or only
  decoration? Today a screen has no such switch; `/cmcp screen` is the only way.
* One default theme when the ship suggests none: `plain` (owner's choice wins) or `tng`? (Decided in the code: `tng`; `%cmcp_theme plain` opts out.)
* Is it acceptable for the std screens (no pack hints) to be arranged by the theme rather than by the pack tree?
