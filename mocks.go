package botty

import (
	"context"
	"log"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type MockBot[T any] struct {
	bot *Bot[T]

	done   chan struct{}
	cancel context.CancelFunc

	api *mockApi[T]

	LastMessage tgbotapi.MessageConfig
	NumMsgSent  int

	err struct {
		sync.Mutex
		err error
	}
}

func NewMockBot[T any](cfg *Config[T]) (*MockBot[T], error) {

	ctx, cancel := context.WithCancel(context.Background())
	mockBot := &MockBot[T]{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	mockBot.api = &mockApi[T]{
		botId:   123,
		updates: make(chan tgbotapi.Update),
		mock:    mockBot,
	}

	cfg.Connect = func(token string) (TGApi, error) {
		return mockBot.api, nil
	}
	var err error
	mockBot.bot, err = New(cfg)

	if err != nil {
		return nil, err
	}

	go func() {
		mockBot.err.Lock()
		defer mockBot.err.Unlock()
		defer close(mockBot.done)
		mockBot.err.err = mockBot.bot.Run(ctx)
	}()
	return mockBot, nil
}

func (mb *MockBot[T]) Err() error {
	mb.err.Lock()
	defer mb.err.Unlock()
	return mb.err.err
}

type mockApi[T any] struct {
	botId UserId

	mock *MockBot[T]

	updates chan tgbotapi.Update
}

func (mb *MockBot[T]) Stop() {
	mb.cancel()
	<-mb.done
}

func (mb *MockBot[T]) CreateSession(userId UserId) (Session[T], error) {
	chatId := ChatId(userId)
	var err error
	mb.bot.sessions[chatId], err = mb.bot.getOrCreateSession(context.Background(), userId, chatId)
	return mb.bot.sessions[chatId], err
}

func (mb *MockBot[T]) LastMessageText() string {
	return mb.LastMessage.Text
}

func (mb *MockBot[T]) LastMessageButtons() []string {
	keyboard, ok := mb.LastMessage.ReplyMarkup.(tgbotapi.ReplyKeyboardMarkup)
	if !ok {
		return nil
	}
	var buttons []string
	for _, row := range keyboard.Keyboard {
		for _, button := range row {
			buttons = append(buttons, button.Text)
		}
	}
	return buttons
}

func (mb *MockBot[T]) Send(userId UserId, text string) {
	mb.api.updates <- tgbotapi.Update{
		Message: &tgbotapi.Message{
			From: &tgbotapi.User{ID: int64(userId)},
			Chat: &tgbotapi.Chat{ID: int64(userId)},
			Text: text,
		},
	}
	// send noop update to synchronize the caller
	mb.api.updates <- tgbotapi.Update{
		UpdateID: -1,
	}
}

func (m *mockApi[T]) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	switch value := c.(type) {

	// ignored
	case tgbotapi.SetMyCommandsConfig:
	default:
		_ = value

		log.Printf("Trying to request something unknown: %T (%v)", c, c)
	}
	return nil, nil
}
func (m *mockApi[T]) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	// log.Printf("Send: %#v", c)
	switch value := c.(type) {
	case (tgbotapi.MessageConfig):
		m.mock.LastMessage = value

	default:
		log.Printf("Trying to send something unknown: %T", c)
	}
	m.mock.NumMsgSent++
	return tgbotapi.Message{}, nil
}
func (m *mockApi[T]) GetMe() (tgbotapi.User, error) {
	return tgbotapi.User{
		ID:    int64(m.botId),
		IsBot: true,
	}, nil
}

func (m *mockApi[T]) GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	return m.updates
}
func (m *mockApi[T]) StopReceivingUpdates() {
	close(m.updates)
}
