/**
 * Encodes the oko demo from the last raw take, without recording again:
 *
 *   deno task encode            (from media/demo/recipe/, or `make demo-video-encode`)
 *
 * It reads `media/demo/.raw/raw.mkv` and `poster.png` (written by record.ts), the captions in
 * `media/demo/captions.vtt` and the music in `media/demo/music.opus`. It adds the title and end
 * cards, burns in the captions (a dark band across the bottom, as in Seed's admin video), lays
 * the music under it at about -28 LUFS, and writes demo.mp4, demo.webm, poster.webp and
 * poster-640.webp to `media/demo/`. It refuses a file that is too loud, too large, not 30 fps or
 * without sound.
 *
 * Needs ffmpeg and ffprobe with libass, libx264, libvpx-vp9, libopus and libwebp.
 */
import { join, resolve } from "@std/path"

const HERE = resolve(new URL(".", import.meta.url).pathname)
export const DEMO = resolve(HERE, "..")
export const RAW = join(DEMO, ".raw")
export const W = 1280
export const H = 800
const FPS = 30
/** How long the title card and the end card stay, in seconds. */
export const TITLE_S = 2.2
export const END_S = 2.6
/** The caption band's height, as in Seed's admin video at 1280×800. */
export const BAND = 84
const CARD = {
  title: "oko",
  claim: "One page for every self-hosted service, with its status and 30-day uptime from gatus.",
  end: "github.com/spy4x/oko",
  endSub: "Free and open source, MIT licence",
}
/** The mix's integrated loudness (LUFS) and the highest true peak (dBTP) it may reach. */
const LOUDNESS = { target: -28, tolerance: 1, peak: -1 }
/** Each file's size limit, in bytes. */
const BUDGET = 3 * 1024 * 1024

/** Runs a command and returns its stderr; throws with it when the command fails. */
async function run(cmd: string, args: string[], timeoutMs = 600_000): Promise<string> {
  const out = await new Deno.Command(cmd, {
    args,
    stdout: "piped",
    stderr: "piped",
    signal: AbortSignal.timeout(timeoutMs),
  }).output()
  const text = new TextDecoder().decode(out.stdout) + new TextDecoder().decode(out.stderr)
  if (!out.success) throw new Error(`${cmd} ${args.join(" ")} failed:\n${text.slice(-3000)}`)
  return text
}

const ffmpeg = (args: string[]) => run("ffmpeg", ["-y", "-hide_banner", "-nostats", ...args])

/** A media file's length in seconds. */
export async function duration(file: string): Promise<number> {
  const out = await run("ffprobe", [
    ...["-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", file],
  ])
  const seconds = Number(out.trim())
  if (!seconds) throw new Error(`ffprobe could not read the length of ${file}`)
  return seconds
}

/** One caption: start and end in seconds of the finished video, and its text. */
export interface Cue {
  start: number
  end: number
  text: string
}

const stamp = (s: number) => {
  const ms = Math.round(s * 1000)
  const hh = String(Math.floor(ms / 3_600_000)).padStart(2, "0")
  const mm = String(Math.floor(ms / 60_000) % 60).padStart(2, "0")
  const ss = String(Math.floor(ms / 1000) % 60).padStart(2, "0")
  return `${hh}:${mm}:${ss}.${String(ms % 1000).padStart(3, "0")}`
}

/** WebVTT text for `cues`. */
export function toVtt(cues: Cue[]): string {
  return "WEBVTT\n\n" +
    cues.map((c, i) => `${i + 1}\n${stamp(c.start)} --> ${stamp(c.end)}\n${c.text}\n`).join("\n")
}

/** Reads the cues of a WebVTT file: its timing lines and the text under each. */
export function parseVtt(text: string): Cue[] {
  const seconds = (t: string) => {
    const parts = t.split(":").map(Number)
    return parts.reduce((sum, p) => sum * 60 + p, 0)
  }
  const cues: Cue[] = []
  for (const block of text.replace(/\r/g, "").split(/\n\n+/)) {
    const lines = block.split("\n")
    const i = lines.findIndex((l) => l.includes("-->"))
    if (i === -1) continue
    const [start, end] = lines[i].split("-->").map((s) => seconds(s.trim().split(" ")[0]))
    cues.push({ start, end, text: lines.slice(i + 1).join(" ").trim() })
  }
  return cues
}

/** ASS colour from a CSS hex colour and an opacity. */
const ass = (hex: string, opacity = 1) => {
  const [r, g, b] = [1, 3, 5].map((i) => hex.slice(i, i + 2))
  const alpha = Math.round((1 - opacity) * 255).toString(16).padStart(2, "0")
  return `&H${alpha}${b}${g}${r}`.toUpperCase()
}
const assTime = (s: number) => stamp(s).slice(1, -1)
const assText = (t: string) => t.replace(/[{}\\]/g, "")
/** An ASS vector rectangle from (x, y), w by h. */
const rect = (x: number, y: number, w: number, h: number) =>
  `{\\pos(${x},${y})\\p1}m 0 0 l ${w} 0 ${w} ${h} 0 ${h}{\\p0}`

/** The subtitle script: the title and end cards, and every caption on its band. */
export function assScript(cues: Cue[], total: number): string {
  const bg = "#0a0a0a"
  const event = (layer: number, start: number, end: number, style: string, text: string) =>
    `Dialogue: ${layer},${assTime(start)},${assTime(end)},${style},,0,0,0,,${text}`
  const endAt = total - END_S
  const lines = [
    // The title card fades into the dashboard; the end card fades in over the last frame.
    event(5, 0, TITLE_S, "Fill", `{\\fad(0,300)}${rect(0, 0, W, H)}`),
    event(6, 0, TITLE_S, "Title", `{\\fad(0,300)\\pos(${W / 2},${H / 2 - 40})}${CARD.title}`),
    event(6, 0, TITLE_S, "Sub", `{\\fad(0,300)\\pos(${W / 2},${H / 2 + 40})}${CARD.claim}`),
    event(5, endAt, total, "Fill", `{\\fad(300,0)}${rect(0, 0, W, H)}`),
    event(6, endAt, total, "End", `{\\fad(300,0)\\pos(${W / 2},${H / 2 - 24})}${CARD.end}`),
    event(6, endAt, total, "Sub", `{\\fad(300,0)\\pos(${W / 2},${H / 2 + 44})}${CARD.endSub}`),
    // A caption ends where the end card starts, even when the take ran a frame longer.
    ...cues.map((c) => ({ ...c, end: Math.min(c.end, endAt) })).flatMap((c) => [
      event(1, c.start, c.end, "Band", `{\\fad(250,250)}${rect(0, H - BAND, W, BAND)}`),
      event(
        2,
        c.start,
        c.end,
        "Caption",
        `{\\fad(250,250)\\pos(${W / 2},${H - BAND / 2})}${assText(c.text)}`,
      ),
    ]),
  ]
  const style = (name: string, size: number, colour: string, bold: boolean) =>
    `Style: ${name},IBM Plex Sans,${size},${colour},&H00000000,&H00000000,&H00000000,` +
    `${bold ? -1 : 0},0,0,0,100,100,0,0,1,0,0,5,0,0,0,1`
  return [
    "[Script Info]",
    "ScriptType: v4.00+",
    `PlayResX: ${W}`,
    `PlayResY: ${H}`,
    "ScaledBorderAndShadow: yes",
    "WrapStyle: 2",
    "",
    "[V4+ Styles]",
    "Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, " +
    "BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, " +
    "BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding",
    style("Fill", 20, ass(bg), false).replace(",5,0,0,0,1", ",7,0,0,0,1"),
    style("Band", 20, ass(bg, 0.85), false).replace(",5,0,0,0,1", ",7,0,0,0,1"),
    style("Caption", 32, ass("#ffffff"), false),
    style("Title", 96, ass("#ededed"), true),
    style("End", 56, ass("#ededed"), true),
    style("Sub", 30, ass("#9a9a9a"), false),
    "",
    "[Events]",
    "Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text",
    ...lines,
    "",
  ].join("\n")
}

/** Integrated loudness (LUFS) and true peak (dBTP) of a file's sound, by EBU R 128. */
async function loudness(file: string): Promise<{ integrated: number; peak: number }> {
  const text = await ffmpeg([
    "-i",
    file,
    "-map",
    "0:a",
    "-af",
    "ebur128=peak=true",
    "-f",
    "null",
    "-",
  ])
  const summary = text.slice(text.lastIndexOf("Summary:"))
  const integrated = summary.match(/I:\s+(-?[\d.]+) LUFS/)?.[1]
  const peak = summary.match(/Peak:\s+(-?[\d.]+) dBFS/)?.[1]
  if (!integrated || !peak) throw new Error(`could not measure the loudness of ${file}`)
  return { integrated: Number(integrated), peak: Number(peak) }
}

/** The music filter: trimmed to the video, faded in over 1 s and out over 2 s, at `gain` dB. */
const musicFilter = (total: number, gain: number) =>
  [
    `atrim=0:${total.toFixed(3)}`,
    "asetpts=PTS-STARTPTS",
    `volume=${gain.toFixed(2)}dB`,
    "afade=t=in:d=1",
    `afade=t=out:st=${(total - 2).toFixed(3)}:d=2`,
  ].join(",")

/** Encodes one file, trying each quality setting until it fits the budget. */
async function encodeOne(
  out: string,
  video: string,
  audio: string,
  total: number,
  codec: (q: number) => string[],
  qualities: number[],
): Promise<number> {
  for (const q of qualities) {
    await ffmpeg([
      ...["-i", join(RAW, "raw.mkv"), "-i", join(DEMO, "music.opus")],
      ...["-filter_complex", `[0:v]${video}[v];[1:a]${audio}[a]`],
      ...["-map", "[v]", "-map", "[a]", "-t", total.toFixed(3), "-r", String(FPS)],
      ...["-map_metadata", "-1", "-map_chapters", "-1"],
      ...codec(q),
      out,
    ])
    const { size } = await Deno.stat(out)
    if (size <= BUDGET) return size
  }
  throw new Error(`${out} stays above ${BUDGET} bytes`)
}

/** Checks a finished file: 30 fps, one audio stream, the expected length. */
async function verify(file: string, total: number) {
  const out = await run("ffprobe", [
    ...["-v", "error", "-show_entries", "stream=codec_type,r_frame_rate", "-of", "csv=p=0", file],
  ])
  if (!out.includes("video,30/1")) throw new Error(`${file} is not 30 fps: ${out}`)
  if (!/^audio/m.test(out)) throw new Error(`${file} has no audio stream`)
  const length = await duration(file)
  if (Math.abs(length - total) > 0.2) {
    throw new Error(`${file} lasts ${length} s, expected ${total.toFixed(2)} s`)
  }
}

export async function encode() {
  const raw = join(RAW, "raw.mkv")
  const rawSeconds = await duration(raw)
  const total = TITLE_S + rawSeconds + END_S
  const cues = parseVtt(await Deno.readTextFile(join(DEMO, "captions.vtt")))
  if (!cues.length) throw new Error("captions.vtt holds no captions")
  for (const c of cues) {
    // At least 2.5 s, and long enough to read at about 15 characters a second.
    if (c.end - c.start < Math.max(2.5, c.text.length / 15)) {
      throw new Error(`"${c.text}" shows for ${(c.end - c.start).toFixed(1)} s, too short to read`)
    }
    if (c.end > total - END_S + 0.1) throw new Error(`"${c.text}" runs into the end card`)
  }
  const music = join(DEMO, "music.opus")
  if (await duration(music) < total) {
    throw new Error(`music.opus is shorter than the video's ${total.toFixed(1)} s`)
  }
  const work = await Deno.makeTempDir({ prefix: "oko-encode-" })
  try {
    const script = join(work, "captions.ass")
    await Deno.writeTextFile(script, assScript(cues, total))
    // The music's gain: measure the trimmed, faded excerpt at 0 dB, then move it to the target.
    const probe = join(work, "music.wav")
    await ffmpeg(["-i", music, "-af", musicFilter(total, 0), probe])
    const gain = LOUDNESS.target - (await loudness(probe)).integrated
    const video = [
      `fps=${FPS}`,
      `tpad=start_duration=${TITLE_S}:start_mode=clone:stop_duration=${END_S}:stop_mode=clone`,
      `subtitles=${script}:fontsdir=${join(HERE, "fonts")}`,
      "format=yuv420p",
    ].join(",")
    const audio = musicFilter(total, gain)
    const mp4 = join(DEMO, "demo.mp4")
    const webm = join(DEMO, "demo.webm")
    const sizes = {
      mp4: await encodeOne(mp4, video, audio, total, (crf) => [
        ...["-c:v", "libx264", "-preset", "slow", "-tune", "animation", "-crf", String(crf)],
        ...["-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart"],
      ], [26, 29, 32]),
      webm: await encodeOne(webm, video, audio, total, (crf) => [
        ...["-c:v", "libvpx-vp9", "-crf", String(crf), "-b:v", "0", "-row-mt", "1"],
        ...["-deadline", "good", "-cpu-used", "2", "-c:a", "libopus", "-b:a", "96k"],
      ], [38, 42, 46]),
    }
    for (const file of [mp4, webm]) {
      await verify(file, total)
      const sound = await loudness(file)
      if (
        Math.abs(sound.integrated - LOUDNESS.target) > LOUDNESS.tolerance ||
        sound.peak > LOUDNESS.peak
      ) {
        throw new Error(`${file}: ${sound.integrated} LUFS, true peak ${sound.peak} dBTP`)
      }
      console.log(`${file}: ${sound.integrated} LUFS, true peak ${sound.peak} dBTP`)
    }
    const poster = join(RAW, "poster.png")
    for (const [name, width] of [["poster.webp", 1280], ["poster-640.webp", 640]] as const) {
      await ffmpeg([
        ...["-i", poster, "-vf", `scale=${width}:-2:flags=lanczos`],
        ...["-map_metadata", "-1", "-c:v", "libwebp", "-quality", "85", join(DEMO, name)],
      ])
    }
    console.log(
      `demo: ${total.toFixed(1)} s, mp4 ${sizes.mp4} B, webm ${sizes.webm} B, ` +
        `music gain ${gain.toFixed(1)} dB, ${cues.length} captions`,
    )
  } finally {
    await Deno.remove(work, { recursive: true })
  }
}

if (import.meta.main) await encode()
