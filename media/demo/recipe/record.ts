/**
 * Records the oko demo from a staged dashboard and encodes it (encode.ts):
 *
 *   deno task record [--warmup 600]       (from media/demo/recipe/, or `make demo-video`)
 *
 * It builds oko from this checkout and starts it with gatus and a few nginx "services" on a
 * Docker network of their own, with the example.com catalog in `catalog.json`. gatus checks them
 * over `--warmup` seconds, so the 30-day uptime pills hold real numbers: the Git service is
 * stopped for 30 s on the way, and Email for the last 75 s, so it shows as down. File sync has no
 * gatus endpoint, so it shows as unknown. Then it records the browser (dashboard, reload, search,
 * hover), and in between `vhs` takes the editor's states while `edit.tape` pastes a service into
 * the config.json oko serves. Every frame is a screenshot with a fixed time on screen (`Take`), so
 * the pace and the caption times are the same on every run, however busy the machine. It writes
 * the raw take `media/demo/.raw/raw.mkv` with `poster.png` beside it, and
 * `media/demo/captions.vtt` from the captions below, then runs the encoder. Containers, image,
 * network, browser and the work directory are removed on success, failure and Ctrl-C.
 *
 * Needs Docker, vhs, ffmpeg, openssl, nano and Playwright's Chromium.
 */
import { join, resolve } from "@std/path"
import { chromium, type Page } from "playwright"
import { BAND, type Cue, DEMO, encode, H, RAW, TITLE_S, toVtt, W } from "./encode.ts"

const HERE = resolve(new URL(".", import.meta.url).pathname)
const ROOT = resolve(DEMO, "../..")
/** Pinned by digest, so a new release cannot change the clip unnoticed. */
const IMAGES = {
  gatus:
    "twinproduction/gatus@sha256:215787ef82f7bd16c3b8e9055630b66f938914c8ff764ef9508e7d1d4d550bf0",
  nginx: "nginx@sha256:892bb2152464f0c5d8972593a5f5afaf8054552580b699d95f8ff4af08c2d33d",
}
const FPS = 30
/** The page zoom: card names stay readable when the clip plays about 528 px wide. */
const ZOOM = 1.5
/** The editor's states, as edit.tape screenshots them, with their time on screen and captions. */
const EDITOR: { file: string; seconds: number; caption?: string }[] = [
  { file: "edit-1.png", seconds: 2.8, caption: "To add a service, open its config.json" },
  { file: "edit-2.png", seconds: 0.8, caption: "Paste the new service in" },
  { file: "edit-3.png", seconds: 2.4 },
  { file: "edit-4.png", seconds: 0.4 },
  { file: "edit-5.png", seconds: 0.8, caption: "Save the file. No restart, no database" },
  { file: "edit-6.png", seconds: 2.5 },
]
/** The terminal's background in edit.tape, which fills the band under the editor. */
const TERMINAL_BG = "0x15120f"

/** Runs a command, throws with its output when it fails. */
async function run(cmd: string, args: string[], opts: { cwd?: string; timeoutMs?: number } = {}) {
  const out = await new Deno.Command(cmd, {
    args,
    cwd: opts.cwd,
    stdout: "piped",
    stderr: "piped",
    signal: AbortSignal.timeout(opts.timeoutMs ?? 120_000),
  }).output()
  const text = new TextDecoder().decode(out.stdout) + new TextDecoder().decode(out.stderr)
  if (!out.success) throw new Error(`${cmd} ${args.join(" ")} failed:\n${text.slice(-3000)}`)
  return text.trim()
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
const log = (msg: string) => console.log(`[${new Date().toISOString().slice(11, 19)}] ${msg}`)

/** A throwaway CA and a certificate for uptime.example.com: oko talks to gatus over HTTPS only. */
async function makeCerts(dir: string) {
  await Deno.mkdir(dir)
  const ssl = (...a: string[]) => run("openssl", a, { cwd: dir })
  const ec = ["-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes"]
  await ssl(
    ...["req", "-x509", ...ec, "-keyout", "ca-key.pem", "-out", "ca.pem", "-days", "2"],
    ...["-subj", "/CN=oko demo CA"],
  )
  await ssl(
    ...["req", ...ec, "-keyout", "uptime-key.pem", "-out", "uptime.csr"],
    ...["-subj", "/CN=uptime.example.com"],
  )
  await Deno.writeTextFile(join(dir, "san.ext"), "subjectAltName=DNS:uptime.example.com\n")
  await ssl(
    ...["x509", "-req", "-in", "uptime.csr", "-CA", "ca.pem", "-CAkey", "ca-key.pem"],
    ...["-CAcreateserial", "-days", "2", "-out", "uptime.pem", "-extfile", "san.ext"],
  )
  for (const f of ["ca.pem", "uptime.pem", "uptime-key.pem"]) await Deno.chmod(join(dir, f), 0o644)
}

/** Builds oko from this checkout and starts the staged services; returns oko's local URL. */
async function stage(net: string, image: string, work: string): Promise<string> {
  const docker = (...a: string[]) => run("docker", a, { timeoutMs: 600_000 })
  log("building oko from this checkout")
  await docker("build", "-q", "-t", image, "-f", join(ROOT, "Dockerfile"), ROOT)
  await docker("network", "create", net)
  const run_ = (name: string, ...a: string[]) =>
    docker("run", "-d", "--name", `${net}-${name}`, "--network", net, ...a)
  const alias = (...hosts: string[]) =>
    hosts.flatMap((h) => ["--network-alias", `${h}.example.com`])
  await run_(
    "web",
    ...alias("books", "photos", "links", "notes", "passwords", "cal", "speed"),
    IMAGES.nginx,
  )
  await run_("git", ...alias("git"), IMAGES.nginx)
  await run_("mail", ...alias("mail"), IMAGES.nginx)
  await run_(
    "gatus",
    ...alias("uptime"),
    ...["-v", `${join(work, "gatus.yaml")}:/config/config.yaml:ro,z`],
    ...["-v", `${join(work, "certs")}:/certs:ro,z`],
    IMAGES.gatus,
  )
  await run_(
    "oko",
    ...[
      "-p",
      "127.0.0.1::8080",
      "-e",
      "DOMAIN=example.com",
      "-e",
      "UPTIME_HOSTS=uptime.example.com",
    ],
    ...["-e", "CONFIG_PATH=/config/config.json", "-e", "SSL_CERT_FILE=/certs/ca.pem"],
    ...["-v", `${join(work, "stage")}:/config:ro,z`, "-v", `${join(work, "certs")}:/certs:ro,z`],
    image,
  )
  const port = (await docker("port", `${net}-oko`, "8080/tcp")).split("\n")[0].split(":").pop()
  const url = `http://127.0.0.1:${port}/`
  const deadline = Date.now() + 30_000
  while (true) {
    try {
      if ((await fetch(url)).ok) return url
    } catch { /* not listening yet */ }
    if (Date.now() > deadline) throw new Error(`oko did not answer at ${url} within 30 s`)
    await sleep(500)
  }
}

/** Lets gatus build history: a short Git outage, then Email goes down. */
async function warmUp(net: string, seconds: number) {
  const docker = (...a: string[]) => run("docker", a)
  const t0 = Date.now()
  const at = (s: number) => sleep(Math.max(0, t0 + s * 1000 - Date.now()))
  log(`warming up gatus for ${seconds} s`)
  await at(seconds * 0.3)
  log("Git service down for 30 s")
  await docker("stop", "-t", "1", `${net}-git`)
  await at(seconds * 0.3 + 30)
  await docker("start", `${net}-git`)
  await at(seconds - 75)
  log("Email down from here on")
  await docker("stop", "-t", "1", `${net}-mail`)
  await at(seconds)
}

/** One frame of the raw take: an image and how long it stays on screen. */
interface Frame {
  file: string
  seconds: number
}

/**
 * The raw take, built frame by frame on a clock of its own: each frame is a screenshot of the
 * settled page with the time it stays on screen. A loaded machine makes the recording slower,
 * never the video choppier, and every caption lands at the same time on every run.
 */
class Take {
  readonly frames: Frame[] = []
  readonly cues: Cue[] = []
  /** The take's clock, in seconds. */
  t = 0
  private open: { text: string; start: number } | null = null

  constructor(readonly dir: string) {}

  /** The path of the next frame's image. */
  next(): string {
    return join(this.dir, `${String(this.frames.length).padStart(6, "0")}.png`)
  }

  /** Adds an image that stays for `seconds`. */
  add(file: string, seconds: number) {
    this.frames.push({ file, seconds })
    this.t += seconds
  }

  /** Screenshots the page as it is now; the frame stays for `seconds`. */
  async shot(page: Page, seconds: number) {
    const file = this.next()
    await page.screenshot({ path: file })
    this.add(file, seconds)
  }

  /** Ends the open caption and starts `text` (or none, for null) at the take's clock. */
  say(text: string | null) {
    if (this.open) this.cues.push({ ...this.open, end: this.t })
    this.open = text ? { text, start: this.t } : null
  }

  /** The concat list that plays every frame for its time. */
  list(): string {
    const lines = this.frames.flatMap((f) => [`file '${f.file}'`, `duration ${f.seconds}`])
    // The demuxer ignores the last duration unless the file is listed once more.
    lines.push(`file '${this.frames.at(-1)!.file}'`)
    return lines.join("\n") + "\n"
  }
}

interface Point {
  x: number
  y: number
}

const ease = (t: number) => t < 0.5 ? 4 * t ** 3 : 1 - (-2 * t + 2) ** 3 / 2

/**
 * Glides the mouse from `from` to `to` along a slight curve with ease-in-out, over 0.4–0.7 s of
 * the take by distance, one frame per 1/30 s: the `demo-video` skill's `moveLike`, timed by the
 * take's clock instead of the wall clock. `from` is updated in place.
 */
async function glide(page: Page, take: Take, from: Point, to: Point) {
  const dx = to.x - from.x
  const dy = to.y - from.y
  const dist = Math.hypot(dx, dy)
  const steps = Math.round(Math.min(0.7, Math.max(0.4, dist * 0.0008)) * FPS)
  const bow = Math.min(40, dist * 0.08)
  const nx = dist ? -dy / dist : 0
  const ny = dist ? dx / dist : 0
  for (let i = 1; i <= steps; i++) {
    const e = ease(i / steps)
    const arc = Math.sin((Math.PI * i) / steps) * bow
    await page.mouse.move(from.x + dx * e + nx * arc, from.y + dy * e + ny * arc)
    await take.shot(page, 1 / FPS)
  }
  from.x = to.x
  from.y = to.y
}

/**
 * Presses `key` (or, with `label`, only shows the keycap of a key the page never receives),
 * then holds for `seconds` of the take: the keycap for up to 0.8 s, the rest without it.
 */
async function press(page: Page, take: Take, seconds: number, key: string, label?: string) {
  if (label) {
    await page.evaluate(
      (l) => (globalThis as unknown as { __demoKey(k: string): void }).__demoKey(l),
      label,
    )
  } else {
    await page.keyboard.press(key)
  }
  await page.waitForTimeout(250)
  const withCap = Math.min(seconds, 0.8)
  await take.shot(page, withCap)
  if (seconds > withCap) {
    await page.waitForTimeout(1000)
    await take.shot(page, seconds - withCap)
  }
}

/** Records every shot into `take`; `edit` records the editor's states while the browser waits. */
async function recordShots(url: string, take: Take, edit: () => Promise<string>) {
  const browser = await chromium.launch()
  try {
    const context = await browser.newContext({
      viewport: { width: W, height: H },
      colorScheme: "dark",
      // Hover borders change at once, so a frame never catches one halfway.
      reducedMotion: "reduce",
      timezoneId: "UTC",
      locale: "en-US",
    })
    // Bigger page, so card names stay readable where the clip plays about 528 px wide.
    await context.addInitScript(`addEventListener("DOMContentLoaded", () =>
      document.body.style.zoom = "${ZOOM}")`)
    const pointer = { x: 1060, y: 150 }
    await context.addInitScript(`window.__demoPointerStart = ${JSON.stringify(pointer)}`)
    await context.addInitScript({ path: join(HERE, "pointer.js") })
    await context.addInitScript({ path: join(HERE, "keys.js") })
    const page = await context.newPage()
    const settle = async () => {
      await page.evaluate(() => document.fonts.ready.then(() => true))
      await page.waitForTimeout(300)
    }
    await page.goto(url, { waitUntil: "networkidle" })
    await settle()
    await page.mouse.move(pointer.x, pointer.y)
    await page.screenshot({
      path: join(RAW, "poster.png"),
      style: "#demo-pointer, #demo-key { display: none !important }",
    })
    const card = async (name: string): Promise<Point> => {
      const box = await page.locator(".service")
        .filter({ has: page.getByText(name, { exact: true }) }).first().boundingBox()
      if (!box) throw new Error(`no card ${name}`)
      return { x: Math.round(box.x + box.width * 0.55), y: Math.round(box.y + box.height * 0.45) }
    }
    /** Scrolls, one frame per 1/30 s with ease-in-out, so `y` (page px) is the top of the view. */
    const scrollTo = async (y: number) => {
      const from = await page.evaluate(() => scrollY)
      const to = await page.evaluate(
        (y) => Math.max(0, Math.min(y, document.documentElement.scrollHeight - innerHeight)),
        y,
      )
      if (Math.abs(to - from) < 2) return
      const steps = Math.round(0.7 * FPS)
      for (let i = 1; i <= steps; i++) {
        await page.evaluate(
          (y) => globalThis.scrollTo(0, y),
          Math.round(from + (to - from) * ease(i / steps)),
        )
        await take.shot(page, 1 / FPS)
      }
    }
    /** Scrolls so the Cloud section's heading sits near the top of the view. */
    const toCloud = async () => {
      const box = await page.locator(".section").nth(1).boundingBox()
      if (!box) throw new Error("no Cloud section")
      await scrollTo((await page.evaluate(() => scrollY)) + box.y - 24)
      const cards = await page.locator(".section").nth(1).boundingBox()
      if (cards && cards.y + cards.height > H - BAND) {
        throw new Error(
          `the Cloud section ends at ${
            Math.round(cards.y + cards.height)
          } px, under the caption band`,
        )
      }
    }

    // 1. The dashboard, then the Cloud section's counter.
    take.say("Every service on one page, with its 30-day uptime")
    await take.shot(page, 3.4)
    take.say("Each server counts what is healthy, down or unknown")
    await toCloud()
    const counter = await page.locator(".section").nth(1).locator(".pill").first().boundingBox()
    if (!counter) throw new Error("no section counter")
    await glide(page, take, pointer, {
      x: Math.round(counter.x + counter.width / 2),
      y: Math.round(counter.y + counter.height + 14),
    })
    await take.shot(page, 2.8)

    // 2. The editor's states (vhs), while the browser waits.
    const states = await edit()
    for (const state of EDITOR) {
      if (state.caption) take.say(state.caption)
      const file = take.next()
      await run("ffmpeg", [
        ...["-y", "-v", "error", "-i", join(states, state.file)],
        ...["-vf", `pad=${W}:${H}:0:0:color=${TERMINAL_BG},format=rgb24`, file],
      ])
      take.add(file, state.seconds)
    }

    // 3. Reload: the new card is there, with its pill.
    take.say("Reload the page: the new card is already there")
    await press(page, take, 0.8, "F5", "F5")
    await page.reload({ waitUntil: "networkidle" })
    await settle()
    await take.shot(page, 0.7)
    await toCloud()
    await glide(page, take, pointer, await card("Speed test"))
    await take.shot(page, 2.4)

    // 4. Search: `/` focuses the box (oko's own autofocus is best effort), typing filters.
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
    take.say("Press / and type to filter by name, product or description")
    await scrollTo(0)
    await take.shot(page, 0.3)
    await press(page, take, 0.6, "/")
    await press(page, take, 0.24, "p")
    await press(page, take, 0.24, "h")
    await press(page, take, 2.2, "o")
    const shown = await page.locator(".service:visible").count()
    if (shown !== 1) throw new Error(`the search "pho" shows ${shown} cards, expected 1`)
    await press(page, take, 1.4, "Escape")

    // 5. The down card, then the dashed unknown card.
    take.say("A red border: gatus says this service is down")
    await toCloud()
    await glide(page, take, pointer, await card("Email"))
    await take.shot(page, 2.6)
    take.say("No data from gatus: unknown, never a false green")
    await glide(page, take, pointer, await card("File sync"))
    await take.shot(page, 3.0)

    // 6. The section, pointer out of the way.
    take.say("Self-hosted, open source, one small Go binary")
    await glide(page, take, pointer, { x: 1200, y: 60 })
    await take.shot(page, 3.0)
    take.say(null)
    await context.close()
  } finally {
    await browser.close()
  }
}

function parseArgs(argv: string[]) {
  let warmup = 600
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--warmup") warmup = Number(argv[++i])
    else throw new Error(`unknown argument ${argv[i]}`)
  }
  if (!(warmup >= 120)) throw new Error("--warmup must be at least 120 seconds")
  return { warmup }
}

async function main() {
  const { warmup } = parseArgs(Deno.args)
  const work = await Deno.makeTempDir({ prefix: "oko-demo-" })
  const net = `oko-demo-${Date.now().toString(36)}`
  const image = `oko-demo:${net}`
  const frames = join(RAW, "frames")
  const cleanup = async () => {
    for (const c of ["oko", "gatus", "web", "git", "mail"]) {
      await run("docker", ["rm", "-f", "-v", `${net}-${c}`]).catch(() => {})
    }
    await run("docker", ["network", "rm", net]).catch(() => {})
    await run("docker", ["rmi", image]).catch(() => {})
    await Deno.remove(work, { recursive: true }).catch(() => {})
    await Deno.remove(frames, { recursive: true }).catch(() => {})
  }
  const onSignal = () => cleanup().finally(() => Deno.exit(130))
  Deno.addSignalListener("SIGINT", onSignal)
  Deno.addSignalListener("SIGTERM", onSignal)
  try {
    await Deno.mkdir(RAW, { recursive: true })
    await makeCerts(join(work, "certs"))
    await Deno.mkdir(join(work, "stage"))
    await Deno.copyFile(join(HERE, "catalog.json"), join(work, "stage/config.json"))
    await Deno.copyFile(join(HERE, "gatus.yaml"), join(work, "gatus.yaml"))
    await Deno.copyFile(join(HERE, "nanorc"), join(work, "stage/nanorc"))
    await Deno.copyFile(join(HERE, "edit.tape"), join(work, "stage/edit.tape"))
    const url = await stage(net, image, work)
    log(`oko at ${url}`)
    await warmUp(net, warmup)
    // Fill oko's cache now, so the first frame already has every pill.
    await fetch(`${url}?refresh=1`).then((r) => r.body?.cancel())
    const stageDir = join(work, "stage")
    const edit = async () => {
      log("recording the editor")
      await run("vhs", ["edit.tape"], { cwd: stageDir, timeoutMs: 180_000 })
      JSON.parse(await Deno.readTextFile(join(stageDir, "config.json")))
      return stageDir
    }
    log("recording the browser")
    await Deno.remove(frames, { recursive: true }).catch(() => {})
    await Deno.mkdir(frames, { recursive: true })
    const take = new Take(frames)
    await recordShots(url, take, edit)
    const list = join(frames, "list.txt")
    await Deno.writeTextFile(list, take.list())
    await run("ffmpeg", [
      ...["-y", "-v", "error", "-f", "concat", "-safe", "0", "-i", list],
      ...["-vf", `fps=${FPS},format=yuv420p`, "-t", take.t.toFixed(3), "-c:v", "ffv1"],
      join(RAW, "raw.mkv"),
    ], { timeoutMs: 600_000 })
    const shifted = take.cues.map((c) => ({ ...c, start: c.start + TITLE_S, end: c.end + TITLE_S }))
    await Deno.writeTextFile(join(DEMO, "captions.vtt"), toVtt(shifted))
    log(`raw take: ${take.frames.length} frames, ${take.t.toFixed(1)} s; encoding`)
    await encode()
    log(`done: ${DEMO}`)
  } finally {
    Deno.removeSignalListener("SIGINT", onSignal)
    Deno.removeSignalListener("SIGTERM", onSignal)
    await cleanup()
  }
}

if (import.meta.main) await main()
