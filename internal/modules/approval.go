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
	groups, totalPages, page, err := getManagedGroups(page)
	if err != nil {
		return nil, "", err
	}

	totalGroups := len(groups)
	if totalGroups == 0 {
		return nil, F(chatID, "allgroups_empty"), nil
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

	kb.AddRow(tg.Button.Data(F(chatID, "owner_panel_back"), "owner:main"))

	approvedCount, pendingCount := countGroupApprovalStates(groups)
	text := F(chatID, "allgroups_title", locales.Arg{
		"total":    totalGroups,
		"approved": approvedCount,
		"pending":  pendingCount,
	})
	return kb.Build(), text, nil
}

func getManagedGroups(page int) ([]int64, int, int, error) {
	chats, err := database.ServedChats()
	if err != nil {
		return nil, 0, 0, err
	}

	var groups []int64
	for _, id := range chats {
		if id < 0 || id >= 4000000000 {
			groups = append(groups, id)
		}
	}

	totalPages := 1
	if len(groups) > 0 {
		totalPages = (len(groups) + pageSize - 1) / pageSize
	}
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	return groups, totalPages, page, nil
}

func countGroupApprovalStates(groups []int64) (approvedCount, pendingCount int) {
	for _, groupID := range groups {
		approved, _ := database.IsApprovedChat(groupID)
		if approved {
			approvedCount++
		} else {
			pendingCount++
		}
	}
	return approvedCount, pendingCount
}

func handleAllGroups(m *telegram.NewMessage) error {
	if !isOwnerOrSudo(m.SenderID()) {
		return telegram.ErrEndGroup
	}

	markup, text, err := buildGroupsKeyboard(m.ChatID(), 0)
	if err != nil {
		m.Reply(F(m.ChatID(), "approval_error_loading_detail", locales.Arg{"error": err.Error()}))
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

		if err := toggleGroupApproval(cb, chatID); err != nil {
			cb.Answer(F(cb.ChatID, "approval_error_loading"), &telegram.CallbackOptions{Alert: true})
			return telegram.ErrEndGroup
		}

		markup, text, err := buildGroupsKeyboard(cb.ChatID, page)
		if err != nil {
			return telegram.ErrEndGroup
		}

		cb.Edit(text, &telegram.SendOptions{ParseMode: "HTML", ReplyMarkup: markup})
		return telegram.ErrEndGroup
	}

	if strings.HasPrefix(data, "approve_") {
		chatID, err := strconv.ParseInt(strings.TrimPrefix(data, "approve_"), 10, 64)
		if err != nil {
			return telegram.ErrEndGroup
		}
		if err := approveGroup(cb, chatID); err != nil {
			cb.Answer(F(cb.ChatID, "approval_error_loading"), &telegram.CallbackOptions{Alert: true})
			return telegram.ErrEndGroup
		}
		cb.Edit(F(cb.ChatID, "approval_owner_request_resolved", locales.Arg{"status": F(cb.ChatID, "approval_owner_status_approved")}), &telegram.SendOptions{ParseMode: "HTML"})
		return telegram.ErrEndGroup
	}

	if strings.HasPrefix(data, "deny_") {
		chatID, err := strconv.ParseInt(strings.TrimPrefix(data, "deny_"), 10, 64)
		if err != nil {
			return telegram.ErrEndGroup
		}
		if err := denyGroup(cb, chatID); err != nil {
			cb.Answer(F(cb.ChatID, "approval_error_loading"), &telegram.CallbackOptions{Alert: true})
			return telegram.ErrEndGroup
		}
		cb.Edit(F(cb.ChatID, "approval_owner_request_resolved", locales.Arg{"status": F(cb.ChatID, "approval_owner_status_denied")}), &telegram.SendOptions{ParseMode: "HTML"})
		return telegram.ErrEndGroup
	}

	if data == "noop" {
		cb.Answer("", &telegram.CallbackOptions{})
		return telegram.ErrEndGroup
	}

	return nil
}

func toggleGroupApproval(cb *telegram.CallbackQuery, chatID int64) error {
	approved, _ := database.IsApprovedChat(chatID)
	if approved {
		if err := database.RemoveApprovedChat(chatID); err != nil {
			return err
		}
		clearApprovalWarning(chatID)
		cb.Answer(F(cb.ChatID, "approval_toggle_removed"), &telegram.CallbackOptions{})
		return nil
	}
	return approveGroup(cb, chatID)
}

func approveGroup(cb *telegram.CallbackQuery, chatID int64) error {
	if err := database.AddApprovedChat(chatID); err != nil {
		return err
	}
	clearApprovalWarning(chatID)
	cb.Answer(F(cb.ChatID, "approval_toggle_added"), &telegram.CallbackOptions{})
	_, _ = cb.Client.SendMessage(chatID, F(chatID, "approval_group_activated"), &telegram.SendOptions{ParseMode: "HTML"})
	return nil
}

func denyGroup(cb *telegram.CallbackQuery, chatID int64) error {
	_ = database.RemoveApprovedChat(chatID)
	clearApprovalWarning(chatID)
	_, _ = cb.Client.SendMessage(chatID, F(chatID, "approval_group_denied"), &telegram.SendOptions{ParseMode: "HTML"})
	leaveChat(cb.Client, chatID)
	cb.Answer(F(cb.ChatID, "approval_toggle_denied"), &telegram.CallbackOptions{})
	return nil
}

func clearApprovalWarning(chatID int64) {
	warnedChatsMu.Lock()
	delete(warnedChats, chatID)
	warnedChatsMu.Unlock()
}
