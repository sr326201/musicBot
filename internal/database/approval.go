package database

func ApprovedChats() ([]int64, error) {
	state, err := getBotState()
	if err != nil {
		return nil, err
	}
	return append([]int64(nil), state.Approved.Chats...), nil
}

func IsApprovedChat(chatID int64) (bool, error) {
	state, err := getBotState()
	if err != nil {
		return false, err
	}
	return contains(state.Approved.Chats, chatID), nil
}

func AddApprovedChat(chatID int64) error {
	return modifyBotState(func(s *BotState) bool {
		var added bool
		s.Approved.Chats, added = addUnique(s.Approved.Chats, chatID)
		return added
	})
}

func RemoveApprovedChat(chatID int64) error {
	return modifyBotState(func(s *BotState) bool {
		var removed bool
		s.Approved.Chats, removed = removeElement(s.Approved.Chats, chatID)
		return removed
	})
}
