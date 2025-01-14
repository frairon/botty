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
