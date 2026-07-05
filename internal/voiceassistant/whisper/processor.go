package whisper

/*
#include "whisper.h"
*/
import "C"
import (
	"fmt"
	"strings"
	"sync"
)

type Processor struct {
	model    *Model
	nThreads int
	mu       sync.Mutex
}

func NewProcessor(model *Model, nThreads int) *Processor {
	if nThreads <= 0 {
		nThreads = 4
	}
	return &Processor{model: model, nThreads: nThreads}
}

func (p *Processor) Transcribe(samples []float32, language string) (string, error) {
	if p.model == nil || p.model.ctx == nil {
		return "", fmt.Errorf("whisper model not loaded")
	}
	if len(samples) == 0 {
		return "", nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	ret := processWhisper(p.model.ctx, samples, language, p.nThreads)
	if ret != 0 {
		return "", fmt.Errorf("whisper_full failed with code %d", ret)
	}

	nSegments := int(C.whisper_full_n_segments(p.model.ctx))
	var sb strings.Builder
	for i := 0; i < nSegments; i++ {
		text := getWhisperText(p.model.ctx, i)
		if text != "" {
			sb.WriteString(text)
		}
	}

	return strings.TrimSpace(sb.String()), nil
}
