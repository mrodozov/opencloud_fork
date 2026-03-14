package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gateway "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/search/pkg/config"
	"github.com/opencloud-eu/reva/v2/pkg/rgrpc/todo/pool"
)

// imageMIMEPrefixes are the MIME-type prefixes this extractor handles.
var imageMIMEPrefixes = []string{"image/"}

// videoMIMEPrefixes are the video MIME-type prefixes this extractor handles.
var videoMIMEPrefixes = []string{"video/"}

// visionResponse mirrors the JSON body returned by the vision service.
type visionResponse struct {
	Description   string           `json:"description"`
	Tags          []string         `json:"tags"`
	KeyframeCount int              `json:"keyframe_count,omitempty"`
}

// Vision is an Extractor that enriches image and video Documents with
// AI-generated descriptions and tags by calling the vision inference service.
// Text content and audio metadata are handled by the embedded Basic extractor.
type Vision struct {
	*Basic
	Retriever
	httpClient     http.Client
	imageEndpoint  string
	videoEndpoint  string
	sizeLimit      uint64
	logger         log.Logger
}

// NewVisionExtractor creates a Vision extractor.
func NewVisionExtractor(
	gatewaySelector pool.Selectable[gateway.GatewayAPIClient],
	logger log.Logger,
	cfg *config.Config,
) (*Vision, error) {
	basic, err := NewBasicExtractor(logger)
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(cfg.Extractor.Vision.ServiceURL, "/")
	if base == "" {
		return nil, fmt.Errorf("vision extractor: SEARCH_EXTRACTOR_VISION_SERVICE_URL is not set")
	}

	timeout := cfg.Extractor.Vision.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	return &Vision{
		Basic:         basic,
		Retriever:     newCS3Retriever(gatewaySelector, logger, cfg.Extractor.CS3AllowInsecure),
		httpClient:    http.Client{Timeout: timeout},
		imageEndpoint: base + "/v1/analyze/image",
		videoEndpoint: base + "/v1/analyze/video",
		sizeLimit:     cfg.ContentExtractionSizeLimit,
		logger:        logger,
	}, nil
}

// Extract returns a Document enriched with AI-generated content for image and
// video files. Other file types fall through to the Basic extractor.
func (v *Vision) Extract(ctx context.Context, ri *provider.ResourceInfo) (Document, error) {
	doc, err := v.Basic.Extract(ctx, ri)
	if err != nil {
		return doc, err
	}

	if ri.Size == 0 || ri.Type != provider.ResourceType_RESOURCE_TYPE_FILE {
		return doc, nil
	}

	if !isImage(ri.MimeType) && !isVideo(ri.MimeType) {
		return doc, nil
	}

	if v.sizeLimit > 0 && ri.Size > v.sizeLimit {
		v.logger.Info().
			Interface("ResourceID", ri.Id).
			Str("Name", ri.Name).
			Msg("file exceeds content extraction size limit, skipping vision analysis")
		return doc, nil
	}

	data, err := v.Retrieve(ctx, ri.Id)
	if err != nil {
		return doc, err
	}
	defer data.Close()

	fileBytes, err := io.ReadAll(data)
	if err != nil {
		return doc, fmt.Errorf("vision: reading file: %w", err)
	}

	endpoint := v.imageEndpoint
	if isVideo(ri.MimeType) {
		endpoint = v.videoEndpoint
	}

	vr, err := v.callVisionService(ctx, endpoint, fileBytes)
	if err != nil {
		// log and degrade gracefully — the basic metadata is still useful
		v.logger.Warn().
			Err(err).
			Interface("ResourceID", ri.Id).
			Str("Name", ri.Name).
			Msg("vision service call failed, skipping content enrichment")
		return doc, nil
	}

	// Append the generated description to Content so it becomes full-text searchable.
	if vr.Description != "" {
		doc.Content = strings.TrimSpace(doc.Content + " " + vr.Description)
	}

	// Merge AI-generated tags with any existing tags (e.g. user-added tags).
	doc.Tags = mergeTags(doc.Tags, vr.Tags)

	return doc, nil
}

// callVisionService posts fileBytes to the vision service and parses the response.
func (v *Vision) callVisionService(ctx context.Context, endpoint string, fileBytes []byte) (*visionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(fileBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vision service request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("vision service returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var vr visionResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return nil, fmt.Errorf("decoding vision response: %w", err)
	}
	return &vr, nil
}

// isImage reports whether a MIME type refers to an image.
func isImage(mime string) bool {
	return hasMIMEPrefix(mime, imageMIMEPrefixes)
}

// isVideo reports whether a MIME type refers to a video.
func isVideo(mime string) bool {
	return hasMIMEPrefix(mime, videoMIMEPrefixes)
}

func hasMIMEPrefix(mime string, prefixes []string) bool {
	mime = strings.ToLower(mime)
	for _, p := range prefixes {
		if strings.HasPrefix(mime, p) {
			return true
		}
	}
	return false
}

// mergeTags returns a deduplicated union of existing and new tags.
func mergeTags(existing, newTags []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(newTags))
	result := make([]string, 0, len(existing)+len(newTags))
	for _, t := range existing {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			result = append(result, t)
		}
	}
	for _, t := range newTags {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			result = append(result, t)
		}
	}
	return result
}
