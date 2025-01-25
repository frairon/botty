package botty

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

type ChatMessage interface {
	Text() string
}

type tgMessage struct {
	m *tgbotapi.Message
}

func (m *tgMessage) Text() string {
	return m.m.Text
}

type CallbackQuery interface {
	Data() string
	ID() string
	MessageID() MessageId
}

type tgCbQuery struct {
	m *tgbotapi.CallbackQuery
}

func (m *tgCbQuery) Data() string {
	return m.m.Data
}
func (m *tgCbQuery) ID() string {
	return m.m.ID
}
func (m *tgCbQuery) MessageID() MessageId {
	if m.m.Message != nil {
		return MessageId(m.m.Message.MessageID)
	}
	return 0

}
