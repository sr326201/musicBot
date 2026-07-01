package modules

import (
	"strings"

	tg "github.com/amarnathcjd/gogram/telegram"

	"main/internal/database"
	"main/internal/locales"
)

func handleOwnerPanel(m *tg.NewMessage) error {
	if !requireOwnerMessage(m) {
		return tg.ErrEndGroup
	}

	text, markup, err := renderOwnerPanel(m.ChannelID())
	if err != nil {
		m.Reply(F(m.ChannelID(), "owner_panel_load_fail", locales.Arg{"error": err.Error()}))
		return tg.ErrEndGroup
	}

	_, _ = m.Reply(text, &tg.SendOptions{ParseMode: "HTML", ReplyMarkup: markup})
	return tg.ErrEndGroup
}

func ownerPanelCallbackHandler(cb *tg.CallbackQuery) error {
	if !requireOwnerCallback(cb) {
		return tg.ErrEndGroup
	}

	chatID := cb.ChatID
	switch cb.DataString() {
	case "owner:main", "owner:refresh":
		text, markup, err := renderOwnerPanel(chatID)
		if err != nil {
			cb.Answer(F(chatID, "owner_panel_load_fail", locales.Arg{"error": err.Error()}), &tg.CallbackOptions{Alert: true})
			return tg.ErrEndGroup
		}
		cb.Edit(text, &tg.SendOptions{ParseMode: "HTML", ReplyMarkup: markup})
		cb.Answer(F(chatID, "owner_panel_refreshed"), &tg.CallbackOptions{})
		return tg.ErrEndGroup
	case "owner:groups":
		markup, text, err := buildGroupsKeyboard(chatID, 0)
		if err != nil {
			cb.Answer(F(chatID, "approval_error_loading_detail", locales.Arg{"error": err.Error()}), &tg.CallbackOptions{Alert: true})
			return tg.ErrEndGroup
		}
		cb.Edit(text, &tg.SendOptions{ParseMode: "HTML", ReplyMarkup: markup})
		cb.Answer("", &tg.CallbackOptions{})
		return tg.ErrEndGroup
	case "owner:maintenance":
		text, markup, err := buildMaintenancePanel(chatID)
		if err != nil {
			cb.Answer(F(chatID, "owner_panel_load_fail", locales.Arg{"error": err.Error()}), &tg.CallbackOptions{Alert: true})
			return tg.ErrEndGroup
		}
		cb.Edit(text, &tg.SendOptions{ParseMode: "HTML", ReplyMarkup: markup})
		cb.Answer("", &tg.CallbackOptions{})
		return tg.ErrEndGroup
	case "owner:users", "owner:sudo", "owner:stats", "owner:system":
		cb.Answer(ownerPanelSectionText(chatID, cb.DataString()), &tg.CallbackOptions{Alert: true})
		return tg.ErrEndGroup
	case "owner:maint_enable":
		err := applyMaintenancePanelToggle(true, "")
		if err != nil {
			cb.Answer(F(chatID, "owner_panel_load_fail", locales.Arg{"error": err.Error()}), &tg.CallbackOptions{Alert: true})
			return tg.ErrEndGroup
		}
		go notifyMaintenanceStart(cb.Client, "")
		cb.Answer(F(chatID, "maint_enabled"), &tg.CallbackOptions{Alert: false})
		text, markup, _ := buildMaintenancePanel(chatID)
		cb.Edit(text, &tg.SendOptions{ParseMode: "HTML", ReplyMarkup: markup})
		return tg.ErrEndGroup
	case "owner:maint_disable":
		err := applyMaintenancePanelToggle(false, "")
		if err != nil {
			cb.Answer(F(chatID, "owner_panel_load_fail", locales.Arg{"error": err.Error()}), &tg.CallbackOptions{Alert: true})
			return tg.ErrEndGroup
		}
		maintCancel.Lock()
		maintCancel.cancel = true
		maintCancel.Unlock()
		cb.Answer(F(chatID, "maint_disabled"), &tg.CallbackOptions{Alert: false})
		text, markup, _ := buildMaintenancePanel(chatID)
		cb.Edit(text, &tg.SendOptions{ParseMode: "HTML", ReplyMarkup: markup})
		return tg.ErrEndGroup
	case "owner:close":
		cb.Edit(F(chatID, "owner_panel_closed"), &tg.SendOptions{ParseMode: "HTML"})
		cb.Answer("", &tg.CallbackOptions{})
		return tg.ErrEndGroup
	}

	return nil
}

func renderOwnerPanel(chatID int64) (string, *tg.ReplyInlineMarkup, error) {
	groups, _, _, err := getManagedGroups(0)
	if err != nil {
		return "", nil, err
	}
	approvedCount, pendingCount := countGroupApprovalStates(groups)
	sudoers, err := database.Sudoers()
	if err != nil {
		return "", nil, err
	}
	blockedUsers, err := database.BlacklistedUsers()
	if err != nil {
		return "", nil, err
	}
	blockedChats, err := database.BlacklistedChats()
	if err != nil {
		return "", nil, err
	}
	servedUsers, err := database.ServedUsers()
	if err != nil {
		return "", nil, err
	}
	maintEnabled, _ := database.IsMaintenanceEnabled()
	maintReason, _ := database.MaintenanceReason()
	maintStatus := F(chatID, "disabled")
	if maintEnabled {
		maintStatus = F(chatID, "enabled")
	}

	text := F(chatID, "owner_panel_main", locales.Arg{
		"groups_total":    len(groups),
		"groups_approved": approvedCount,
		"groups_pending":  pendingCount,
		"users_total":     len(servedUsers),
		"sudo_total":      len(sudoers) + 1,
		"blocked_users":   len(blockedUsers),
		"blocked_chats":   len(blockedChats),
		"maint_status":    maintStatus,
		"maint_reason":    ownerPanelReasonLine(chatID, maintReason),
	})

	markup := tg.NewKeyboard().
		AddRow(
			tg.Button.Data(F(chatID, "owner_panel_btn_groups"), "owner:groups"),
			tg.Button.Data(F(chatID, "owner_panel_btn_users"), "owner:users"),
		).
		AddRow(
			tg.Button.Data(F(chatID, "owner_panel_btn_sudo"), "owner:sudo"),
			tg.Button.Data(F(chatID, "owner_panel_btn_stats"), "owner:stats"),
		).
		AddRow(
			tg.Button.Data(F(chatID, "owner_panel_btn_maintenance"), "owner:maintenance"),
			tg.Button.Data(F(chatID, "owner_panel_btn_system"), "owner:system"),
		).
		AddRow(
			tg.Button.Data(F(chatID, "owner_panel_btn_refresh"), "owner:refresh"),
			tg.Button.Data(F(chatID, "CLOSE_BTN"), "owner:close"),
		).
		Build()

	return text, markup, nil
}

func ownerPanelReasonLine(chatID int64, reason string) string {
	if strings.TrimSpace(reason) == "" {
		return F(chatID, "owner_panel_no_reason")
	}
	return formatMaintenanceReason(chatID, reason)
}

func ownerPanelSectionText(chatID int64, data string) string {
	key := strings.ReplaceAll(data, "owner:", "owner_panel_section_")
	return F(chatID, key, locales.Arg{
		"groups_cmd":    "/allgroups",
		"sudo_list_cmd": "/sudoers",
		"sudo_add_cmd":  "/addsudo",
		"sudo_del_cmd":  "/delsudo",
		"blocked_cmd":   "/blacklisted",
		"stats_cmd":     "/stats",
		"active_cmd":    "/active",
		"maint_cmd":     "/maintenance",
		"restart_cmd":   "/restart",
		"broadcast_cmd": "/broadcast",
		"logs_cmd":      "/logs",
		"logger_cmd":    "/logger",
		"shell_cmd":     "/sh",
		"eval_cmd":      "/eval",
	})
}

func buildMaintenancePanel(chatID int64) (string, *tg.ReplyInlineMarkup, error) {
	enabled, _ := database.IsMaintenanceEnabled()
	reason, _ := database.MaintenanceReason()

	status := F(chatID, "disabled")
	if enabled {
		if reason != "" {
			status = F(chatID, "enabled_with_reason", locales.Arg{"reason": reason})
		} else {
			status = F(chatID, "enabled")
		}
	}

	text := F(chatID, "owner_panel_maintenance", locales.Arg{
		"status": status,
		"cmd":    "/maintenance",
	})

	kb := tg.NewKeyboard()
	if enabled {
		kb.AddRow(tg.Button.Data(F(chatID, "owner_panel_maint_btn_disable"), "owner:maint_disable"))
	} else {
		kb.AddRow(tg.Button.Data(F(chatID, "owner_panel_maint_btn_enable"), "owner:maint_enable"))
	}
	kb.AddRow(tg.Button.Data(F(chatID, "owner_panel_back"), "owner:main"))

	return text, kb.Build(), nil
}

func applyMaintenancePanelToggle(enable bool, reason string) error {
	return database.SetMaintenance(enable, reason)
}
