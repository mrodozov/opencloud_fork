# OpenCloud Search Service — Complete Reference

## Table of Contents

1. [Overview](#overview)
2. [Architecture & How It Connects](#architecture--how-it-connects)
3. [Configuration & Environment Variables](#configuration--environment-variables)
4. [Running the Service Standalone](#running-the-service-standalone)
5. [Search Engine Backends](#search-engine-backends)
6. [Content Extractors](#content-extractors)
7. [Event-Driven Indexing](#event-driven-indexing)
8. [Manual Indexing (CLI)](#manual-indexing-cli)
9. [The gRPC API](#the-grpc-api)
10. [Query Language (KQL)](#query-language-kql)
11. [Index Data Structure](#index-data-structure)
12. [Metrics & Observability](#metrics--observability)
13. [Troubleshooting](#troubleshooting)

---

## Overview

The search service indexes files stored in OpenCloud and provides full-text and metadata search across all user spaces. It has two modes of operation:

- **Reactive**: listens to file events over NATS (uploads, moves, deletes) and indexes incrementally
- **Manual**: triggered via CLI (`opencloud search index`) or gRPC call to reindex an entire space

The search service does **not** store files — it stores metadata about files (name, size, mtime, tags, extracted content) in a local index (Bleve by default, or OpenSearch).

---

## Architecture & How It Connects

```
                          ┌────────────────────────────────┐
                          │        search service           │
                          │                                 │
  NATS events ────────────►  event consumer (12 event types)│
  (file upload, move, etc)│            │                    │
                          │            ▼                    │
  gRPC clients ───────────►  gRPC handler (Search, Index)  │
  (desktop client, web)   │            │                    │
                          │            ▼                    │
                          │     search.Service              │
                          │      ├─ engine (bleve)          │
                          │      ├─ extractor               │
                          │      └─ gateway client          │
                          └────────────┬───────────────────-┘
                                       │
                    ┌──────────────────┼────────────────────┐
                    │                  │                     │
                    ▼                  ▼                     ▼
             CS3 Gateway          Bleve index         Vision/Tika
             (get files,          (on disk)           service
              list spaces)                            (extract tags)
```

### Services the search service connects to:

| Service | Default address | Purpose |
|---|---|---|
| CS3 Gateway (reva) | `127.0.0.1:19000` | Stat files, download files, list spaces |
| NATS event bus | `127.0.0.1:9233` | Listen for file change events |
| Vision service | configurable | Extract AI tags from images/videos |
| Apache Tika | `127.0.0.1:9998` | Extract text content from documents |
| OpenSearch | configurable | Alternative to Bleve (if configured) |

### Ports the search service itself exposes:

| Address | Purpose |
|---|---|
| `127.0.0.1:9220` | gRPC API (Search, IndexSpace) |
| `127.0.0.1:9224` | Debug HTTP (metrics, health, config, pprof) |

---

## Configuration & Environment Variables

Configuration is applied in this order (each overrides the previous):
1. Defaults hardcoded in `pkg/config/defaults/`
2. YAML config file (`/etc/opencloud/opencloud.yaml` or service-specific file)
3. Environment variables

### Core / Auth

| Env var | Default | Required | Description |
|---|---|---|---|
| `OC_JWT_SECRET` / `SEARCH_JWT_SECRET` | — | **YES** | JWT secret for token validation. Must match all other services. |
| `OC_SERVICE_ACCOUNT_ID` / `SEARCH_SERVICE_ACCOUNT_ID` | — | **YES** | Service account ID used internally to access storage on behalf of users. |
| `OC_SERVICE_ACCOUNT_SECRET` / `SEARCH_SERVICE_ACCOUNT_SECRET` | — | **YES** | Service account secret. |
| `OC_REVA_GATEWAY` | `127.0.0.1:19000` | YES | Address of the CS3 gateway. The gateway is the single entry point to all storage operations. |

### Network

| Env var | Default | Description |
|---|---|---|
| `SEARCH_GRPC_ADDR` | `127.0.0.1:9220` | gRPC listen address. Change to `0.0.0.0:9220` to accept external connections. |
| `SEARCH_GRPC_DISABLED` | `false` | Disable gRPC entirely (event-only mode). |
| `SEARCH_DEBUG_ADDR` | `127.0.0.1:9224` | Debug HTTP server (Prometheus metrics, health check). |
| `SEARCH_DEBUG_TOKEN` | — | Bearer token to protect the debug endpoints. |
| `SEARCH_DEBUG_PPROF` | `false` | Enable Go pprof profiling endpoint. |

### Search Engine

| Env var | Default | Description |
|---|---|---|
| `SEARCH_ENGINE_TYPE` | `bleve` | Which index backend to use. Options: `bleve`, `open-search`. |
| `SEARCH_ENGINE_BLEVE_DATA_PATH` | `$OC_BASE_DATA_PATH/search` | Directory where Bleve stores its index. The actual index lands at `<path>/bleve/`. |

#### OpenSearch (only relevant when `SEARCH_ENGINE_TYPE=open-search`)

| Env var | Default | Description |
|---|---|---|
| `SEARCH_ENGINE_OPEN_SEARCH_CLIENT_ADDRESSES` | — | Comma-separated list of OpenSearch node addresses. |
| `SEARCH_ENGINE_OPEN_SEARCH_CLIENT_USERNAME` | — | OpenSearch username. |
| `SEARCH_ENGINE_OPEN_SEARCH_CLIENT_PASSWORD` | — | OpenSearch password. |
| `SEARCH_ENGINE_OPEN_SEARCH_CLIENT_CA_CERT` | — | Path to CA certificate for TLS. |
| `SEARCH_ENGINE_OPEN_SEARCH_CLIENT_INSECURE` | `false` | Skip TLS verification for OpenSearch. |
| `SEARCH_ENGINE_OPEN_SEARCH_RESOURCE_INDEX_NAME` | `opencloud-resource` | Index name inside OpenSearch. |

### Content Extractor

| Env var | Default | Description |
|---|---|---|
| `SEARCH_EXTRACTOR_TYPE` | `basic` | Which extractor to use. Options: `basic`, `tika`, `vision`. |
| `SEARCH_EXTRACTOR_CS3SOURCE_INSECURE` / `OC_INSECURE` | `false` | Skip TLS when downloading files from storage for extraction. |
| `SEARCH_CONTENT_EXTRACTION_SIZE_LIMIT` | `20971520` (20 MB) | Files larger than this are not sent to the extractor. |
| `SEARCH_BATCH_SIZE` | `500` | How many documents to accumulate before flushing a batch to the index. |

#### Tika (only relevant when `SEARCH_EXTRACTOR_TYPE=tika`)

| Env var | Default | Description |
|---|---|---|
| `SEARCH_EXTRACTOR_TIKA_TIKA_URL` | `http://127.0.0.1:9998` | URL of the Apache Tika server. |
| `SEARCH_EXTRACTOR_TIKA_CLEAN_STOP_WORDS` | `true` | Remove common stop words (the, a, is, …) from extracted text. |

#### Vision (only relevant when `SEARCH_EXTRACTOR_TYPE=vision`)

| Env var | Default | Description |
|---|---|---|
| `SEARCH_EXTRACTOR_VISION_SERVICE_URL` | — | URL of the vision inference service (e.g. `http://192.168.1.5:8384`). |
| `SEARCH_EXTRACTOR_VISION_TIMEOUT` | `60s` | How long to wait for the vision service to respond before giving up on a file. |

### Events (NATS)

| Env var | Default | Description |
|---|---|---|
| `OC_EVENTS_ENDPOINT` / `SEARCH_EVENTS_ENDPOINT` | `127.0.0.1:9233` | NATS JetStream endpoint. |
| `OC_EVENTS_CLUSTER` / `SEARCH_EVENTS_CLUSTER` | `opencloud-cluster` | NATS cluster name. |
| `SEARCH_EVENTS_DISABLED` | `false` | Disable event consumption entirely. Use this if you only want manual indexing. |
| `OC_ASYNC_UPLOADS` / `SEARCH_EVENTS_ASYNC_UPLOADS` | `true` | When true, listens for `UploadReady` events. When false, listens for `FileUploaded`. Match this to the storage-users configuration. |
| `SEARCH_EVENTS_NUM_CONSUMERS` | `1` | Number of parallel event processing workers. Increase if events are backing up. |
| `SEARCH_EVENTS_REINDEX_DEBOUNCE_DURATION` | `1000` | Milliseconds to wait after the last event for a space before triggering a full space reindex. Prevents thundering-herd on bulk uploads. |
| `SEARCH_EVENTS_MAX_ACK_PENDING` | `1000` | Max number of events in-flight before NATS pauses delivery. |
| `SEARCH_EVENTS_ACK_WAIT` | `1m` | How long NATS waits for an ack before redelivering an event. |
| `OC_EVENTS_ENABLE_TLS` / `SEARCH_EVENTS_ENABLE_TLS` | `false` | Enable TLS for NATS. |
| `OC_EVENTS_TLS_INSECURE` / `SEARCH_EVENTS_TLS_INSECURE` | `false` | Skip TLS verification for NATS. |

### Logging

| Env var | Default | Description |
|---|---|---|
| `OC_LOG_LEVEL` / `SEARCH_LOG_LEVEL` | `error` | Log verbosity. Options: `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic`. |

---

## Running the Service Standalone

### Minimum required environment

```bash
export OC_JWT_SECRET="your-jwt-secret"
export OC_SERVICE_ACCOUNT_ID="your-service-account-id"
export OC_SERVICE_ACCOUNT_SECRET="your-service-account-secret"
export OC_REVA_GATEWAY="127.0.0.1:19000"

opencloud search server
```

The service will:
- Connect to the gateway at `OC_REVA_GATEWAY`
- Connect to NATS at `127.0.0.1:9233`
- Create a Bleve index at `$OC_BASE_DATA_PATH/search/bleve/`
- Listen for gRPC on `127.0.0.1:9220`

### With vision extraction

```bash
export OC_JWT_SECRET="your-jwt-secret"
export OC_SERVICE_ACCOUNT_ID="..."
export OC_SERVICE_ACCOUNT_SECRET="..."
export OC_REVA_GATEWAY="127.0.0.1:19000"
export OC_INSECURE=true                                         # if storage uses self-signed cert
export SEARCH_EXTRACTOR_TYPE=vision
export SEARCH_EXTRACTOR_VISION_SERVICE_URL="http://192.168.1.5:8384"
export SEARCH_EXTRACTOR_VISION_TIMEOUT=60s
export SEARCH_ENGINE_BLEVE_DATA_PATH=/var/lib/opencloud/search

opencloud search server
```

### As a Docker env block (for docker run -e)

```
-e OC_JWT_SECRET=... \
-e OC_SERVICE_ACCOUNT_ID=... \
-e OC_SERVICE_ACCOUNT_SECRET=... \
-e OC_REVA_GATEWAY=127.0.0.1:19000 \
-e SEARCH_EXTRACTOR_TYPE=vision \
-e SEARCH_EXTRACTOR_VISION_SERVICE_URL=http://192.168.1.5:8384 \
-e SEARCH_EXTRACTOR_VISION_TIMEOUT=60s \
-e OC_INSECURE=true \
```

---

## Search Engine Backends

### Bleve (default)

Bleve is an embedded Go search library — no external process needed. The index is a directory on disk.

**Index location**: `$SEARCH_ENGINE_BLEVE_DATA_PATH/bleve/`

**Files inside the index directory**:
```
bleve/
  index_meta.json          ← index schema/mapping (do not edit manually)
  store/
    root.bolt              ← BoltDB key-value store (index metadata, term vectors)
    000000000067.zap       ← Zap segment file (actual indexed documents)
    000000000068.zap       ← segments are merged over time
```

**Field analyzers**:
- `Name`, `Tags`: `lowercaseKeyword` — stored as single lowercase token, exact-match
- `Content`: `fulltext` — unicode tokenized, lowercased, Porter stemmed (enables partial matching)
- All other fields: `keyword` — exact match only, no tokenization

**Wiping the index** (forces full reindex next time):
```bash
rm -rf /var/lib/opencloud/search/bleve
```
The service recreates it automatically on startup. Run `opencloud search index` after restart.

### OpenSearch

Use when you need distributed search or when the Bleve index gets too large.

```bash
export SEARCH_ENGINE_TYPE=open-search
export SEARCH_ENGINE_OPEN_SEARCH_CLIENT_ADDRESSES=http://opensearch:9200
export SEARCH_ENGINE_OPEN_SEARCH_CLIENT_USERNAME=admin
export SEARCH_ENGINE_OPEN_SEARCH_CLIENT_PASSWORD=admin
```

The service creates the index `opencloud-resource` automatically with a predefined template on startup.

---

## Content Extractors

The extractor runs when a file is indexed. It decides what metadata gets stored in the index.

### Basic extractor (`SEARCH_EXTRACTOR_TYPE=basic`)

Only reads metadata already attached to the resource:
- File name, size, MIME type, modification time
- Tags from `ArbitraryMetadata["tags"]`

No file content is read. Suitable for privacy-sensitive deployments or low-resource hardware.

### Tika extractor (`SEARCH_EXTRACTOR_TYPE=tika`)

Downloads the file from storage and sends it to Apache Tika for content extraction.

Extracted fields:
- Full text content (indexed and searchable)
- Image dimensions (width/height)
- EXIF photo metadata (camera make/model, ISO, f-number, focal length, GPS)
- GPS coordinates (latitude, longitude, altitude)
- Audio tags (album, artist, track, genre, duration)

Requires a running Tika server:
```bash
docker run -d -p 9998:9998 apache/tika
export SEARCH_EXTRACTOR_TIKA_TIKA_URL=http://127.0.0.1:9998
```

### Vision extractor (`SEARCH_EXTRACTOR_TYPE=vision`)

Downloads the file from storage and sends it to the vision service for AI analysis.

**Only processes**: `image/*` and `video/*` MIME types. Other files fall back to basic extraction.

**Skips files**: larger than `SEARCH_CONTENT_EXTRACTION_SIZE_LIMIT` (default 20 MB).

**Vision service API** (what the search service calls):

```
POST {SEARCH_EXTRACTOR_VISION_SERVICE_URL}/v1/analyze/image
POST {SEARCH_EXTRACTOR_VISION_SERVICE_URL}/v1/analyze/video

Content-Type: application/octet-stream
Body: raw file bytes

Response:
{
  "description": "A cat sitting on a windowsill",
  "tags": ["cat", "window", "indoor"],
  "keyframe_count": 10    ← only for video
}
```

The description is appended to the `Content` field (searchable as text). Tags are merged with existing file tags and deduplicated.

If the vision service is unreachable or returns an error, the file is still indexed with basic metadata — a warning is logged but the indexing does not fail.

---

## Event-Driven Indexing

When the service starts, it subscribes to a NATS consumer group called `"search-pull"`. For every event it receives, it either updates individual resources or schedules a full space reindex.

### Event types and what happens

| Event | What triggers it | How search responds |
|---|---|---|
| `UploadReady` | Async upload finished | Debounce → reindex space |
| `FileUploaded` | Sync upload finished | Debounce → reindex space |
| `FileTouched` | File mtime updated | Debounce → reindex space |
| `ContainerCreated` | New folder created | Debounce → reindex space |
| `FileVersionRestored` | File rolled back to old version | Debounce → reindex space |
| `TagsAdded` | Tags added to file | Immediate upsert of that file only |
| `TagsRemoved` | Tags removed from file | Immediate upsert of that file only |
| `SpaceRenamed` | Space renamed | Debounce → reindex space |
| `ItemMoved` | File/folder moved or renamed | Update path/name in index |
| `ItemTrashed` | File moved to trash | Mark as deleted in index |
| `ItemRestored` | File restored from trash | Unmark deleted in index |
| `ItemPurged` | File permanently deleted | Remove from index |
| `TrashbinPurged` | Trash emptied | Remove all deleted items for that space |

### The debouncer

Most events do not immediately trigger content extraction. Instead they are debounced:

```
event arrives → start 1000ms timer
another event for same space → reset timer
timer fires → trigger IndexSpace (full reindex of that space)
```

The timeout cap is 30 seconds — even if events keep coming, a reindex is forced every 30s.

This prevents thrashing during bulk uploads (e.g. syncing 500 files triggers one reindex, not 500).

### Checking if events are flowing

```bash
docker logs opencloud 2>&1 | grep -i "search\|upload\|index" | tail -30
```

Or check NATS consumer lag (if you have natscli):
```bash
nats consumer info opencloud-cluster search-pull
```

---

## Manual Indexing (CLI)

Used to index existing files that were uploaded before the search service was running, or after wiping the index.

### Index a specific space

The space ID format is `<storageId>$<spaceId>`. You can find it in the storage-users logs:

```bash
opencloud search index --space "5fa3d061-53cf-47c1-85b5-ded19ae3b6ed$cda7d58f-aeeb-4aa2-bd6a-b3480ce7cefe"
```

This command:
1. Connects to the search gRPC service on `127.0.0.1:9220`
2. Sends an `IndexSpace` RPC
3. The service walks the entire space tree via the CS3 gateway
4. For each file: checks if it is already indexed with the same mtime → skips if yes
5. For new/changed files: downloads file data, runs extractor, writes to index
6. Has a 10-minute timeout

Exit 0 with no output = success (the service logs at debug level internally).

### Index all spaces

```bash
opencloud search index --all-spaces
```

This calls `IndexSpace` with an empty space ID, which triggers the server-side path:
1. Gets a service account auth context
2. Calls `ListStorageSpaces` on the gateway
3. Indexes each space found

Note: requires the service account to have access to list all spaces.

### Finding a space ID

**From error logs** — storage-users logs include space IDs on every file operation:
```json
{"spaceid":"5fa3d061-53cf-47c1-85b5-ded19ae3b6ed$cda7d58f-aeeb-4aa2-bd6a-b3480ce7cefe"}
```

**From the filesystem** (inside the container):
```bash
find /var/lib/opencloud/storage/users -name ".spaceroot" | head -20
```
Each `.spaceroot` file path encodes the space structure.

**Format rules**:
- `<storageId>$<spaceId>` — standard format, what you pass to `--space`
- `<storageId>$<spaceId>!<nodeId>` — extended format with root node ID (also accepted)

### Force a full reindex from scratch

1. Remove the index:
   ```bash
   rm -rf ~/opencloud/opencloud-data/search/bleve
   # or inside container:
   rm -rf /var/lib/opencloud/search/bleve
   ```
2. Restart the container (service recreates index structure on startup)
3. Run:
   ```bash
   opencloud search index --space "5fa3d061-....$cda7d58f-..."
   ```

---

## The gRPC API

Service name: `eu.opencloud.api.search`
Default address: `127.0.0.1:9220`

### SearchProvider.Search

Performs a user-scoped search. The user's identity is taken from the JWT in the request metadata.

```protobuf
rpc Search(SearchRequest) returns (SearchResponse)

message SearchRequest {
  string query = 1;          // KQL query string
  int32  page_size = 2;      // 0 = 200, -1 = unlimited
  string page_token = 3;     // Pagination token from previous response
  Reference ref = 4;         // Optional: scope search to this path/space
}

message SearchResponse {
  repeated Match matches = 1;
  int32  total_matches = 2;
  string next_page_token = 3;
}
```

Results are cached for 1 second per (query, user, page_size, ref) tuple.

Slow queries (>500ms) are logged as warnings.

### SearchProvider.IndexSpace

Triggers manual indexing. Can be called via the CLI or directly over gRPC.

```protobuf
rpc IndexSpace(IndexSpaceRequest) returns (IndexSpaceResponse)

message IndexSpaceRequest {
  string space_id = 1;   // Empty = index all spaces
  string user_id = 2;    // Unused
}
```

Timeout: the gRPC client (`opencloud search index`) sets a 10-minute call timeout.

---

## Query Language (KQL)

The search service uses a subset of Keyword Query Language (KQL). Queries are compiled to Bleve queries internally.

### Basic text search

```
cat                        → search Name field for "cat"
"orange cat"               → phrase search in Name
```

### Field-specific search

```
name:report
name:"Q4 report"
content:invoice            → full-text search in extracted content
tag:vacation
tag:cat
tags:animal
mtime>=2024-01-01
mtime<2024-06-01
size>1000000
mediatype:image
mediatype:video
mediatype:pdf
mediatype:document
type:1                     → type 1 = file, type 2 = folder
```

### Mime type shortcuts

| Query | Matches |
|---|---|
| `mediatype:file` | Any non-folder |
| `mediatype:folder` | Folders |
| `mediatype:document` | DOCX, ODT, TXT, RTF, PDF-like |
| `mediatype:spreadsheet` | XLSX, ODS, CSV |
| `mediatype:presentation` | PPTX, ODP, PPT |
| `mediatype:pdf` | application/pdf |
| `mediatype:image` | image/* |
| `mediatype:video` | video/* |
| `mediatype:audio` | audio/* |
| `mediatype:archive` | ZIP, GZIP, 7Z, RAR, TAR |

### Boolean operators

```
cat AND dog
cat OR kitten
NOT deleted
name:report AND mtime>=2024-01-01
(cat OR kitten) AND tag:photo
```

### Scope

```
scope:Photos               → limit to a specific space/path
```

### Combined examples

```
tag:vacation AND mediatype:image
content:invoice AND mtime>=2024-01-01
name:"budget" AND mediatype:spreadsheet
mediatype:video AND tag:cat
```

---

## Index Data Structure

Each indexed resource is stored as a document with these fields:

| Field | Type | Description |
|---|---|---|
| `ID` | string | Formatted resource ID (`storageId$spaceId!opaqueId`) |
| `RootID` | string | Space root ID (`storageId$spaceId`) |
| `ParentID` | string | Parent resource ID |
| `Path` | string | Path relative to space root |
| `Type` | uint64 | 1=file, 2=container |
| `Deleted` | bool | True if in trash |
| `Hidden` | bool | True if name starts with `.` |
| `Name` | string | File/folder name (lowercaseKeyword analyzer) |
| `Size` | uint64 | File size in bytes |
| `Mtime` | string | Last modified time (RFC3339Nano) |
| `MimeType` | string | MIME type |
| `Tags` | []string | Tags (lowercaseKeyword analyzer) |
| `Content` | string | Extracted text content (fulltext analyzer) |
| `Title` | string | Document title (from Tika/extractor) |
| `Audio` | object | Audio metadata (album, artist, track, etc.) |
| `Image` | object | Image dimensions (width, height) |
| `Photo` | object | EXIF data (camera, ISO, f-number, GPS, etc.) |
| `Location` | object | GPS coordinates (lat, lon, altitude) |

Deleted items are **not removed** from the index when trashed — they are marked `Deleted=true`. This allows "search trash" functionality. Items are only fully removed on `ItemPurged`.

---

## Metrics & Observability

### Health check

```bash
curl http://127.0.0.1:9224/healthz
```

### Prometheus metrics

```bash
curl http://127.0.0.1:9224/metrics
```

Key metrics:

| Metric | What it tells you |
|---|---|
| `opencloud_search_search_duration_seconds` | Search query latency histogram, broken by status (success/error) |
| `opencloud_search_index_duration_seconds` | IndexSpace operation latency histogram |
| `opencloud_search_events_outstanding_acks` | Events received but not yet acked (should stay near 0) |
| `opencloud_search_events_unprocessed` | Events waiting to be processed |
| `opencloud_search_events_redelivered` | Events NATS is retrying (indicates slow processing) |

### Debug config dump

```bash
curl http://127.0.0.1:9224/config.json
```

Shows the resolved configuration the service is running with — useful to verify env vars were picked up.

### Log level

For debugging indexing issues, set:
```bash
SEARCH_LOG_LEVEL=debug
```

This will log every file path being walked during an `IndexSpace` call:
```
{"level":"debug","path":"./Photos/cat.jpg","message":"Walking tree"}
```

---

## Troubleshooting

### Search service crashes on startup: `error parsing mapping JSON: unexpected end of JSON input`

The Bleve index is corrupt (usually from partial deletion of index files).

```bash
rm -rf ~/opencloud/opencloud-data/search/bleve
# restart container, then reindex
opencloud search index --space "<your-space-id>"
```

### `opencloud search index --space` returns `invalid space id`

The space ID format is wrong or the search service is not running.

1. Check the search service is up: `docker logs opencloud 2>&1 | grep -i "search" | tail -10`
2. Space ID must be `<storageId>$<spaceId>` — find it from storage-users logs
3. If the search service just restarted after a crash, wait a few seconds and retry

### `opencloud search index --all-spaces` returns exit 0 but nothing is indexed

Known bug: `--all-spaces` passes an empty space ID to the gRPC call. The server then tries to `ListStorageSpaces` using the service account. If the service account is not configured or lacks permissions, it gets back an empty list and silently does nothing.

Workaround: use `--space` with an explicit space ID from the storage-users logs.

### New uploads are not being indexed automatically

Check in order:
1. Is NATS running? `docker logs opencloud 2>&1 | grep -i "nats\|events"`
2. Is the event consumer connected? `SEARCH_LOG_LEVEL=debug` will log event processing
3. Does `OC_ASYNC_UPLOADS` in the search service match the setting in storage-users?
   - If storage-users uses async uploads, search must set `SEARCH_EVENTS_ASYNC_UPLOADS=true`
   - Mismatch means the search service listens for the wrong event type

### Vision extraction not happening for new uploads

The vision extractor only runs during `IndexSpace` (full space walk) or when a file event triggers a reindex. To confirm:
1. Check vision service is reachable from inside the container: `curl http://<vision-host>:8384/v1/health`
2. Check `SEARCH_EXTRACTOR_VISION_SERVICE_URL` is set to the correct IP (not a placeholder)
3. Set `SEARCH_LOG_LEVEL=debug` and watch for vision-related log entries during indexing
4. Wipe the bleve index and run a full reindex to force vision extraction on all existing files

### Index exists but search returns no results

```bash
# Check document count via debug endpoint
curl http://127.0.0.1:9224/metrics | grep opencloud_search
```

Or check index size on disk:
```bash
ls -lh /var/lib/opencloud/search/bleve/store/*.zap
```

A `.zap` file present means data was written. If search still returns nothing, the user's JWT may not be resolving to a valid user — check `SEARCH_LOG_LEVEL=warn` for auth errors.
