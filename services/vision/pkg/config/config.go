package config

// Config holds all configuration for the vision inference service.
type Config struct {
	HTTP    HTTP    `yaml:"http"`
	Model   Model   `yaml:"model"`
	Log     Log     `yaml:"log"`
}

// HTTP configures the HTTP server.
type HTTP struct {
	Addr string `yaml:"addr" env:"VISION_HTTP_ADDR" desc:"Address the vision service listens on." default:":8384"`
}

// Model configures the RKNN model and inference parameters.
type Model struct {
	// Path to the .rknn model file (classification model, e.g. MobileNetV3 or EfficientNet-Lite).
	ModelPath string `yaml:"model_path" env:"VISION_MODEL_PATH" desc:"Path to the RKNN classification model file (.rknn)."`

	// Path to a text file with one ImageNet label per line (1000 lines for ImageNet-1K).
	LabelsPath string `yaml:"labels_path" env:"VISION_LABELS_PATH" desc:"Path to the labels text file, one label per line."`

	// Input dimensions expected by the model. Most classification models use 224x224.
	InputWidth  int `yaml:"input_width"  env:"VISION_MODEL_INPUT_WIDTH"  desc:"Model input image width in pixels."  default:"224"`
	InputHeight int `yaml:"input_height" env:"VISION_MODEL_INPUT_HEIGHT" desc:"Model input image height in pixels." default:"224"`

	// How many top predictions to return as tags.
	TopK int `yaml:"top_k" env:"VISION_MODEL_TOP_K" desc:"Number of top predictions to return as tags." default:"5"`

	// Minimum confidence (0-1) for a label to be included in the output.
	ConfidenceThreshold float32 `yaml:"confidence_threshold" env:"VISION_MODEL_CONFIDENCE_THRESHOLD" desc:"Minimum prediction confidence to include a label." default:"0.05"`
}

// Log configures logging.
type Log struct {
	Level  string `yaml:"level"  env:"VISION_LOG_LEVEL"  desc:"Log level (debug, info, warn, error)." default:"info"`
	Pretty bool   `yaml:"pretty" env:"VISION_LOG_PRETTY" desc:"Enable pretty-printed log output."`
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() Config {
	return Config{
		HTTP: HTTP{
			Addr: ":8384",
		},
		Model: Model{
			InputWidth:          224,
			InputHeight:         224,
			TopK:                5,
			ConfidenceThreshold: 0.05,
		},
		Log: Log{
			Level: "info",
		},
	}
}
