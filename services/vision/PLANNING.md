# Vision Service — Planning Notes

## Potential improvements

### Replace ffmpeg/ffprobe syscalls with native Go bindings (`go-astiav`)

**What:** Replace `pkg/video/keyframe.go`'s `exec.Command("ffmpeg", ...)` and `exec.Command("ffprobe", ...)` calls with CGo bindings to the libav* C libraries via [`github.com/asticode/go-astiav`](https://github.com/asticode/go-astiav).

**Why:**
- Eliminates the runtime dependency on `ffmpeg` and `ffprobe` binaries
- Eliminates temp files on disk — currently `ExtractKeyframes` writes the full video to a temp file just so ffmpeg can seek it; with libav you decode entirely in memory
- In-process decoding is faster (no subprocess + IPC overhead)

**How:**
1. Install FFmpeg dev packages on the target machine (same pattern as `librknnrt.so`):
   ```bash
   apt install libavcodec-dev libavformat-dev libavfilter-dev libavutil-dev libswscale-dev libswresample-dev
   ```
2. Add `go-astiav` as a dependency and add CGo flags (mirrors `rknn.go`):
   ```go
   #cgo LDFLAGS: -lavcodec -lavformat -lavutil -lavfilter -lswscale -lswresample
   ```
3. Rewrite `probeDuration`, `extractEvenly`, and `extractIFrames` using the libav API (open format context → find video stream → seek + decode frames → encode to JPEG in memory).

**Note on linking:** Since the binary already dynamically links `librknnrt.so` via CGo (`#cgo LDFLAGS: -lrknnrt`), linking libav the same way is no different — the `.so` files just need to be on the library path at runtime. Static linking is an option but not necessary. Cross-compilation is not a concern since the binary runs natively on the target machine.

**Trade-offs:**
- Adds runtime `.so` dependencies (`libavcodec.so` etc.) — same model as `librknnrt.so`
- Codec updates handled via package manager (same as any system lib)
- LGPL license — fine for most uses, but avoid enabling GPL-only codecs (x264, x265) if distribution matters

**Status:** Not started — parked for later consideration.
