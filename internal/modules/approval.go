package modules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amarnathcjd/gogram/telegram"
	tg "github.com/amarnathcjd/gogram/telegram"

	"main/internal/database"
	"main/internal/locales"
)

const pageSize = 8

func buildGroupsKeyboard(chatID int64, page int) (*telegram.ReplyInlineMarkup, string, error) {
	chats, err := database.ServedChats()
	if err != nil {
		return nil, "", err
	}

	var groups []int64
	for _, id := range chats {
		if id < 0 || id >= 4000000000 {
			groups = append(groups, id)
		}
	}

	totalGroups := len(groups)
	if totalGroups == 0 {
		return nil, F(chatID, "allgroups_empty"), nil
	}

	totalPages := (totalGroups + pageSize - 1) / pageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * pageSize
	end := start + pageSize
	if end > totalGroups {
		end = totalGroups
	}

	kb := tg.NewKeyboard()

	for _, chatIDGrp := range groups[start:end] {
		status := "🔴"
		approved, _ := database.IsApprovedChat(chatIDGrp)
		if approved {
			status = "🟢"
		}

		btnText := fmt.Sprintf("%s %d", status, chatIDGrp)
		btnData := fmt.Sprintf("tgl_app_%d_%d", chatIDGrp, page)

		kb.AddRow(
			tg.Button.Data(btnText, btnData),
		)
	}

	var navRow []tg.KeyboardButton
	if page > 0 {
		navRow = append(navRow, tg.Button.Data(F(chatID, "approval_nav_prev"), fmt.Sprintf("chats_pg_%d", page-1)))
	}

	navRow = append(navRow, tg.Button.Data(F(chatID, "approval_nav_page", locales.Arg{"page": page + 1, "total": totalPages}), "noop"))

	if end < totalGroups {
		navRow = append(navRow, tg.Button.Data(F(chatID, "approval_nav_next"), fmt.Sprintf("chats_pg_%d", page+1)))
	}

	if len(navRow) > 0 {
		kb.AddRow(navRow...)
	}

	text := F(chatID, "allgroups_title")
	return kb.Build(), text, nil
}

func handleAllGroups(m *telegram.NewMessage) error {
	if !isOwnerOrSudo(m.SenderID()) {
		return telegram.ErrEndGroup
	}

	markup, text, err := buildGroupsKeyboard(m.ChatID(), 0)
	if err != nil {
		m.Reply("❌ Error: " + err.Error())
		return telegram.ErrEndGroup
	}

	m.Reply(text, &telegram.SendOptions{
		ParseMode:   "HTML",
		ReplyMarkup: markup,
	})
	return telegram.ErrEndGroup
}

func handleGroupManagementCallbacks(cb *telegram.CallbackQuery) error {
	if !isOwnerOrSudo(cb.SenderID) {
		cb.Answer(F(cb.ChatID, "approval_mgmt_denied"), &telegram.CallbackOptions{Alert: true})
		return telegram.ErrEndGroup
	}

	data := cb.DataString()

	if strings.HasPrefix(data, "chats_pg_") {
		pageStr := strings.TrimPrefix(data, "chats_pg_")

		page, err := strconv.Atoi(pageStr)
		if err != nil {
			return telegram.ErrEndGroup
		}

		markup, text, err := buildGroupsKeyboard(cb.ChatID, page)
		if err != nil {
			cb.Answer(F(cb.ChatID, "approval_error_loading"), &telegram.CallbackOptions{Alert: true})
			return telegram.ErrEndGroup
		}

		cb.Edit(text, &telegram.SendOptions{ParseMode: "HTML", ReplyMarkup: markup})
		cb.Answer("", &telegram.CallbackOptions{})
		return telegram.ErrEndGroup
	}

	if strings.HasPrefix(data, "tgl_app_") {
		parts := strings.Split(strings.TrimPrefix(data, "tgl_app_"), "_")
		if len(parts) != 2 {
			return telegram.ErrEndGroup
		}

		chatID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return telegram.ErrEndGroup
		}

		page, err := strconv.Atoi(parts[1])
		if err != nil {
			return telegram.ErrEndGroup
		}

		approved, _ := database.IsApprovedChat(chatID)
		if approved {
			database.RemoveApprovedChat(chatID)
			cb.Answer(F(cb.ChatID, "approval_toggle_removed"), &telegram.CallbackOptions{})
		} else {
			database.AddApprovedChat(chatID)
			cb.Answer(F(cb.ChatID, "approval_toggle_added"), &telegram.CallbackOptions{})

			cb.Client.SendMessage(chatID, F(chatID, "approval_group_activated"), &telegram.SendOptions{ParseMode: "HTML"})
		}

		markup, text, err := buildGroupsKeyboard(cb.ChatID, page)
		if err != nil {
			return telegram.ErrEndGroup
		}

		cb.Edit(text, &telegram.SendOptions{ParseMode: "HTML", ReplyMarkup: markup})
		return telegram.ErrEndGroup
	}

	if data == "noop" {
		cb.Answer("", &telegram.CallbackOptions{})
		return telegram.ErrEndGroup
	}

	return nil
}
