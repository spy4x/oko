# oko demo video

The recipe for `media/demo/`: `demo.mp4`, `demo.webm`, `poster.webp`, `poster-640.webp` and
`captions.vtt`. Everything on screen is staged: the example.com catalog in `catalog.json`, gatus
checking nginx containers (`gatus.yaml`), and the editor shot in `edit.tape`. Nothing touches a real
server, account or home directory.

## Needs

- Docker (oko is built from this checkout; gatus and nginx are pulled by digest)
- Deno 2, and Playwright's Chromium: `deno run -A npm:playwright@1.63.0 install chromium`
- `vhs`, `nano`, `openssl`
- `ffmpeg` and `ffprobe` with libass, libx264, libvpx-vp9, libopus and libwebp

## Remake the video

1. From the repository root, run `make demo-video`. It takes about 13 minutes: 10 of them let gatus
   build uptime history, so the cards show real percentages.
2. It writes the raw take to `media/demo/.raw/` (ignored by git), `captions.vtt` from the captions
   in `record.ts` and their times, and then encodes. The encoder prints the length, the sizes and
   the loudness, and fails on a mix louder or quieter than -28 LUFS (±1), a true peak above -1 dBTP,
   a file over 3 MB, a caption too short to read, or a file that is not 30 fps with sound.
3. Check every frame on a contact sheet:

   ```bash
   ffmpeg -i media/demo/demo.mp4 -vf "fps=1,scale=320:-1,tile=6x8" -frames:v 1 sheet.png
   ```

4. Commit `media/demo/` without `.raw/`.

## Change only the captions, cards or music

Edit `media/demo/captions.vtt` (times are of the finished video, title card included), the `CARD`
text in `encode.ts`, or replace `music.opus` and `MUSIC.md`. Then run `make demo-video-encode`: it
re-encodes the last raw take in under a minute, without recording again. A new recording rewrites
`captions.vtt` from `record.ts`, so move a wording change there too.

## What is where

- `record.ts`: stages oko, gatus and the services, records the shots and writes the raw take and the
  captions. Every frame is a screenshot with a fixed time on screen, and the pointer glides one
  frame per 1/30 s, so a busy machine slows the recording, not the video. The page is zoomed to 1.5
  (`ZOOM`), so card names stay readable where the clip plays about 528 px wide, and the shots scroll
  between the Home and Cloud sections.
- `encode.ts`: title and end cards, burned-in captions (`fonts/`: IBM Plex Sans, OFL), music, the
  two video files and the posters, and the checks.
- `pointer.js`, `keys.js`: the drawn pointer and the keycaps, from the global `demo-video` skill.
- `edit.tape`, `nanorc`: the editor shot, as six screenshots; `EDITOR` in `record.ts` gives each its
  time on screen and caption.
