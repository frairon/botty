package botty

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type (
	UserId    int64
	ChatId    int64
	MessageId int64
)

type TGApi interface {
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	GetMe() (tgbotapi.User, error)
	GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel
	StopReceivingUpdates()
}

type Bot[T any] struct {
	botApi TGApi

	config *Config[T]

	acceptNewUser bool

	mSessions sync.Mutex
	sessions  map[ChatId]*session[T]

	startTime time.Time

	// will be closed when bot is shutting down
	shutdown chan struct{}
}

func New[T any](config *Config[T]) (*Bot[T], error) {

	if err := config.validate(); err != nil {
		return nil, err
	}

	botApi, err := config.Connect(config.Token)
	if err != nil {
		return nil, fmt.Errorf("error connecting to bot api: %w", err)
	}

	return &Bot[T]{
		config:   config,
		botApi:   botApi,
		sessions: make(map[ChatId]*session[T]),
		shutdown: make(chan struct{}),
	}, nil
}

func (b *Bot[T]) getOrCreateSession(ctx context.Context, userId UserId, chatId ChatId) (*session[T], error) {
	b.mSessions.Lock()
	defer b.mSessions.Unlock()

	session := b.sessions[chatId]
	if session == nil {
		session = NewSession(userId, chatId, b.config.AppStateManager.CreateAppState(userId, chatId), b, ctx, b.botApi)
		b.sessions[chatId] = session

		// create an initial state and activate
		session.getOrPushCurrentState()
		session.CurrentState().Activate(session)

	}

	return session, nil
}

var (
	CommandReload = tgbotapi.BotCommand{
		Command:     "reload",
		Description: "Reloads the current state",
	}
	CommandCancel = tgbotapi.BotCommand{
		Command:     "back",
		Description: "stop the current operation, go to the previous state",
	}
	CommandHelp = tgbotapi.BotCommand{
		Command:     "help",
		Description: "Show general help",
	}
	CommandMain = tgbotapi.BotCommand{
		Command:     "home",
		Description: "Go back to root state",
	}
	CommandUsers = tgbotapi.BotCommand{
		Command:     "users",
		Description: "Goes to the user management",
	}
)

func (b *Bot[T]) Run(ctx context.Context) error {
	b.startTime = time.Now()
	b.shutdown = make(chan struct{})

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.botApi.GetUpdatesChan(u)

	// stop the updates
	defer b.botApi.StopReceivingUpdates()

	_, err := b.botApi.Request(tgbotapi.NewSetMyCommands(
		CommandMain,
		CommandUsers,
		CommandCancel,
		// CommandHelp,
		CommandReload))
	if err != nil {
		log.Printf("error setting my commands")
	}

	b.loadSessions(ctx)

	// broadcast shutdown message and store everything
	defer func() {
		for _, session := range b.sessions {
			session.Shutdown()
		}
		b.ForeachSessionAsync(func(session Session[T]) {
			if session.LastUserAction().IsZero() {
				return
			}
			session.SendMessage("Bot is restarting for maintenance. See you in a few minutes. 🧘")
		})
		b.storeSessions(ctx)
	}()

	sessionStoreTicker := time.NewTicker(60 * time.Second)
	defer sessionStoreTicker.Stop()

	for {
		select {
		case upd, ok := <-updates:
			if !ok {
				return nil
			}

			// an update-ID < 0 cannot happen, but it's used by the mock to achieve
			// synchronous behavior. We will drop it here.
			if upd.UpdateID < 0 {
				continue
			}

			user := upd.SentFrom()
			if user == nil {
				log.Printf("no sending user - dropping update: %v", upd)
				continue
			}
			if !b.config.UserManager.UserExists(UserId(user.ID)) {
				if !b.acceptNewUser {
					log.Printf("user not allowed: %v", user.ID)
					continue
				}

				name := findNameForUser(user)
				log.Printf("Adding new user with %d (%s)", user.ID, name)
				if err := b.config.UserManager.AddUser(UserId(user.ID), name); err != nil {
					log.Printf("Error adding user: %#v: %v", user, err)
					continue
				}
			}

			session, err := b.getOrCreateSession(ctx, UserId(user.ID), ChatId(upd.FromChat().ID))
			if err != nil {
				log.Printf("error handling update %#v: %v", upd, err)
				continue
			}

			if !session.Handle(upd) {
				if upd.Message != nil && upd.Message.Command() != "" {
					command := upd.Message.Command()
					switch command {
					case CommandCancel.Command:
						session.PopState()
					case CommandReload.Command:
						session.ReplaceState(session.CurrentState())
					case CommandHelp.Command:
						session.SendMessage("Help message how to use the bot. TODO.")
					case CommandMain.Command:
						session.ResetToState(b.rootState())
					case CommandUsers.Command:
						session.ResetToState(UsersList[T](b.config.UserManager))
					default:
						log.Printf("unhandled command: %s", command)
					}
				} else {
					log.Printf("unhandled update: %#v", upd)
				}
			}
		case <-ctx.Done():
			return nil
		case <-b.shutdown:
			log.Printf("bot shutdown initiated")
			return nil
		case <-sessionStoreTicker.C:
			b.storeSessions(ctx)
		}
	}
}

func (b *Bot[T]) rootState() State[T] {
	return b.config.RootState()
}

func (b *Bot[T]) ForeachSessionAsync(do func(session Session[T])) {
	for _, session := range b.sessions {
		session := session
		go func() {
			do(session)
		}()
	}
}

func (b *Bot[T]) shutdownBot() {
	close(b.shutdown)
}

func (b *Bot[T]) AcceptUsers(dur time.Duration) {
	b.acceptNewUser = true
	go func() {
		select {
		case <-time.After(dur):
			b.acceptNewUser = false
		case <-b.shutdown:
		}
	}()
}

func (b *Bot[T]) storeSessions(ctx context.Context) {
	b.mSessions.Lock()
	defer b.mSessions.Unlock()
	for _, session := range b.sessions {
		err := b.config.AppStateManager.StoreSessionState(StoredSessionState[T]{
			UserID:     UserId(session.userId),
			ChatID:     ChatId(session.chatId),
			LastAction: time.Now(),
			State:      session.appState,
		})
		if err != nil {
			log.Printf("error storing session for user %d: %v", session.userId, err)
		}
	}
}

func (b *Bot[T]) loadSessions(ctx context.Context) error {
	b.mSessions.Lock()
	defer b.mSessions.Unlock()

	sessions, err := b.config.AppStateManager.LoadSessionStates()
	if err != nil {
		return fmt.Errorf("error loading sessions: %v", err)
	}

	for _, session := range sessions {

		if session.ChatID == 0 || session.UserID == 0 {
			log.Printf("ignoring invalid session: %#v", session)
			continue
		}

		bs := NewSession(UserId(session.UserID), ChatId(session.ChatID), session.State, b, ctx, b.botApi)
		b.sessions[session.ChatID] = bs

		// if the user was active in the last 30 days, we'll tell them that the bot is back by activating the current state
		if !session.LastAction.IsZero() && time.Since(session.LastAction) < time.Hour*24*30 {
			bs.getOrPushCurrentState().Activate(bs)
		} else {
			// initialize to root state
			// TODO: this needs to be some kind of 'init' function instead
			bs.getOrPushCurrentState()
		}

	}

	return nil
}
