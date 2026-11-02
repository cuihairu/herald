# Config Switch Effect Generator

Generates off/on comparison screenshots and an animated GIF for every
Herald configuration switch whose `enabled` (or boolean) state changes
runtime behavior.

## How it works

1. `generate.py` renders a per-switch HTML demo page (replicating the
   dashboard's antd Switch + a behavioral effect preview).
2. Headless Chromium (`chromium_headless_shell`) captures the off state,
   the on state, and a sequence of frames as the switch animates
   off → on (CSS transition driven by `--virtual-time-budget`).
3. `ffmpeg` stitches the frames into a looping GIF.

Output lands in `docs/public/screenshots/config/`.

## Run

```bash
python3 tools/config_effect/generate.py
```

Requires: `python3`, `chromium_headless_shell` (Playwright cache),
`ffmpeg`. See `docs/guide/configuration-effects.md` for the generated
documentation.
