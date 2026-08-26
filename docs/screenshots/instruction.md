# Recreating the dashboard screenshot

How to capture `docs/screenshots/dashboard.webp` from the live deployment at
`https://dash.antonshubin.com/` for use as the README hero image. Repeat when the
UI changes, a new feature needs to be visible, or the gatus data stabilises
enough that the HTML modification step can be dropped.

## Prerequisites

**Noto Color Emoji font** — headless Chromium renders service-icon emojis as
tofu rectangles without it. One-time setup:

```bash
mkdir -p ~/.fonts
curl -sSL -o ~/.fonts/NotoColorEmoji.ttf \
  "https://github.com/googlefonts/noto-emoji/raw/main/fonts/NotoColorEmoji.ttf"
fc-cache -fv ~/.fonts
```

Other tools already on this host: Python 3 with Pillow, Playwright MCP browser,
ImageMagick (`magick`).

## Step 1 — desktop capture

In the Playwright browser:

1. Navigate to `https://dash.antonshubin.com/`.
2. `resize(1440, 900)`.
3. Wait for fonts + paint settle:
   ```js
   await document.fonts.ready;
   await new Promise(r => setTimeout(r, 600));
   ```
4. **Modify the DOM** (see "Uptime distribution" below) — the deployed gatus is
   fresh and shows uniform ~46 % which reads as a broken dashboard. The
   modification fakes a realistic 80 / 15 / 5 distribution.
5. Take a **full-page** screenshot, save as `docs/screenshots/_full-desktop.png`.
6. Crop the top **1080 px** with Pillow (4 clean rows of service cards, no row
   clipping). This becomes the desktop panel of the composite.

## Step 2 — mobile capture

The same browser tab, no reload:

1. `resize(390, 844)`.
2. Re-run the uptime-modification script — DOM mutation does not survive resize
   triggers.
3. Full-page screenshot, crop top **940 px** (4 clean rows × 2 cards).

## Step 3 — composite

`docs/screenshots/dashboard.webp` is a single Pillow-produced image. Targets:

| Property        | Value                                |
| --------------- | ------------------------------------ |
| Canvas size     | 2030 × 1216 px                       |
| Background      | `#0a0a0a`                            |
| Border          | 3 px `#f0f0f0`, 6 px inner padding   |
| Vertical divider| 2 px white, in the gap between panels|
| Labels          | Open Sans Semibold 22 px, `#d2d2d2`  |
| Canvas padding  | 40 px outer, 96 px gap between panels|
| WebP            | `quality=70, method=6` (~78 KB)      |

Drop the desktop and mobile crops into the layout (desktop left, mobile right,
both vertically centered on the canvas), draw the borders + divider + labels,
export.

## Uptime distribution

Apply in DOM order to `.service-uptime` elements (36 on the deployed page).
Update each pill text + class, then re-derive the per-section `.section-uptime`
to show max(uptime) of that section.

```js
const seq = [
  [96.10,'good'],[98.20,'good'],[94.86,'transition'],[94.03,'transition'],
  [95.03,'good'],[95.46,'good'],[96.70,'good'],[96.68,'good'],
  [97.72,'good'],[99.24,'good'],[93.01,'transition'],[95.13,'good'],
  [95.99,'good'],[96.09,'good'],[97.95,'good'],[67.60,'bad'],
  [99.46,'good'],[99.05,'good'],[98.49,'good'],[98.38,'good'],
  [97.53,'good'],[99.03,'good'],[93.64,'transition'],[97.11,'good'],
  [58.92,'bad'],[96.12,'good'],[95.43,'good'],[95.48,'good'],
  [92.68,'transition'],[95.15,'good'],[99.79,'good'],[98.25,'good'],
  [96.38,'good'],[98.68,'good'],[95.13,'good'],[95.78,'good'],
];
document.querySelectorAll('.service-uptime').forEach((p, i) => {
  if (i >= seq.length) return;
  const [pct, cls] = seq[i];
  p.textContent = pct.toFixed(2) + '% \u2197';
  p.className = 'service-uptime service-uptime--' + cls;
});
document.querySelectorAll('.section').forEach(sec => {
  const uptimes = [...sec.querySelectorAll('.service-uptime')]
    .map(el => parseFloat(el.textContent.match(/([0-9.]+)%/)?.[1] ?? 0))
    .filter(x => x);
  if (!uptimes.length) return;
  const max = Math.max(...uptimes);
  const cls = max >= 95 ? 'good' : max >= 90 ? 'transition' : max >= 80 ? 'warn' : 'bad';
  const pill = sec.querySelector('.pill.pill--good, .pill.pill--transition, .pill.pill--warn, .pill.pill--bad');
  if (pill) {
    pill.textContent = max.toFixed(2) + '% \u00b7 30d';
    pill.className = 'pill pill--' + cls;
  }
});
```

Distribution intent: 80 % services in 95–100 % (`good`, green), 15 % in 90–95 %
(`transition`, yellow-green gradient), 5 % in 40–90 % (`bad`, red). Exact tally
in the committed sequence: 29 good / 5 transition / 2 bad out of 36 total.

## Notes

- The HTML modification is **screenshot-only** — the deployed page is not
  changed and no production code is affected.
- The CSS class suffixes `good` / `transition` / `warn` / `bad` and the
  `--uptime-{good,warn,bad}-*` colour variables come from
  [PR #3](https://github.com/spy4x/oko/pull/3), which landed the SLA-tier colour
  coding. Once gatus has accumulated 30+ days of data, the DOM modification
  step can likely be dropped entirely — the screenshot would then reflect the
  real dashboard state.
- Source PNGs (`_full-desktop.png`, `_full-mobile.png`) are intentionally not
  tracked — only the final `dashboard.webp` is committed.
- README insertion: one line under the ASCII flow diagram,
  `![Oko dashboard — desktop and mobile views](docs/screenshots/dashboard.webp)`.
