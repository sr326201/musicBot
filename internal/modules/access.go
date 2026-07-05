package modules

import (
	"strings"

	tg "github.com/amarnathcjd/gogram/telegram"

	"main/internal/config"
	"main/internal/database"
)

func isOwner(userID int64) bool {
	return config.OwnerID != 0 && userID == config.OwnerID
}

func isSudo(userID int64) bool {
	ok, err := database.IsSudo(userID)
	return err == nil && ok
}

func isOwnerOrSudo(userID int64) bool {
	return isOwner(userID) || isSudo(userID)
}

func canBypassMaintenance(userID int64) bool {
	isMaint, _ := database.IsMaintenanceEnabled()
	if !isMaint {
		return true
	}
	return isOwnerOrSudo(userID)
}

func shouldReplyPrivilegeError(m *tg.NewMessage) bool {
	return m.IsPrivate() || strings.HasSuffix(m.GetCommand(), m.Client.Me().Username)
}

func requireOwnerMessage(m *tg.NewMessage) bool {
	if isOwner(m.SenderID()) {
		return true
	}
	if shouldReplyPrivilegeError(m) {
		// m.Reply(F(m.ChannelID(), "only_owner"))
	}
	return false
}

func requireSudoMessage(m *tg.NewMessage) bool {
	if isOwnerOrSudo(m.SenderID()) {
		return true
	}
	if shouldReplyPrivilegeError(m) {
		m.Reply(F(m.ChannelID(), "only_sudo"))
	}
	return false
}

func requireOwnerCallback(cb *tg.CallbackQuery) bool {
	if isOwner(cb.SenderID) {
		return true
	}
	// cb.Answer(F(cb.ChatID, "only_owner"), &tg.CallbackOptions{Alert: true})
	return false
}
