package botty

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type GlobalMessageHandler[T any] func(bs Session[T], message *tgbotapi.Message) bool

// remove that type. It indicates we could use it outside of a session but we shouldn't. Instead
// the context should have an updater-interface that modifies a messsage and the message-id becomes its own (int)-type
type Message interface {
	Update(text string)
	UpdateTemplate(template string, values KeyValues)

	ID() MessageId
	Text() string
}

type message[T any] struct {
	messageId MessageId // use this in the state
	// if we add a bot-session, do not marshal that to state but inject when unmarshalling

	text string
	bot  *Bot[T]

	session Session[T]
}

func (m *message[T]) Text() string {
	return m.text
}
func (m *message[T]) UpdateTemplate(template string, values KeyValues) {
	value, err := RunTemplate(template, values...)
	if err != nil {
		m.session.SendError(err)
		return
	}
	m.Update(value)
}
func (m *message[T]) Update(text string) {
	edit := tgbotapi.EditMessageTextConfig{
		BaseEdit: tgbotapi.BaseEdit{
			ChatID:    int64(m.session.ChatId()),
			MessageID: int(m.messageId),
		},
		Text:      text,
		ParseMode: "html",
	}

	resp, err := m.bot.botApi.Request(edit)

	// update internal text
	m.text = text

	if err != nil {
		m.bot.handleError(m.session, fmt.Errorf("error updating message: %v, response: %#v", err, resp))
	}
}
func (m *message[T]) RemoveKeyboardForMessage() {
	m.bot.botApi.Request(tgbotapi.EditMessageReplyMarkupConfig{
		BaseEdit: tgbotapi.BaseEdit{
			ChatID:      int64(m.session.ChatId()),
			MessageID:   int(m.messageId),
			ReplyMarkup: nil,
		},
	})
}

func (m *message[T]) ID() MessageId {
	return m.messageId
}

type Session[T any] interface {
	SendMessage(text string, opts ...SendMessageOption) Message
	SendTemplateMessage(template string, values KeyValues, opts ...SendMessageOption) Message

	updateMessage(messageId MessageId, text string, opts ...SendMessageOption) Message
	updateInlineMessage(queryId string, messageId MessageId, text string, opts ...SendMessageOption) Message

	Fail(message string, formatErrorMsg string, args ...interface{})

	RootState() State[T]
	PushState(state State[T])
	PopState()
	ReplaceState(state State[T])
	ResetToState(state State[T])
	DropStates(n int)
	SendError(err error)
	SendErrorf(format string, args ...interface{})
	CurrentState() State[T]

	SendInlineMessage(text string, handler func(bs Session[T], message InlineMessage[T], query string) bool, opts ...SendMessageOption) InlineMessage[T]

	// re-enters the current state.
	Reenter()

	RemoveKeyboardForMessage(messageId MessageId)

	// returns the current user ID
	UserId() UserId

	ChatId() ChatId

	AcceptUsers(duration time.Duration)

	BotName() (string, error)

	Context() context.Context

	State() T

	LastUserAction() time.Time
}

type session[T any] struct {
	botApi TGApi

	userId UserId
	chatId ChatId

	// session state the app
	appState T

	bot *Bot[T]

	lastUserAction time.Time

	mMessages             sync.Mutex
	currentInlineMessages map[MessageId]InlineMessage[T]
	stateStack            []State[T]

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
		currentInlineMessages:  map[MessageId]InlineMessage[T]{},
	}

}

func (bs *session[T]) State() T {
	return bs.appState
}

func (bs *session[T]) Context() context.Context {
	return bs.botCtx
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

func (bs *session[T]) LastUserAction() time.Time {
	return bs.lastUserAction
}

func (bs *session[T]) Reenter() {
	bs.CurrentState().Enter(bs)
}

func (bs *session[T]) SendInlineMessage(text string, handler func(bs Session[T], message InlineMessage[T], query string) bool, opts ...SendMessageOption) InlineMessage[T] {

	// send the initial message
	msg := bs.SendMessage(text, opts...)
	log.Printf("inline-message: sending message-id: %v", msg.ID())
	// create an inline message
	inMsg := &inlineMessage[T]{
		message: &message[T]{
			text:      msg.Text(),
			messageId: msg.ID(),
			session:   bs,
			bot:       bs.bot,
		},
	}
	inMsg.handler = handler

	// add handler to current stack's inline message handler
	bs.mMessages.Lock()
	defer bs.mMessages.Unlock()
	bs.currentInlineMessages[msg.ID()] = inMsg

	return inMsg
}

func (bs *session[T]) Handle(update tgbotapi.Update) bool {
	curState := bs.getOrPushCurrentState()

	bs.lastUserAction = time.Now()

	switch {
	case update.Message != nil:

		// if the message is a command, try to handle that instead.
		// First the current state, then the context
		if cmd := update.Message.CommandWithAt(); cmd != "" {
			args := strings.Split(update.Message.CommandArguments(), " ")
			if curState.HandleCommand(bs, cmd, args...) {
				return true
			}
			return bs.handleCommand(cmd, args)
		}

		return curState.HandleMessage(bs, &tgMessage{m: update.Message})
	case update.CallbackQuery != nil:

		if curState.HandleCallbackQuery(bs, &tgCbQuery{m: update.CallbackQuery}) {
			return true
		}
		bs.mMessages.Lock()
		log.Printf("message-id in callback-query: %v", update.CallbackQuery.Message.MessageID)
		handler, has := bs.currentInlineMessages[MessageId(update.CallbackQuery.Message.MessageID)]
		bs.mMessages.Unlock()

		if has && handler != nil {
			// try to handle
			if handler.handleQuery(update.CallbackQuery.Data) {
				// confirm response
				bs.botApi.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
				return true
			}
		}

		return bs.removeExpiredCallback(update.CallbackQuery)
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

func (bs *session[T]) removeCurrentInlineMessages() {
	bs.mMessages.Lock()
	defer bs.mMessages.Unlock()
	for _, m := range bs.currentInlineMessages {
		m.RemoveKeyboard()
	}

	// reset inline messages
	bs.currentInlineMessages = map[MessageId]InlineMessage[T]{}
}

func (bs *session[T]) PushState(state State[T]) {
	if len(bs.stateStack) > 0 {
		bs.CurrentState().Leave(bs)
	}

	bs.removeCurrentInlineMessages()
	bs.stateStack = append(bs.stateStack, state)
	state.Enter(bs)
}

func (bs *session[T]) PopState() {
	if len(bs.stateStack) == 0 {
		return
	}

	bs.CurrentState().Leave(bs)

	bs.removeCurrentInlineMessages()

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
	state.Enter(bs)
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

	return &message[T]{messageId: MessageId(sentMsg.MessageID), text: sentMsg.Text, bot: bs.bot, session: bs}
}

func (bs *session[T]) SendError(err error) {
	bs.bot.handleError(bs, err)
}

func (bs *session[T]) SendErrorf(format string, args ...interface{}) {
	bs.SendError(fmt.Errorf(format, args...))
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

func (bs *session[T]) updateTemplateMessage(messageId MessageId, template string, values KeyValues, opts ...SendMessageOption) Message {
	value, err := RunTemplate(template, values...)
	if err != nil {
		bs.SendError(err)
	}

	return bs.updateMessage(messageId, value, opts...)
}
func (bs *session[T]) updateMessage(messageId MessageId, text string, opts ...SendMessageOption) Message {

	if messageId == 0 {
		return bs.SendMessage(text, opts...)
	}
	edit := tgbotapi.EditMessageTextConfig{
		BaseEdit: tgbotapi.BaseEdit{
			ChatID:    int64(bs.chatId),
			MessageID: int(messageId),
		},
		Text:      text,
		ParseMode: "html",
	}

	log.Printf("edit: %#v", edit)

	options := &sendMessageOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if len(options.inlineKeyboard) > 0 {
		edit.BaseEdit.ReplyMarkup = convertToMarkup(options.inlineKeyboard)
	}
	resp, err := bs.botApi.Request(edit)
	if err != nil {
		log.Printf("error updating message: %v, response: %#v", err, resp)
	}

	return &message[T]{messageId: messageId, text: text}
}

func (bs *session[T]) updateInlineMessage(queryId string, messageId MessageId, text string, opts ...SendMessageOption) Message {
	msg := bs.updateMessage(messageId, text, opts...)

	bs.botApi.Request(tgbotapi.NewCallback(queryId, ""))

	return msg
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
		bs.stateStack[i].Leave(bs)
	}
	// remove inline messages
	bs.removeCurrentInlineMessages()
}

// TODO remove
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
