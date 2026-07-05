package whisper

/*
#cgo CFLAGS: -I../../../whisper.cpp/include -I../../../whisper.cpp/ggml/include
#cgo linux LDFLAGS: -L . -lwhisper -lggml -lggml-cpu -lggml-base -lstdc++ -lgomp -lm -lpthread
#cgo darwin LDFLAGS: -L . -lwhisper -lggml -lggml-cpu -lggml-base -lc++
#cgo windows LDFLAGS: -L . -lwhisper -lggml -lggml-cpu -lggml-base -lstdc++ -lm

#include "whisper.h"
#include <stdlib.h>

static struct whisper_context* whisper_load(const char* path) {
    struct whisper_context_params params = whisper_context_default_params();
    return whisper_init_from_file_with_params(path, params);
}

static int whisper_process(struct whisper_context* ctx, const float* samples, int n_samples, const char* lang, int n_threads) {
    struct whisper_full_params params = whisper_full_default_params(WHISPER_SAMPLING_GREEDY);
    params.language = lang;
    params.n_threads = n_threads;
    params.print_progress = false;
    params.print_realtime = false;
    params.print_timestamps = false;
    params.print_special = false;
    params.translate = false;
    params.single_segment = true;
    params.no_timestamps = true;
    params.no_context = true;
    return whisper_full(ctx, params, samples, n_samples);
}

static const char* whisper_get_text(struct whisper_context* ctx, int i) {
    return whisper_full_get_segment_text(ctx, i);
}
*/
import "C"
import "unsafe"

// --- Go Wrappers

func loadWhisperModel(path string) *C.struct_whisper_context {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	return C.whisper_load(cPath)
}

func processWhisper(ctx *C.struct_whisper_context, samples []float32, lang string, threads int) int {
	cLang := C.CString(lang)
	defer C.free(unsafe.Pointer(cLang))
	return int(C.whisper_process(ctx, (*C.float)(&samples[0]), C.int(len(samples)), cLang, C.int(threads)))
}

func getWhisperText(ctx *C.struct_whisper_context, index int) string {
	cText := C.whisper_get_text(ctx, C.int(index))
	if cText == nil {
		return ""
	}
	return C.GoString(cText)
}

func freeCString(p unsafe.Pointer) {
	C.free(p)
}