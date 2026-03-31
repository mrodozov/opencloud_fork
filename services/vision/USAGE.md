# Vision Service — Usage Guide

## What it does

AI-powered image/video analysis running on the **Rockchip RK3566 NPU** (Odroid-M1S).
Classifies images/video frames using RKNN models and returns tags + descriptions.
Integrates into the Search service to enrich file metadata automatically.

---

## Requirements

- Rockchip RK3566 NPU hardware (Odroid-M1S)
- `librknnrt.so` on library path (`/usr/lib` or `/usr/local/lib`)
- Pre-converted `.rknn` model file (converted with rknn-toolkit2 targeting rk3566)
- ImageNet labels text file (1000 lines)
- `ffmpeg` / `ffprobe` binaries (for video support)

---

## Running it

```bash
vision --model /opt/vision/model.rknn --labels /opt/vision/labels.txt
```

Or via environment variables:

```bash
VISION_MODEL_PATH=/opt/vision/model.rknn \
VISION_LABELS_PATH=/opt/vision/labels.txt \
VISION_HTTP_ADDR=:8384 \
vision
```

---

## Configuration (environment variables)

| Variable                          | Default      | Description                          |
|-----------------------------------|--------------|--------------------------------------|
| `VISION_MODEL_PATH`               | *(required)* | Path to `.rknn` model file           |
| `VISION_LABELS_PATH`              | *(required)* | Path to labels text file             |
| `VISION_HTTP_ADDR`                | `:8384`      | HTTP listen address                  |
| `VISION_MODEL_INPUT_WIDTH`        | `224`        | Model input width (pixels)           |
| `VISION_MODEL_INPUT_HEIGHT`       | `224`        | Model input height (pixels)          |
| `VISION_MODEL_TOP_K`              | `5`          | Number of top predictions to return  |
| `VISION_MODEL_CONFIDENCE_THRESHOLD` | `0.05`    | Minimum confidence score to include  |
| `VISION_LOG_LEVEL`                | `info`       | Log level (debug/info/warn/error)    |
| `VISION_LOG_PRETTY`               | —            | Enable pretty-printed logs           |

---

## API Endpoints

### POST `/v1/analyze/image`
Send raw image bytes (JPEG, PNG, GIF) in the request body.

```bash
curl -X POST http://localhost:8384/v1/analyze/image \
  --data-binary @photo.jpg
```

Response:
```json
{
  "description": "An image containing golden retriever and dog",
  "tags": ["golden retriever", "dog"],
  "predictions": [
    { "label": "golden retriever", "confidence": 0.95 },
    { "label": "dog", "confidence": 0.92 }
  ]
}
```

### POST `/v1/analyze/video`
Send raw video bytes in the request body. Extracts up to 8 keyframes and classifies each.

```bash
curl -X POST http://localhost:8384/v1/analyze/video \
  --data-binary @video.mp4
```

Response:
```json
{
  "description": "An image containing forest and tree",
  "tags": ["forest", "tree"],
  "predictions": [...],
  "keyframe_count": 8
}
```

### GET `/healthz`
Liveness probe — returns `"ok"` with HTTP 200.

---

## Image size

You do NOT need to pre-resize images. The service automatically downscales any image
to the model's input size (224x224 by default) using bilinear interpolation before
running inference. A 4000x3000 photo works fine.

**Note on aspect ratio:** The resize is a stretch, not a crop or letterbox. Very wide
or very tall images get squished to a square. This rarely affects classification quality
since MobileNetV3 was trained on similarly preprocessed data.

---

## Integration with Search Service

Set these env vars in the Search service to enable automatic vision-based tagging:

```bash
SEARCH_EXTRACTOR_TYPE=vision
SEARCH_EXTRACTOR_VISION_SERVICE_URL=http://localhost:8384
SEARCH_EXTRACTOR_VISION_TIMEOUT=60s   # optional
```

When Search indexes image or video files it posts them to the Vision service and merges
the returned tags/descriptions into the search document (full-text searchable).

---

## Preparing the RKNN model (MobileNetV3)

### Step 1 — Export from PyTorch to ONNX

```python
import torch
import torchvision.models as models

model = models.mobilenet_v3_large(pretrained=True)
model.eval()

dummy = torch.randn(1, 3, 224, 224)
torch.onnx.export(model, dummy, 'mobilenet_v3_large.onnx',
                  input_names=['input'], output_names=['output'],
                  opset_version=11)
```

### Step 2 — Convert ONNX to RKNN

```python
from rknn.api import RKNN

rknn = RKNN()

rknn.config(
    mean_values=[[123.675, 116.28, 103.53]],
    std_values=[[58.395, 57.12, 57.375]],
    target_platform='rk3566'
)

rknn.load_onnx(model='mobilenet_v3_large.onnx')
rknn.build(do_quantization=False)
rknn.export_rknn('./mobilenet_v3.rknn')
rknn.release()
```

### Step 3 — Get the ImageNet labels file

```bash
wget https://raw.githubusercontent.com/pytorch/hub/master/imagenet_classes.txt -O labels.txt
```
