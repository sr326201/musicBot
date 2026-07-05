package voiceassistant

import (
	"encoding/binary"
	"math"
)

// Whisper expects float32 PCM at 16kHz mono.
// ntgcalls delivers s16le at 96kHz stereo.
// This file converts between the two formats.

const (
	whisperSampleRate = 16000
	ntgSampleRate     = 96000
	ntgChannels       = 2
)

// convertS16LEToFloat32Mono converts interleaved s16le PCM (stereo or mono)
// to float32 mono at 16kHz, suitable for whisper.cpp.
//
// Steps:
//  1. Decode s16le bytes to int16 samples
//  2. If stereo, average L+R channels to mono
//  3. Downsample from 96kHz to 16kHz (factor = 6)
//  4. Normalize int16 [-32768, 32767] → float32 [-1.0, 1.0]
func convertS16LEToFloat32Mono(pcm []byte, srcSampleRate, channels int) []float32 {
	nSamples := len(pcm) / 2
	if nSamples == 0 {
		return nil
	}

	// Step 1 & 2: decode to mono float32 at source sample rate
	var monoFloats []float32
	if channels == 2 {
		monoFloats = make([]float32, 0, nSamples/2)
		for i := 0; i < len(pcm)-3; i += 4 { // 2 channels × 2 bytes
			left := int16(pcm[i]) | int16(pcm[i+1])<<8
			right := int16(pcm[i+2]) | int16(pcm[i+3])<<8
			avg := (float64(left) + float64(right)) / 2.0
			monoFloats = append(monoFloats, float32(avg/32768.0))
		}
	} else {
		monoFloats = make([]float32, 0, nSamples)
		for i := 0; i < len(pcm)-1; i += 2 {
			sample := int16(pcm[i]) | int16(pcm[i+1])<<8
			monoFloats = append(monoFloats, float32(float64(sample)/32768.0))
		}
	}

	// Step 3: downsample to 16kHz
	if srcSampleRate == whisperSampleRate {
		return monoFloats
	}

	factor := float64(srcSampleRate) / float64(whisperSampleRate)
	outLen := int(float64(len(monoFloats)) / factor)
	out := make([]float32, outLen)

	for i := 0; i < outLen; i++ {
		srcIdx := float64(i) * factor
		idx0 := int(srcIdx)
		frac := srcIdx - float64(idx0)

		if idx0 >= len(monoFloats)-1 {
			out[i] = monoFloats[len(monoFloats)-1]
		} else {
			// Linear interpolation
			out[i] = monoFloats[idx0]*(1-float32(frac)) + monoFloats[idx0+1]*float32(frac)
		}
	}

	return out
}

// convertS16LEToBytes is a helper that encodes float32 samples back to s16le bytes.
// Useful if you need to feed processed audio back to ntgcalls.
func convertS16LEToBytes(samples []float32) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		clamped := math.Max(-1.0, math.Min(1.0, float64(s)))
		v := int16(clamped * 32767)
		binary.LittleEndian.PutUint16(out[i*2:i*2+2], uint16(v))
	}
	return out
}
