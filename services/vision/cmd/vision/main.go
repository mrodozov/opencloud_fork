// Command vision is a standalone HTTP service that runs image and video
// classification on the Rockchip RK3566 NPU (Odroid-M1S) via the RKNN runtime.
//
// Usage:
//
//	vision --model /opt/vision/model.rknn --labels /opt/vision/labels.txt
//
// Environment variables (see config package for full list):
//
//	VISION_MODEL_PATH   – path to the .rknn model file
//	VISION_LABELS_PATH  – path to the labels text file
//	VISION_HTTP_ADDR    – listen address (default :8384)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opencloud-eu/opencloud/services/vision/pkg/config"
	"github.com/opencloud-eu/opencloud/services/vision/pkg/inference"
	"github.com/opencloud-eu/opencloud/services/vision/pkg/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vision:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.DefaultConfig()

	// simple flag layer on top of the config struct
	flag.StringVar(&cfg.HTTP.Addr, "addr", cfg.HTTP.Addr, "HTTP listen address")
	flag.StringVar(&cfg.Model.ModelPath, "model", envOrDefault("VISION_MODEL_PATH", ""), "Path to .rknn model file")
	flag.StringVar(&cfg.Model.LabelsPath, "labels", envOrDefault("VISION_LABELS_PATH", ""), "Path to labels text file")
	flag.IntVar(&cfg.Model.InputWidth, "input-width", cfg.Model.InputWidth, "Model input width")
	flag.IntVar(&cfg.Model.InputHeight, "input-height", cfg.Model.InputHeight, "Model input height")
	flag.IntVar(&cfg.Model.TopK, "top-k", cfg.Model.TopK, "Number of top predictions")
	flag.Parse()

	// allow env vars to override flags not explicitly set
	if v := os.Getenv("VISION_HTTP_ADDR"); v != "" {
		cfg.HTTP.Addr = v
	}

	if cfg.Model.ModelPath == "" {
		return errors.New("--model / VISION_MODEL_PATH is required")
	}
	if cfg.Model.LabelsPath == "" {
		return errors.New("--labels / VISION_LABELS_PATH is required")
	}

	fmt.Printf("vision: loading model %s\n", cfg.Model.ModelPath)
	model, err := inference.LoadModel(
		cfg.Model.ModelPath,
		cfg.Model.LabelsPath,
		cfg.Model.InputWidth,
		cfg.Model.InputHeight,
		cfg.Model.TopK,
		cfg.Model.ConfidenceThreshold,
	)
	if err != nil {
		return fmt.Errorf("loading model: %w", err)
	}
	defer model.Close()
	fmt.Println("vision: model loaded successfully")

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      server.New(model, 256, 8),
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		fmt.Printf("vision: listening on %s\n", cfg.HTTP.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "vision: server error:", err)
		}
	}()

	<-quit
	fmt.Println("vision: shutting down …")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
