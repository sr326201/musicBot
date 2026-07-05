package provider

import (
	"context"
)

// LLMProvider قرارداد اصلی برای تمام مدل‌ها
type LLMProvider interface {
	// Chat یک درخواست یکپارچه می‌گیرد و پاسخ نهایی را برمی‌گرداند
	Chat(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error)

	// Stream در صورتی که نیاز به استریم کردن جواب‌ها داری (مثلاً برای TTS زنده)
	Stream(ctx context.Context, req types.ChatRequest, outCh chan<- types.ChatStreamChunk) error
}

// types.go
type Message struct {
	Role    string // system, user, assistant, tool
	Content string
}

type ChatRequest struct {
	Messages []Message
	Tools    []ToolDefinition // برای ابزارهایی مثل playback و room
	Model    string
}

type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall // اگر هوش مصنوعی تصمیم گرفت ابزاری را اجرا کند
}
