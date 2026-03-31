# Search + Vision Integration: Debugging Session Notes

This documents the full debugging process used to get the vision-enriched search
working on OpenCloud with posixfs storage and the Rockchip RK3566 NPU vision service.

---

## Setup

- OpenCloud running in Docker (`opencloudeu/opencloud:opencloud_release_crypt_520`)
- Storage driver: **posixfs** (not decomposedfs)
- Vision service: standalone HTTP on port 8384 (Odroid-M1S, RK3566 NPU)
- Search extractor: `SEARCH_EXTRACTOR_TYPE=vision`

---

## Major Issues Found

### 1. Wrong space ID

The `opencloud search index --space` command needs a space ID in the format
`<storageId>$<spaceId>`. The space ID is NOT the user ID.

With posixfs the space ID is stored as an xattr on the user's home directory:

```bash
getfattr -d ~/opencloud/opencloud-data/storage/users/users/<user-id>/
# look for user.oc.space.id
```

We were initially passing the user ID as the space ID, which gave "invalid space id".

The correct command:
```bash
docker exec opencloud opencloud search index \
  --space "5fa3d061-53cf-47c1-85b5-ded19ae3b6ed\$cda7d58f-aeeb-4aa2-bd6a-b3480ce7cefe"
```

Note: the `$` must be escaped with `\$` in shell/SSH to prevent variable expansion.

---

### 2. Search log level hid all vision failures

The default search log level is `error`. Vision failures are logged at `warn` level,
so they were silently swallowed. Nothing appeared in `docker logs opencloud` even
when hundreds of files were failing.

Fix: add `-e SEARCH_LOG_LEVEL=warn` to the docker run command.

---

### 3. WebP images not supported

The vision service's `model.go` only imported decoders for JPEG, PNG, GIF.
WebP images returned "image: unknown format" and were silently skipped.

Fix: add the WebP decoder import to `services/vision/pkg/inference/model.go`:
```go
_ "golang.org/x/image/webp"
```

Then rebuild on the Odroid:
```bash
cd ~/vision
/usr/local/go/bin/go build -o vision ./cmd/vision/
```

---

### 4. Deleting bleve index while container was running

We deleted the bleve index directory while the search service had open file handles
on it. The service kept its old in-memory state and re-created a corrupt/mismatched
index. Reindex commands completed but nothing was stored.

Fix: always stop the container first, delete bleve from the **host** volume path,
then start the container:

```bash
docker stop opencloud
rm -rf ~/opencloud/opencloud-data/search/bleve
docker start opencloud
sleep 8
docker exec opencloud opencloud search index --space "..."
```

---

### 5. Fake JPEG files (Apple binary plists)

Several files had `.jpeg` extensions but were actually Apple binary property lists
(`bplist00` header). These were uploaded from iOS and got corrupted or were
exported in a non-standard way. Vision correctly rejects them with "unknown format".

These 7 files are unfixable — vision cannot decode binary plists as images.
The other 2 failures were videos where ffmpeg could not extract any frames.

To identify: check the first bytes of a failing file:
```bash
xxd "/path/to/file.jpeg" | head -2
# bplist00 = Apple binary plist, not a real JPEG
```

---

### 6. `--all-spaces` flag is broken

`opencloud search index --all-spaces` silently passes an empty space ID to the
gRPC handler. It does trigger a `ListStorageSpaces` call but then fails on
storage-shares with a non-fatal error, and the personal space iteration is
incomplete.

Always use `--space <id>` explicitly.

---

## Finding the Correct Space ID

```bash
# Get all xattrs on the user home dir to find the space ID
getfattr -d ~/opencloud/opencloud-data/storage/users/users/<user-id>/

# Storage ID comes from opencloud.yaml (storageusers.driver_config.root or similar)
# or from the first segment of any existing space ID you already know
```

For this setup:
- Storage ID: `5fa3d061-53cf-47c1-85b5-ded19ae3b6ed`
- Space ID: `cda7d58f-aeeb-4aa2-bd6a-b3480ce7cefe`
- User ID: `077bc36a-77f5-4140-bc20-85455b1c4d87`

---

## Debugging Vision Failures

### Step 1: expose warn-level logs

Add to docker run:
```
-e SEARCH_LOG_LEVEL=warn
```

Then check:
```bash
docker logs opencloud 2>&1 | grep "warn.*vision"
```

Output format:
```json
{"level":"warn","service":"search","error":"vision service returned HTTP 422: ...","Name":"file.png","message":"vision service call failed"}
```

### Step 2: count and categorise failures

```bash
docker logs opencloud 2>&1 | grep "warn.*vision" | python3 -c "
import sys, json
from collections import Counter
ext = Counter()
for line in sys.stdin:
    try:
        d = json.loads(line)
        name = d.get('Name', '')
        e = name.rsplit('.', 1)[-1].lower() if '.' in name else '(none)'
        ext[e] += 1
    except: pass
print(dict(ext.most_common()))
"
```

### Step 3: HTTP proxy to intercept what arrives at vision

Use `scripts/vision-proxy.py` to run a logging proxy between the search service
and the vision service. It logs the size and magic bytes (first 4 bytes) of every
request, then forwards it to the real vision service.

```bash
# On the Odroid, start the proxy:
python3 scripts/vision-proxy.py >> /tmp/proxy.log 2>&1 &

# Restart container pointing to proxy port:
docker run ... -e SEARCH_EXTRACTOR_VISION_SERVICE_URL=http://192.168.1.4:8385 ...

# Wipe bleve and reindex:
docker stop opencloud
rm -rf ~/opencloud/opencloud-data/search/bleve
docker start opencloud && sleep 8
docker exec opencloud opencloud search index --space "5fa3d061-...\$cda7d58f-..."

# Check what arrived:
cat /tmp/proxy.log | head -30
# 89504e47 = PNG, ffd8ff = JPEG, 47494638 = GIF, 52494646 = WebP (RIFF), 1a45dfa3 = WebM, 00000020 = MP4
```

Magic bytes reference:
| Hex        | Format     |
|------------|------------|
| `89504e47` | PNG        |
| `ffd8ff`   | JPEG       |
| `47494638` | GIF        |
| `52494646` | WebP/RIFF  |
| `1a45dfa3` | WebM / MKV |
| `00000020` | MP4        |
| `6270 6c69 7374` | Apple bplist (not an image) |

---

## Final Working Docker Run Command

```bash
docker run --restart=always --name opencloud -d \
  -p 9200:9200 \
  -v $HOME/opencloud/opencloud-config:/etc/opencloud \
  -v $HOME/opencloud/opencloud-data:/var/lib/opencloud \
  -v /etc/localtime:/etc/localtime:ro \
  -e OC_INSECURE=true \
  -e PROXY_HTTP_ADDR=0.0.0.0:9200 \
  -e OC_URL=https://192.168.1.4:9200 \
  -e SEARCH_EXTRACTOR_TYPE=vision \
  -e SEARCH_EXTRACTOR_VISION_SERVICE_URL=http://192.168.1.4:8384 \
  -e SEARCH_EXTRACTOR_VISION_TIMEOUT=60s \
  -e IDP_ACCESS_TOKEN_EXPIRATION=86400 \
  -e PROXY_OIDC_ACCESS_TOKEN_VERIFY_METHOD=none \
  opencloudeu/opencloud:opencloud_release_crypt_520
```

(Remove `SEARCH_LOG_LEVEL=warn` once confirmed working — it adds noise.)

---

## Full Reindex Command

```bash
docker exec opencloud opencloud search index \
  --space "5fa3d061-53cf-47c1-85b5-ded19ae3b6ed\$cda7d58f-aeeb-4aa2-bd6a-b3480ce7cefe"
```

## Force Reindex a Specific File or Folder

The CLI only supports `--space` granularity. To force reindex of a single file or folder,
change its mtime then rerun the space index (the walker skips files with unchanged mtime):

```bash
# Single file (run inside container or map the host path):
touch "/var/lib/opencloud/storage/users/users/<user-id>/path/to/file.jpg"

# Whole folder:
find "/var/lib/opencloud/storage/users/users/<user-id>/path/to/folder" -type f -exec touch {} \;

# Then reindex:
docker exec opencloud opencloud search index --space "5fa3d061-...\$cda7d58f-..."
```

---

## How Vision Tags Work in the UI

- Vision-generated **tags** (`["golden retriever", "tabby cat", ...]`) are stored in the
  bleve search index under the file's `Tags` field.
- The **description** (`"An image containing golden retriever and tabby cat"`) is stored
  in the `Content` field for full-text search.
- OpenCloud's web client reads these from the search index and displays them on the file
  (under the title and as tag chips you didn't add yourself).
- They are **not** written back to the file's CS3 metadata/xattrs — if you wipe and
  don't reindex, they disappear.

---

## Files Changed in This Session

| File | Change |
|------|--------|
| `services/vision/pkg/inference/model.go` | Added `_ "golang.org/x/image/webp"` import |
| `services/vision/pkg/inference/rknn.go` | CGo pointer panic fix (`C.CBytes()`) |
