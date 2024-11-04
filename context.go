package botty

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type GlobalMessageHandler[T any] func(bs Session[T], message *tgbotapi.Message) bool

type SessionSettings struct {
	PauseAllNotifications bool

	NotifyOnBell             bool
	NotifyOnButtonTempChange bool
	NotifyOnAutoTempChange   bool
	NotifyOnMovement         bool
	NotifyOnEnterLeave       bool
	NotifyOnTempChange       bool

	SingleMovementAlert   bool
	SingleEnterLeaveAlert bool
}

type Session[T any] interface {
	SendMessage(text string, opts ...SendMessageOption) int

	Fail(message string, formatErrorMsg string, args ...interface{})

	SendMessageWithCommands(text string, replyCommands ButtonKeyboard, opts ...SendMessageOption) int

	RootState() State[T]
	PushState(state State[T])
	PopState()
	ReplaceState(state State[T])
	ResetToState(state State[T])
	DropStates(n int)
	SendError(err error)
	UpdateMessageForCallback(queryId string, messageId int, text string, opts ...SendMessageOption)
	RemoveKeyboardForMessage(messageId int)

	AcceptUsers(duration time.Duration)

	BotName() (string, error)
}

type botSession[T any] struct {
	userId int64
	chatId int64

	user *tgbotapi.User
	chat *tgbotapi.Chat

	lastUserAction time.Time

	states []State[T]

	bot *Bot[T]

	// session context by the app
	sessionContext T

	botCtx context.Context

	botApi *tgbotapi.BotAPI

	sessionCommandHandlers map[string]CommandHandler[T]
}

func NewSession[T any](userId int64, chatId int64, sessionContext T, bot *Bot[T], botCtx context.Context, botApi *tgbotapi.BotAPI) *botSession[T] {
	return &botSession[T]{
		userId:                 userId,
		chatId:                 chatId,
		bot:                    bot,
		botCtx:                 botCtx,
		botApi:                 botApi,
		sessionContext:         sessionContext,
		sessionCommandHandlers: make(map[string]CommandHandler[T]),
	}
}

func (bs *botSession[T]) getOrPushCurrentState() State[T] {
	if len(bs.states) == 0 {
		bs.states = []State[T]{bs.bot.rootState()}
	}

	return bs.states[len(bs.states)-1]
}

func (bs *botSession[T]) RootState() State[T] {
	return bs.bot.rootState()
}

func (bs *botSession[T]) AcceptUsers(duration time.Duration) {
	bs.bot.AcceptUsers(duration)
}

func (bs *botSession[T]) Handle(update tgbotapi.Update) bool {
	curState := bs.getOrPushCurrentState()

	bs.lastUserAction = time.Now()

	switch {
	case update.Message != nil:

		// if the message is a command, try to handle that instead.
		// First the current stae, then the context
		if cmd := update.Message.CommandWithAt(); cmd != "" {
			args := strings.Split(update.Message.CommandArguments(), " ")
			if curState.HandleCommand(bs, cmd, args...) {
				return true
			}
			return bs.handleCommand(cmd, args)
		}

		return curState.HandleMessage(bs, update.Message)
	case update.CallbackQuery != nil:

		if curState.HandleCallbackQuery(bs, update.CallbackQuery) {
			return true
		} else {
			return bs.removeExpiredCallback(update.CallbackQuery)
		}

	default:
		log.Printf("unhandled update: %#v", update)
	}
	return false
}

func (bs *botSession[T]) removeExpiredCallback(query *tgbotapi.CallbackQuery) bool {
	alert := tgbotapi.NewCallbackWithAlert(query.InlineMessageID, "message expired, buttons disabled")
	alert.CallbackQueryID = query.ID

	if query.Message != nil {
		bs.RemoveKeyboardForMessage(query.Message.MessageID)
	}
	_, err := bs.botApi.Request(alert)
	if err != nil {
		bs.SendError(err)
	}
	return true
}

func (bs *botSession[T]) RemoveKeyboardForMessage(messageId int) {
	// construct an update reply-markup message manually, because we need to set
	// the ReplyMarkup to nil, which is not supported by the library
	bs.botApi.Request(tgbotapi.EditMessageReplyMarkupConfig{
		BaseEdit: tgbotapi.BaseEdit{
			ChatID:      bs.chatId,
			MessageID:   messageId,
			ReplyMarkup: nil,
		},
	})
}

func (bs *botSession[T]) handleCommand(command string, args []string) bool {
	switch command {
	case CommandCancel.Command:
		bs.PopState()
		return true
	}

	for _, handler := range bs.sessionCommandHandlers {
		if handler.Handle(bs, command, args...) {
			return true
		}
	}

	return false
}

// TODO: can probably be removed
func (bs *botSession[T]) SetCommandHandler(name string, handler CommandHandler[T]) {
	bs.sessionCommandHandlers[name] = handler
}

func (bs *botSession[T]) PushState(state State[T]) {
	if len(bs.states) > 0 {
		bs.CurrentState().BeforeLeave(bs)
	}
	bs.states = append(bs.states, state)
	state.Activate(bs)
}

func (bs *botSession[T]) PopState() {
	if len(bs.states) == 0 {
		return
	}

	bs.CurrentState().BeforeLeave(bs)

	bs.states = bs.states[:len(bs.states)-1]

	curState := bs.getOrPushCurrentState()

	curState.Return(bs)
}

func (bs *botSession[T]) DropStates(n int) {
	if len(bs.states) > n {
		bs.states = bs.states[:len(bs.states)-n]
	} else {
		bs.states = nil
	}
	bs.getOrPushCurrentState().Return(bs)
}

func (bs *botSession[T]) CurrentState() State[T] {
	if len(bs.states) == 0 {
		return nil
	}
	return bs.states[len(bs.states)-1]
}

func (bs *botSession[T]) ReplaceState(state State[T]) {
	if len(bs.states) == 0 {
		return
	}

	bs.states[len(bs.states)-1] = state
	state.Activate(bs)
}

func (bs *botSession[T]) ResetToState(state State[T]) {
	bs.states = nil
	bs.PushState(state)
}

func (bs *botSession[T]) Context() T {
	return bs.sessionContext
}

func (bs *botSession[T]) UserId() int64 {
	return bs.userId
}

func (bs *botSession[T]) ChatId() int64 {
	return bs.chatId
}

func (bs *botSession[T]) SendMessageWithCommands(text string, replyCommands ButtonKeyboard, opts ...SendMessageOption) int {
	msg := tgbotapi.NewMessage(bs.ChatId(), text)
	msg.ParseMode = "html"

	options := &sendMessageOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if replyCommands != nil {
		keyboard := tgbotapi.ReplyKeyboardMarkup{
			ResizeKeyboard: true,
		}
		for _, row := range replyCommands {
			// rows might be nil
			if row == nil {
				continue
			}
			var rowKeys []tgbotapi.KeyboardButton
			for _, cmd := range row {
				rowKeys = append(rowKeys, tgbotapi.NewKeyboardButton(string(cmd)))
			}
			keyboard.Keyboard = append(keyboard.Keyboard, rowKeys)
		}

		msg.ReplyMarkup = keyboard

	} else if len(options.inlineKeyboard) > 0 {

		markup := tgbotapi.NewInlineKeyboardMarkup()
		for _, row := range options.inlineKeyboard {
			keyboardRow := tgbotapi.NewInlineKeyboardRow()
			for _, button := range row {
				keyboardRow = append(keyboardRow, tgbotapi.NewInlineKeyboardButtonData(button.Label, button.Data))
			}
			markup.InlineKeyboard = append(markup.InlineKeyboard, keyboardRow)
		}
		msg.ReplyMarkup = markup
	} else {
		if !options.keepKeyboard {
			msg.ReplyMarkup = tgbotapi.ReplyKeyboardRemove{RemoveKeyboard: true}
		}
	}
	msg.DisableNotification = !options.notification

	sentMsg, err := bs.botApi.Send(msg)
	if err != nil {
		log.Printf("Error sending message %#v: %v", msg, err)
	}
	return sentMsg.MessageID
}

type (
	sendMessageOptions struct {
		keepKeyboard   bool
		inlineKeyboard InlineKeyboard
		notification   bool
	}
	SendMessageOption func(options *sendMessageOptions)
)

func SendMessageKeepKeyboard() SendMessageOption {
	return func(opts *sendMessageOptions) {
		opts.keepKeyboard = true
	}
}

func SendMessageInlineKeyboard(keyboard InlineKeyboard) SendMessageOption {
	return func(opts *sendMessageOptions) {
		opts.inlineKeyboard = keyboard
	}
}

func SendMessageWithNotification() SendMessageOption {
	return func(opts *sendMessageOptions) {
		opts.notification = true
	}
}

func (bs *botSession[T]) SendMessage(text string, opts ...SendMessageOption) int {
	return bs.SendMessageWithCommands(text, nil, opts...)
}

func (bs *botSession[T]) UpdateMessageForCallback(queryId string, messageId int, text string, opts ...SendMessageOption) {
	edit := tgbotapi.EditMessageTextConfig{
		BaseEdit: tgbotapi.BaseEdit{
			ChatID:    bs.chatId,
			MessageID: messageId,
		},
		Text:      text,
		ParseMode: "html",
	}

	options := &sendMessageOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if len(options.inlineKeyboard) > 0 {
		edit.BaseEdit.ReplyMarkup = convertToMarkup(options.inlineKeyboard)
	}

	_, err := bs.botApi.Request(edit)
	if err != nil {
		log.Printf("error updating message: %v", err)
	}
	bs.botApi.Request(tgbotapi.NewCallback(queryId, ""))
}

func (bs *botSession[T]) SendError(err error) {
	_, sendErr := bs.botApi.Send(tgbotapi.NewMessage(bs.ChatId(), fmt.Sprintf("error: %v", err)))
	if sendErr != nil {
		log.Printf("Error sending error: %v", sendErr)
	}
}

func (bs *botSession[T]) Fail(message string, formatErrorMsg string, args ...interface{}) {
	log.Printf(formatErrorMsg, args...)
	bs.SendMessage(message)
	bs.PopState()
}

func (bs *botSession[T]) BotName() (string, error) {
	me, err := bs.botApi.GetMe()
	if err != nil {
		return "", fmt.Errorf("error getting bot identity: %v", err)
	}
	return me.UserName, nil
}

func (bs *botSession[T]) Shutdown() {
	for i := len(bs.states) - 1; i >= 0; i-- {
		bs.states[i].BeforeLeave(bs)
	}
}

func convertToMarkup(keyboard InlineKeyboard) *tgbotapi.InlineKeyboardMarkup {
	markup := tgbotapi.NewInlineKeyboardMarkup()
	for _, row := range keyboard {
		keyboardRow := tgbotapi.NewInlineKeyboardRow()
		for _, button := range row {
			keyboardRow = append(keyboardRow, tgbotapi.NewInlineKeyboardButtonData(button.Label, button.Data))
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, keyboardRow)
	}
	return &markup
}
