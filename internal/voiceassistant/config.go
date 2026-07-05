package voiceassistant

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Laky-64/gologging"
)

// Config holds voice assistant configuration
type Config struct {
	Enabled        bool
	ModelPath      string
	Language       string // "auto", "en", "fa", etc.
	NThreads       int
	TranscribeChan chan TranscriptionResult // chatID → result broadcast
}

// TranscriptionResult is the output of a transcription pass
type TranscriptionResult struct {
	ChatID int64
	Text   string
	Error  error
}

// DefaultConfig returns the default voice assistant configuration.
// Model path: <cwd>/whisper.cpp/models/ggml-base.bin
func DefaultConfig() Config {
	modelPath := os.Getenv("WHISPER_MODEL_PATH")
	if modelPath == "" {
		modelPath = filepath.Join("whisper.cpp", "models", "ggml-small.bin")
	}

	lang := os.Getenv("WHISPER_LANGUAGE")
	if lang == "" {
		lang = "auto"
	}

	nThreads := 4
	if v := os.Getenv("WHISPER_N_THREADS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			nThreads = n
		}
	}

	enabled := true
	if v := os.Getenv("VOICE_ASSISTANT_ENABLED"); v != "" {
		enabled = strings.ToLower(v) == "true" || v == "1"
	}

	return Config{
		Enabled:        enabled,
		ModelPath:      modelPath,
		Language:       lang,
		NThreads:       nThreads,
		TranscribeChan: make(chan TranscriptionResult, 64),
	}
}

func init() {
	l := gologging.GetLogger("voiceassistant")
	l.SetLevel(gologging.DebugLevel)
}
