package botty

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot[T any] struct {
	botApi *tgbotapi.BotAPI

	config *Config[T]

	acceptNewUser bool

	mSessions             sync.Mutex
	sessions              map[int64]*botSession[T]
	sessionContextFactory func(userId, chatId int64) T

	startTime time.Time

	// will be closed when bot is shutting down
	shutdown chan struct{}

	rootState StateFactory[T]
}

func New[T any](config *Config[T]) (*Bot[T], error) {

	botApi, err := tgbotapi.NewBotAPI(config.Token)
	if err != nil {
		return nil, fmt.Errorf("error connecting to bot api: %w", err)
	}

	return &Bot[T]{
		config:   config,
		botApi:   botApi,
		sessions: make(map[int64]*botSession[T]),
		shutdown: make(chan struct{}),
	}, nil
}

func (b *Bot[T]) getOrCreateSession(ctx context.Context, user *tgbotapi.User, chat *tgbotapi.Chat) (*botSession[T], error) {
	if chat == nil {
		return nil, fmt.Errorf("chat is nil, cannot create session")
	}
	if user == nil {
		return nil, fmt.Errorf("user is nil, cannot create session")
	}
	b.mSessions.Lock()
	defer b.mSessions.Unlock()

	session := b.sessions[chat.ID]
	if session == nil {
		session = NewSession(user.ID, chat.ID, b.sessionContextFactory(user.ID, chat.ID), b, ctx, b.botApi)
		b.sessions[chat.ID] = session

		// create an initial state and activate
		session.getOrPushCurrentState()
		session.CurrentState().Activate(session)

	}

	if session.chat == nil {
		session.chat = chat
	}

	if session.user == nil {
		session.user = user
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
		b.broadcastToActive("Bot is restarting for maintenance. See you in a few minutes. 🧘")
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

			user := upd.SentFrom()
			if user == nil {
				log.Printf("no sending user - dropping update: %v", upd)
				continue
			}

			if !b.config.UserManager.UserExists(user.ID) {
				if !b.acceptNewUser {
					log.Printf("user not allowed: %v", user.ID)
					continue
				}

				name := findNameForUser(user)
				log.Printf("Adding new user with %d (%s)", user.ID, name)
				if err := b.config.UserManager.AddUser(user.ID, name); err != nil {
					log.Printf("Error adding user: %#v: %v", user, err)
					continue
				}
			}

			session, err := b.getOrCreateSession(ctx, user, upd.FromChat())
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

func (b *Bot[T]) foreachSessionAsync(do func(session Session[T])) {
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
		err := b.config.SessionContextManager.StoreSession(StoredSession[T]{
			UserID:         session.userId,
			ChatID:         session.chatId,
			LastAction:     time.Now(),
			SessionContext: session.sessionContext,
		})
		if err != nil {
			log.Printf("error storing session for user %d: %v", session.userId, err)
		}
	}
}

func (b *Bot[T]) loadSessions(ctx context.Context) error {
	b.mSessions.Lock()
	defer b.mSessions.Unlock()

	sessions, err := b.config.SessionContextManager.LoadSessions()
	if err != nil {
		return fmt.Errorf("error loading sessions: %v", err)
	}

	for _, session := range sessions {

		if session.ChatID == 0 || session.UserID == 0 {
			log.Printf("ignoring invalid session: %#v", session)
			continue
		}

		bs := NewSession(session.UserID, session.ChatID, session.SessionContext, b, ctx, b.botApi)
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

func (b *Bot[T]) broadcastToActive(message string) {
	b.mSessions.Lock()
	defer b.mSessions.Unlock()

	for _, session := range b.sessions {
		if session.lastUserAction.IsZero() {
			continue
		}
		session.SendMessage(message, SendMessageKeepKeyboard())
	}
}

type BroadcastOptions[T any] struct {
	message  string
	newState StateFactory[T]
}
type BroadcastOption[T any] func(opts *BroadcastOptions[T])

func BroadcastNewState[T any](stateBuilder StateFactory[T]) BroadcastOption[T] {
	return func(opts *BroadcastOptions[T]) {
		opts.newState = stateBuilder
	}
}

func BroadcastMessage[T any](message string) BroadcastOption[T] {
	return func(opts *BroadcastOptions[T]) {
		opts.message = message
	}
}

func BroadcastMessagef[T any](format string, args ...interface{}) BroadcastOption[T] {
	return BroadcastMessage[T](fmt.Sprintf(format, args...))
}

func (b *Bot[T]) broadcast(opts ...BroadcastOption[T]) {
	b.mSessions.Lock()
	defer b.mSessions.Unlock()

	for _, session := range b.sessions {
		var options BroadcastOptions[T]

		for _, opt := range opts {
			opt(&options)
		}

		session := session
		go func() {
			if options.message != "" {
				session.SendMessage(options.message, SendMessageKeepKeyboard())
			}
			if options.newState != nil {
				session.ResetToState(options.newState())
			}
		}()

	}
}
