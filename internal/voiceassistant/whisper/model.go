package whisper

/*
#include "whisper.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/Laky-64/gologging"
)

type Model struct {
	ctx *C.struct_whisper_context
	mu  sync.Mutex
}

func NewModel(modelPath string) (*Model, error) {
	if modelPath == "" {
		return nil, fmt.Errorf("whisper model path is empty")
	}

	gologging.InfoF("[whisper] loading model from %s", modelPath)
	ctx := loadWhisperModel(modelPath)
	if ctx == nil {
		return nil, fmt.Errorf("failed to load whisper model from %s", modelPath)
	}

	gologging.Info("[whisper] model loaded successfully")
	return &Model{ctx: ctx}, nil
}

func (m *Model) IsMultilingual() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil {
		return false
	}
	return C.whisper_is_multilingual(m.ctx) != 0
}

func LanguageID(lang string) int {
	cLang := C.CString(lang)
	defer C.free(unsafe.Pointer(cLang))
	return int(C.whisper_lang_id(cLang))
}

func (m *Model) Free() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx != nil {
		C.whisper_free(m.ctx)
		m.ctx = nil
	}
}
