package voiceassistant

// Transcriber converts raw PCM audio to text
type Transcriber interface {
	// Transcribe processes PCM audio and returns transcribed text
	Transcribe(pcmData []byte, sampleRate int, channels int) (string, error)

	// TranscribeWithContext provides conversation context for better accuracy
	TranscribeWithContext(pcmData []byte, ctx TranscriptionContext) (string, error)

	// Close releases resources
	Close() error
}

type TranscriptionContext struct {
	Language    string   // e.g., "fa", "en", "auto"
	PrevTexts   []string // previous transcriptions for context
	AudioFormat AudioFormat
}

type AudioFormat struct {
	SampleRate int
	Channels   int
	BitDepth   int // always 16 (s16le)
}
