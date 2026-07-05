package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/joho/godotenv"
)

type BotResponse struct {
	Action   string `json:"action"`
	Response string `json:"response"`
}

func askAI(client *openrouter.OpenRouter, text string) (BotResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := client.Chat.Send(ctx, components.ChatRequest{
		Model: openrouter.Pointer("cohere/north-mini-code:free"),
		Messages: []components.ChatMessages{
			components.CreateChatMessagesSystem(
				components.ChatSystemMessage{
					Role: components.ChatSystemMessageRoleSystem,
					Content: components.CreateChatSystemMessageContentStr(
						`You are a Telegram command router.

Return ONLY valid minified JSON:

{
  "action": "restart_server | stop_server | show_stats | get_logs | unknown",
  "response": "short user friendly Persian message"
}

Rules:
- NO markdown
- NO extra keys
- ALWAYS return valid JSON`,
					),
				},
			),
			components.CreateChatMessagesUser(
				components.ChatUserMessage{
					Role:    components.ChatUserMessageRoleUser,
					Content: components.CreateChatUserMessageContentStr(text),
				},
			),
		},
	}, nil)
	if err != nil {
		return BotResponse{}, err
	}

	if resp.ChatResult == nil || len(resp.ChatResult.Choices) == 0 {
		return BotResponse{}, fmt.Errorf("empty response")
	}

	msg := resp.ChatResult.Choices[0].Message

	val, ok := msg.Content.Get()
	if !ok || val == nil || val.Str == nil {
		return BotResponse{}, fmt.Errorf("empty content")
	}

	rawJSON := strings.TrimSpace(*val.Str)

	var result BotResponse
	if err := json.Unmarshal([]byte(rawJSON), &result); err != nil {
		return BotResponse{}, fmt.Errorf("invalid JSON from model: %w | raw: %s", err, rawJSON)
	}

	return result, nil
}

func main() {
	_ = godotenv.Load()

	client := openrouter.New(
		openrouter.WithSecurity(os.Getenv("API_KEY")),
	)

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("Bot ready. Type your message. Type 'exit' to quit.")

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if text == "exit" || text == "quit" {
			break
		}

		result, err := askAI(client, text)
		if err != nil {
			log.Println("error:", err)
			continue
		}

		fmt.Println("ACTION:", result.Action)
		fmt.Println("RESPONSE:", result.Response)

		switch result.Action {
		case "restart_server":
			fmt.Println(">> restarting server...")

		case "stop_server":
			fmt.Println(">> stopping server...")

		case "show_stats":
			fmt.Println(">> showing stats...")

		case "get_logs":
			fmt.Println(">> getting logs...")

		default:
			fmt.Println(">> unknown action")
		}
	}
}
