// Package server implements the HTTP API for the vision inference service.
//
// Endpoints:
//
//	POST /v1/analyze/image  – analyse a single image (JPEG, PNG, GIF)
//	POST /v1/analyze/video  – extract keyframes from a video and analyse them
//
// Both endpoints accept the raw file bytes as the request body and return JSON.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/opencloud-eu/opencloud/services/vision/pkg/inference"
	"github.com/opencloud-eu/opencloud/services/vision/pkg/video"
)

// AnalyseResponse is the JSON body returned for both image and video requests.
type AnalyseResponse struct {
	Description   string                  `json:"description"`
	Tags          []string                `json:"tags"`
	Predictions   []inference.Prediction  `json:"predictions"`
	KeyframeCount int                     `json:"keyframe_count,omitempty"`
}

// ErrorResponse is returned on failure.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Server holds the HTTP mux and the loaded model.
type Server struct {
	model      *inference.Model
	maxBodyMB  int64
	maxFrames  int
	mux        *http.ServeMux
}

// New creates a Server wired to the given model.
// maxBodyMB limits the maximum accepted request body (default 256 MB).
// maxFrames is the maximum number of keyframes to extract per video.
func New(model *inference.Model, maxBodyMB int64, maxFrames int) *Server {
	if maxBodyMB <= 0 {
		maxBodyMB = 256
	}
	if maxFrames <= 0 {
		maxFrames = video.DefaultMaxKeyframes
	}

	s := &Server{
		model:     model,
		maxBodyMB: maxBodyMB,
		maxFrames: maxFrames,
		mux:       http.NewServeMux(),
	}
	s.mux.HandleFunc("/v1/analyze/image", s.handleImage)
	s.mux.HandleFunc("/v1/analyze/video", s.handleVideo)
	s.mux.HandleFunc("/healthz", s.handleHealth)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleImage accepts raw image bytes and returns classification results.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	data, err := s.readBody(r)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}

	result, err := s.model.Classify(data)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("classify: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, AnalyseResponse{
		Description: result.Description,
		Tags:        result.Tags,
		Predictions: result.Predictions,
	})
}

// handleVideo extracts keyframes from the uploaded video and returns
// aggregated classification results across all frames.
func (s *Server) handleVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	data, err := s.readBody(r)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}

	frames, err := video.ExtractKeyframes(data, s.maxFrames)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("keyframe extraction: %v", err))
		return
	}

	result, err := s.model.ClassifyMany(frames)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("classify frames: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, AnalyseResponse{
		Description:   result.Description,
		Tags:          result.Tags,
		Predictions:   result.Predictions,
		KeyframeCount: len(frames),
	})
}

// handleHealth returns 200 OK for liveness probes.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// readBody reads the request body up to maxBodyMB megabytes.
func (s *Server) readBody(r *http.Request) ([]byte, error) {
	limit := s.maxBodyMB * 1024 * 1024
	lr := &io.LimitedReader{R: r.Body, N: limit + 1}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if lr.N == 0 {
		return nil, fmt.Errorf("request body exceeds %d MB limit", s.maxBodyMB)
	}
	return data, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: strings.TrimSpace(msg)})
}
