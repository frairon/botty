package botty

import (
	"fmt"
	"time"
)

type User struct {
	ID   int64
	Name string
}

type StoredSession[T any] struct {
	UserID         int64
	ChatID         int64
	LastAction     time.Time
	SessionContext T
}

type UserManager interface {
	ListUsers() ([]User, error)
	AddUser(userID int64, userName string) error
	UserExists(userID int64) bool
	DeleteUser(int64) error
}

type SessionContextManager[T any] interface {
	CreateSessionContext(userId, chatId int64) T
	StoreSession(session StoredSession[T]) error

	// rename to list sessions
	LoadSessions() ([]StoredSession[T], error)
}

type Config[T any] struct {
	// bot token
	Token string

	SessionContextManager SessionContextManager[T]

	RootState StateFactory[T]

	UserManager UserManager
}

func (c *Config[T]) valid() (*Config[T], error) {
	validatedCfg := &Config[T]{
		Token:                 "",
		SessionContextManager: c.SessionContextManager,
		UserManager:           c.UserManager,
		RootState: func() State[T] {
			return &functionState[T]{
				activate: func(bs Session[T]) {
					bs.SendMessage("hello world")
				},
			}
		},
	}

	if c.SessionContextManager == nil {
		return nil, fmt.Errorf("session context manager must be provided")
	}
	if c.UserManager == nil {
		return nil, fmt.Errorf("user manager must be provided")
	}

	if c.Token != "" {
		validatedCfg.Token = c.Token
	}

	return validatedCfg, nil

}
