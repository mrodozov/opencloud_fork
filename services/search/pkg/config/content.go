package config

import "time"

// Extractor defines which extractor to use
type Extractor struct {
	Type             string          `yaml:"type" env:"SEARCH_EXTRACTOR_TYPE" desc:"Defines the content extraction engine. Defaults to 'basic'. Supported values are: 'basic', 'tika', and 'vision'." introductionVersion:"1.0.0"`
	CS3AllowInsecure bool            `yaml:"cs3_allow_insecure" env:"OC_INSECURE;SEARCH_EXTRACTOR_CS3SOURCE_INSECURE" desc:"Ignore untrusted SSL certificates when connecting to the CS3 source." introductionVersion:"1.0.0"`
	Tika             ExtractorTika   `yaml:"tika"`
	Vision           ExtractorVision `yaml:"vision"`
}

// ExtractorTika configures the Tika extractor
type ExtractorTika struct {
	TikaURL        string `yaml:"tika_url" env:"SEARCH_EXTRACTOR_TIKA_TIKA_URL" desc:"URL of the tika server." introductionVersion:"1.0.0"`
	CleanStopWords bool   `yaml:"clean_stop_words" env:"SEARCH_EXTRACTOR_TIKA_CLEAN_STOP_WORDS" desc:"Defines if stop words should be cleaned or not. See the documentation for more details." introductionVersion:"1.0.0"`
}

// ExtractorVision configures the vision extractor that uses the RKNN-based
// vision inference service to generate descriptions and tags for image and
// video files.
type ExtractorVision struct {
	// ServiceURL is the base URL of the vision inference service.
	// Example: http://localhost:8384
	ServiceURL string        `yaml:"service_url" env:"SEARCH_EXTRACTOR_VISION_SERVICE_URL" desc:"Base URL of the vision inference service (e.g. http://localhost:8384)." introductionVersion:"1.0.0"`
	// Timeout for calls to the vision service. Defaults to 60s.
	Timeout    time.Duration `yaml:"timeout" env:"SEARCH_EXTRACTOR_VISION_TIMEOUT" desc:"HTTP timeout for vision service calls (e.g. 60s). Defaults to 60s." introductionVersion:"1.0.0"`
}
