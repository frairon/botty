package botty

import (
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type User struct {
	ID   UserId
	Name string
}

type StoredSessionState[T any] struct {
	UserID     UserId
	ChatID     ChatId
	LastAction time.Time
	State      T
}

type UserManager interface {
	ListUsers() ([]User, error)
	AddUser(userID UserId, userName string) error
	UserExists(userID UserId) bool
	DeleteUser(userID UserId) error
}

type AppStateManager[T any] interface {
	CreateAppState(userId UserId, chatId ChatId) T
	StoreSessionState(state StoredSessionState[T]) error

	// rename to list sessions
	LoadSessionStates() ([]StoredSessionState[T], error)
}

type Config[T any] struct {
	// bot token
	Token string

	AppStateManager AppStateManager[T]

	RootState StateFactory[T]

	UserManager UserManager

	Connect func(token string) (TGApi, error)
}

func NewConfig[T any](token string, appStateManager AppStateManager[T], userManager UserManager, rootState StateFactory[T]) *Config[T] {

	return &Config[T]{
		Token:           token,
		AppStateManager: appStateManager,
		UserManager:     userManager,
		RootState:       rootState,
		Connect: func(token string) (TGApi, error) {
			api, err := tgbotapi.NewBotAPI(token)
			if err != nil {
				return nil, fmt.Errorf("error connecting to bot api: %w", err)
			}
			return api, err
		},
	}
}

func (c *Config[T]) validate() error {

	if c.AppStateManager == nil {
		return fmt.Errorf("session context manager must be provided")
	}
	if c.UserManager == nil {
		return fmt.Errorf("user manager must be provided")
	}

	return nil
}
