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

// remove that type. It indicates we could use it outside of a session but we shouldn't. Instead
// the context should have an updater-interface that modifies a messsage and the message-id becomes its own (int)-type
type Message interface {
	UpdateMessage(queryId string, text string, opts ...SendMessageOption)
	RemoveKeyboardForMessage()
}

type message struct {
	messageId int // use this in the state
	// if we add a bot-session, do not marshal that to state but inject when unmarshalling
}

func (m *message) UpdateMessage(queryId string, text string, opts ...SendMessageOption) {
}
func (m *message) RemoveKeyboardForMessage() {
}

type Session[T any] interface {
	SendMessage(text string, opts ...SendMessageOption) Message

	Fail(message string, formatErrorMsg string, args ...interface{})

	SendTemplateMessage(template string, values KeyValues, opts ...SendMessageOption) Message

	RootState() State[T]
	PushState(state State[T])
	PopState()
	ReplaceState(state State[T])
	ResetToState(state State[T])
	DropStates(n int)
	SendError(err error)

	RemoveKeyboardForMessage(messageId MessageId)

	// returns the current user ID
	UserId() UserId

	AcceptUsers(duration time.Duration)

	BotName() (string, error)

	State() T
}

type session[T any] struct {
	botApi TGApi

	userId UserId
	chatId ChatId

	// session state the app
	appState T

	bot *Bot[T]

	lastUserAction time.Time

	stateStack []State[T]

	botCtx context.Context

	sessionCommandHandlers map[string]CommandHandler[T]
}

func NewSession[T any](userId UserId, chatId ChatId, appState T, bot *Bot[T], botCtx context.Context, botApi TGApi) *session[T] {
	return &session[T]{
		userId:                 userId,
		chatId:                 chatId,
		botCtx:                 botCtx,
		botApi:                 botApi,
		bot:                    bot,
		sessionCommandHandlers: make(map[string]CommandHandler[T]),
		appState:               appState,
	}

}

func (bs *session[T]) State() T {
	return bs.appState
}

func (bs *session[T]) getOrPushCurrentState() State[T] {
	if len(bs.stateStack) == 0 {
		bs.stateStack = []State[T]{bs.bot.rootState()}
	}

	return bs.stateStack[len(bs.stateStack)-1]
}

func (bs *session[T]) RootState() State[T] {
	return bs.bot.rootState()
}

func (bs *session[T]) AcceptUsers(duration time.Duration) {
	bs.bot.AcceptUsers(duration)
}

func (bs *session[T]) Handle(update tgbotapi.Update) bool {
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

func (bs *session[T]) removeExpiredCallback(query *tgbotapi.CallbackQuery) bool {
	alert := tgbotapi.NewCallbackWithAlert(query.InlineMessageID, "message expired, buttons disabled")
	alert.CallbackQueryID = query.ID

	if query.Message != nil {
		bs.RemoveKeyboardForMessage(MessageId(query.Message.MessageID))
	}
	_, err := bs.bot.botApi.Request(alert)
	if err != nil {
		bs.SendError(err)
	}
	return true
}

func (bs *session[T]) RemoveKeyboardForMessage(messageId MessageId) {
	// construct an update reply-markup message manually, because we need to set
	// the ReplyMarkup to nil, which is not supported by the library
	bs.botApi.Request(tgbotapi.EditMessageReplyMarkupConfig{
		BaseEdit: tgbotapi.BaseEdit{
			ChatID:      int64(bs.chatId),
			MessageID:   int(messageId),
			ReplyMarkup: nil,
		},
	})
}

func (bs *session[T]) handleCommand(command string, args []string) bool {
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
func (bs *session[T]) SetCommandHandler(name string, handler CommandHandler[T]) {
	bs.sessionCommandHandlers[name] = handler
}

func (bs *session[T]) PushState(state State[T]) {
	if len(bs.stateStack) > 0 {
		bs.CurrentState().BeforeLeave(bs)
	}
	bs.stateStack = append(bs.stateStack, state)
	state.Activate(bs)
}

func (bs *session[T]) PopState() {
	if len(bs.stateStack) == 0 {
		return
	}

	bs.CurrentState().BeforeLeave(bs)

	bs.stateStack = bs.stateStack[:len(bs.stateStack)-1]

	curState := bs.getOrPushCurrentState()

	curState.Return(bs)
}

func (bs *session[T]) DropStates(n int) {
	if len(bs.stateStack) > n {
		bs.stateStack = bs.stateStack[:len(bs.stateStack)-n]
	} else {
		bs.stateStack = nil
	}
	bs.getOrPushCurrentState().Return(bs)
}

func (bs *session[T]) CurrentState() State[T] {
	if len(bs.stateStack) == 0 {
		return nil
	}
	return bs.stateStack[len(bs.stateStack)-1]
}

func (bs *session[T]) ReplaceState(state State[T]) {
	if len(bs.stateStack) == 0 {
		return
	}

	bs.stateStack[len(bs.stateStack)-1] = state
	state.Activate(bs)
}

func (bs *session[T]) ResetToState(state State[T]) {
	bs.stateStack = nil
	bs.PushState(state)
}

func (bs *session[T]) UserId() UserId {
	return bs.userId
}

func (bs *session[T]) ChatId() ChatId {
	return bs.chatId
}

func (bs *session[T]) SendTemplateMessage(template string, values KeyValues, opts ...SendMessageOption) Message {
	template = strings.TrimSpace(template)
	value, err := RunTemplate(template, values...)
	if err != nil {
		bs.SendError(err)
	}
	return bs.SendMessage(value, opts...)
}

func (bs *session[T]) SendMessage(text string, opts ...SendMessageOption) Message {
	msg := tgbotapi.NewMessage(int64(bs.ChatId()), text)
	msg.ParseMode = "html"

	options := &sendMessageOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if options.keyboard != nil {
		keyboard := tgbotapi.ReplyKeyboardMarkup{
			ResizeKeyboard: true,
		}
		for _, row := range options.keyboard.Buttons() {
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
	return &message{messageId: sentMsg.MessageID}
}

func (bs *session[T]) SendError(err error) {
	_, sendErr := bs.botApi.Send(tgbotapi.NewMessage(int64(bs.ChatId()), fmt.Sprintf("error: %v", err)))
	if sendErr != nil {
		log.Printf("Error sending error: %v", sendErr)
	}
}

type (
	sendMessageOptions struct {
		keyboard       Keyboard
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
func SendMessageWithKeyboard(keyboard Keyboard) SendMessageOption {
	return func(opts *sendMessageOptions) {
		opts.keyboard = keyboard
	}
}

func (bs *session[T]) UpdateMessageForCallback(queryId string, messageId MessageId, text string, opts ...SendMessageOption) {
	edit := tgbotapi.EditMessageTextConfig{
		BaseEdit: tgbotapi.BaseEdit{
			ChatID:    int64(bs.chatId),
			MessageID: int(messageId),
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

func (bs *session[T]) c(err error) {
	_, sendErr := bs.botApi.Send(tgbotapi.NewMessage(int64(bs.ChatId()), fmt.Sprintf("error: %v", err)))
	if sendErr != nil {
		log.Printf("Error sending error: %v", sendErr)
	}
}

func (bs *session[T]) Fail(message string, formatErrorMsg string, args ...interface{}) {
	log.Printf(formatErrorMsg, args...)
	bs.SendMessage(message)
	bs.PopState()
}

func (bs *session[T]) BotName() (string, error) {
	me, err := bs.botApi.GetMe()
	if err != nil {
		return "", fmt.Errorf("error getting bot identity: %v", err)
	}
	return me.UserName, nil
}

func (bs *session[T]) Shutdown() {
	for i := len(bs.stateStack) - 1; i >= 0; i-- {
		bs.stateStack[i].BeforeLeave(bs)
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
