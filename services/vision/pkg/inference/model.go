package inference

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	_ "image/gif"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
)

// Prediction holds a single classification result.
type Prediction struct {
	Label      string
	Confidence float32
}

// Result is the full output of analysing one image.
type Result struct {
	// Tags is the ordered list of top-K label strings.
	Tags []string
	// Predictions contains labels with their confidence scores.
	Predictions []Prediction
	// Description is a natural-language sentence generated from the top predictions.
	Description string
}

// Model wraps an RKNN context and a label list. It is safe for concurrent use
// via an internal mutex (one RKNN context = one NPU command queue at a time).
type Model struct {
	mu          sync.Mutex
	ctx         *Context
	labels      []string
	inputW      int
	inputH      int
	topK        int
	minConf     float32
}

// LoadModel opens the .rknn model file and labels file, initialises the NPU
// context, and returns a ready-to-use Model.
func LoadModel(modelPath, labelsPath string, inputW, inputH, topK int, minConf float32) (*Model, error) {
	modelData, err := os.ReadFile(modelPath)
	if err != nil {
		return nil, fmt.Errorf("vision: reading model %q: %w", modelPath, err)
	}

	ctx, err := Init(modelData)
	if err != nil {
		return nil, fmt.Errorf("vision: init RKNN: %w", err)
	}

	labels, err := loadLabels(labelsPath)
	if err != nil {
		ctx.Destroy()
		return nil, fmt.Errorf("vision: loading labels %q: %w", labelsPath, err)
	}

	return &Model{
		ctx:     ctx,
		labels:  labels,
		inputW:  inputW,
		inputH:  inputH,
		topK:    topK,
		minConf: minConf,
	}, nil
}

// Classify decodes and analyses a single image supplied as raw bytes (JPEG, PNG, GIF).
// It returns the top predictions and a generated description.
func (m *Model) Classify(imgBytes []byte) (Result, error) {
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return Result{}, fmt.Errorf("vision: decoding image: %w", err)
	}

	tensor := resizeBilinearToUint8(img, m.inputW, m.inputH)

	m.mu.Lock()
	logits, err := m.ctx.RunUint8(tensor, m.inputW, m.inputH)
	m.mu.Unlock()
	if err != nil {
		return Result{}, fmt.Errorf("vision: NPU inference: %w", err)
	}

	probs := softmax(logits)
	preds := m.topKPredictions(probs)
	desc := buildDescription(preds)

	tags := make([]string, len(preds))
	for i, p := range preds {
		tags[i] = p.Label
	}

	return Result{
		Tags:        tags,
		Predictions: preds,
		Description: desc,
	}, nil
}

// ClassifyMany classifies multiple images (e.g. video keyframes) and returns
// an aggregated Result by averaging confidence scores across frames.
func (m *Model) ClassifyMany(frames [][]byte) (Result, error) {
	if len(frames) == 0 {
		return Result{}, fmt.Errorf("vision: no frames provided")
	}

	// accumulate per-label confidence sums
	sums := make(map[string]float32)
	counts := 0

	for _, frame := range frames {
		r, err := m.Classify(frame)
		if err != nil {
			// skip unreadable frames rather than failing entirely
			continue
		}
		counts++
		for _, p := range r.Predictions {
			sums[p.Label] += p.Confidence
		}
	}

	if counts == 0 {
		return Result{}, fmt.Errorf("vision: all frames failed inference")
	}

	// average and sort
	averaged := make([]Prediction, 0, len(sums))
	for label, sum := range sums {
		avg := sum / float32(counts)
		if avg >= m.minConf {
			averaged = append(averaged, Prediction{Label: label, Confidence: avg})
		}
	}
	sort.Slice(averaged, func(i, j int) bool {
		return averaged[i].Confidence > averaged[j].Confidence
	})
	if len(averaged) > m.topK {
		averaged = averaged[:m.topK]
	}

	tags := make([]string, len(averaged))
	for i, p := range averaged {
		tags[i] = p.Label
	}

	return Result{
		Tags:        tags,
		Predictions: averaged,
		Description: buildDescription(averaged),
	}, nil
}

// Close releases the underlying NPU context.
func (m *Model) Close() {
	m.ctx.Destroy()
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func loadLabels(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var labels []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			labels = append(labels, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(labels) == 0 {
		return nil, fmt.Errorf("labels file is empty")
	}
	return labels, nil
}

// softmax converts raw logits to a probability distribution.
func softmax(logits []float32) []float32 {
	max := logits[0]
	for _, v := range logits[1:] {
		if v > max {
			max = v
		}
	}
	sum := float32(0)
	exps := make([]float32, len(logits))
	for i, v := range logits {
		e := float32(math.Exp(float64(v - max)))
		exps[i] = e
		sum += e
	}
	for i := range exps {
		exps[i] /= sum
	}
	return exps
}

// topKPredictions maps probabilities to labels and returns the top-K above minConf.
func (m *Model) topKPredictions(probs []float32) []Prediction {
	type idxConf struct {
		idx  int
		conf float32
	}
	candidates := make([]idxConf, 0, len(probs))
	for i, p := range probs {
		if i < len(m.labels) && p >= m.minConf {
			candidates = append(candidates, idxConf{i, p})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].conf > candidates[j].conf
	})
	if len(candidates) > m.topK {
		candidates = candidates[:m.topK]
	}
	preds := make([]Prediction, len(candidates))
	for i, c := range candidates {
		preds[i] = Prediction{
			Label:      normaliseLabel(m.labels[c.idx]),
			Confidence: c.conf,
		}
	}
	return preds
}

// normaliseLabel converts underscore-separated ImageNet labels to human-readable form.
// E.g. "golden_retriever" → "golden retriever"
func normaliseLabel(s string) string {
	// ImageNet labels can look like "n02099601 golden_retriever"; strip the synset ID.
	if idx := strings.Index(s, " "); idx != -1 {
		s = s[idx+1:]
	}
	return strings.ReplaceAll(s, "_", " ")
}

// buildDescription generates a natural-language sentence from top predictions.
func buildDescription(preds []Prediction) string {
	if len(preds) == 0 {
		return "an image"
	}

	labels := make([]string, 0, len(preds))
	for _, p := range preds {
		labels = append(labels, p.Label)
	}

	switch len(labels) {
	case 1:
		return fmt.Sprintf("An image containing %s", labels[0])
	case 2:
		return fmt.Sprintf("An image containing %s and %s", labels[0], labels[1])
	default:
		last := labels[len(labels)-1]
		rest := strings.Join(labels[:len(labels)-1], ", ")
		return fmt.Sprintf("An image containing %s, and %s", rest, last)
	}
}

// resizeBilinearToUint8 resizes img to (w, h) using bilinear interpolation and
// returns the pixel data as a flat []byte in NHWC/RGB order ready for RKNN.
func resizeBilinearToUint8(src image.Image, w, h int) []byte {
	srcBounds := src.Bounds()
	srcW := srcBounds.Max.X - srcBounds.Min.X
	srcH := srcBounds.Max.Y - srcBounds.Min.Y

	out := make([]byte, h*w*3)
	xScale := float64(srcW) / float64(w)
	yScale := float64(srcH) / float64(h)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// source pixel (bilinear sample point)
			sx := (float64(x)+0.5)*xScale - 0.5
			sy := (float64(y)+0.5)*yScale - 0.5

			x0 := int(math.Floor(sx))
			y0 := int(math.Floor(sy))
			x1 := x0 + 1
			y1 := y0 + 1

			// clamp to source bounds
			if x0 < 0 {
				x0 = 0
			}
			if y0 < 0 {
				y0 = 0
			}
			if x1 >= srcW {
				x1 = srcW - 1
			}
			if y1 >= srcH {
				y1 = srcH - 1
			}

			dx := sx - math.Floor(sx)
			dy := sy - math.Floor(sy)

			// sample 4 neighbours
			c00 := toRGB(src.At(srcBounds.Min.X+x0, srcBounds.Min.Y+y0))
			c10 := toRGB(src.At(srcBounds.Min.X+x1, srcBounds.Min.Y+y0))
			c01 := toRGB(src.At(srcBounds.Min.X+x0, srcBounds.Min.Y+y1))
			c11 := toRGB(src.At(srcBounds.Min.X+x1, srcBounds.Min.Y+y1))

			idx := (y*w + x) * 3
			for c := 0; c < 3; c++ {
				v := (1-dx)*(1-dy)*float64(c00[c]) +
					dx*(1-dy)*float64(c10[c]) +
					(1-dx)*dy*float64(c01[c]) +
					dx*dy*float64(c11[c])
				out[idx+c] = uint8(math.Round(v))
			}
		}
	}
	return out
}

// toRGB converts any color to [3]uint8 {R, G, B}.
func toRGB(c color.Color) [3]uint8 {
	r, g, b, _ := c.RGBA()
	return [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}
