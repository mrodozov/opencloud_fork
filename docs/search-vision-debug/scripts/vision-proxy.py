#!/usr/bin/env python3
"""
vision-proxy.py — HTTP logging proxy for debugging vision service calls.

Listens on port 8385, logs the size and first 4 magic bytes of every request,
then forwards the request to the real vision service on port 8384.

Usage:
    python3 vision-proxy.py >> /tmp/proxy.log 2>&1 &

Then point the container at the proxy:
    -e SEARCH_EXTRACTOR_VISION_SERVICE_URL=http://<host>:8385

Magic bytes reference:
    89504e47 = PNG
    ffd8ff.. = JPEG
    47494638 = GIF
    52494646 = WebP (RIFF container)
    1a45dfa3 = WebM / MKV
    00000020 = MP4
    6270 6c69 7374 3030 = Apple bplist (NOT an image)
"""

import http.server
import urllib.request
import urllib.error
import sys

LISTEN_PORT = 8385
FORWARD_PORT = 8384
FORWARD_HOST = "localhost"


class LogProxy(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        magic = body[:4].hex() if body else "empty"
        print(
            f"[proxy] {self.path}: {length} bytes, first4={magic}",
            flush=True,
        )

        url = f"http://{FORWARD_HOST}:{FORWARD_PORT}{self.path}"
        req = urllib.request.Request(
            url,
            data=body,
            headers=dict(self.headers),
            method="POST",
        )
        try:
            resp = urllib.request.urlopen(req)
            data = resp.read()
            self.send_response(resp.status)
            self.end_headers()
            self.wfile.write(data)
        except urllib.error.HTTPError as e:
            data = e.read()
            print(f"[proxy] -> HTTP {e.code}: {data[:200]}", flush=True)
            self.send_response(e.code)
            self.end_headers()
            self.wfile.write(data)
        except Exception as e:
            print(f"[proxy] -> error: {e}", flush=True)
            self.send_response(502)
            self.end_headers()

    def log_message(self, *args):
        pass  # suppress default access log


if __name__ == "__main__":
    server = http.server.HTTPServer(("0.0.0.0", LISTEN_PORT), LogProxy)
    print(f"[proxy] listening on :{LISTEN_PORT}, forwarding to :{FORWARD_PORT}", flush=True)
    server.serve_forever()
