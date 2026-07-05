package voiceassistant

import (
	"math"
	"sync"
	"time"
)

// VAD is a simple energy-based Voice Activity Detector.
// It uses RMS energy of raw s16le PCM to determine speech/silence.
type VAD struct {
	mu sync.Mutex

	threshold    float64       // RMS energy threshold to consider as speech
	minSpeechMs  time.Duration // minimum speech duration to count
	minSilenceMs time.Duration // silence duration to mark end of utterance
	speechPadMs  time.Duration // padding around detected speech

	speechStarted bool
	speechStart   time.Time
	lastSpeechAt  time.Time
}

// VADConfig configures the voice activity detector.
type VADConfig struct {
	Threshold    float64       // RMS threshold (default: 300.0 for s16le)
	MinSpeechMs  time.Duration // default: 250ms
	MinSilenceMs time.Duration // default: 700ms
	SpeechPadMs  time.Duration // default: 150ms
}

// DefaultVADConfig returns sensible defaults for 96kHz stereo s16le audio.
func DefaultVADConfig() VADConfig {
	return VADConfig{
		Threshold:    300.0,
		MinSpeechMs:  250 * time.Millisecond,
		MinSilenceMs: 700 * time.Millisecond,
		SpeechPadMs:  150 * time.Millisecond,
	}
}

// NewVAD creates a new Voice Activity Detector.
func NewVAD(cfg VADConfig) *VAD {
	return &VAD{
		threshold:    cfg.Threshold,
		minSpeechMs:  cfg.MinSpeechMs,
		minSilenceMs: cfg.MinSilenceMs,
		speechPadMs:  cfg.SpeechPadMs,
	}
}

// DetectSpeech analyzes a chunk of raw s16le PCM audio and returns true if speech is detected.
// pcm must be 16-bit signed little-endian, stereo or mono.
func (v *VAD) DetectSpeech(pcm []byte) bool {
	if len(pcm) < 2 {
		return false
	}

	rms := computeRMS(pcm)

	v.mu.Lock()
	defer v.mu.Unlock()

	if rms >= v.threshold {
		now := time.Now()
		v.lastSpeechAt = now

		if !v.speechStarted {
			v.speechStarted = true
			v.speechStart = now
		}
		return true
	}

	return false
}

// IsUtteranceComplete returns true if enough silence has passed since the last speech
// to consider the utterance finished.
func (v *VAD) IsUtteranceComplete(lastSpeechTime time.Time) bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.speechStarted {
		return false
	}

	// Wait for minimum silence duration after last speech
	silenceDuration := time.Since(lastSpeechTime)
	if silenceDuration < v.minSilenceMs {
		return false
	}

	// Check that speech was long enough
	speechDuration := v.lastSpeechAt.Sub(v.speechStart) + v.speechPadMs
	if speechDuration < v.minSpeechMs {
		// Too short, reset and ignore
		v.speechStarted = false
		return false
	}

	// Utterance complete!
	v.speechStarted = false
	return true
}

// Reset resets the VAD state.
func (v *VAD) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.speechStarted = false
	v.speechStart = time.Time{}
	v.lastSpeechAt = time.Time{}
}

// computeRMS calculates the Root Mean Square of 16-bit signed PCM samples.
// Supports both mono and stereo interleaved formats.
func computeRMS(pcm []byte) float64 {
	nSamples := len(pcm) / 2 // 2 bytes per sample (s16le)
	if nSamples == 0 {
		return 0
	}

	var sum float64
	for i := 0; i < len(pcm)-1; i += 2 {
		// s16le: little-endian signed 16-bit
		sample := int16(pcm[i]) | int16(pcm[i+1])<<8
		sum += float64(sample) * float64(sample)
	}

	return math.Sqrt(sum / float64(nSamples))
}
