package botty

import (
	"fmt"
	"strconv"
	"strings"
)

type promptOptions struct {
	dropStates int
	message    string
}

type PromptOption func(opts *promptOptions)

func PromptDropStates(states int) PromptOption {
	return func(opts *promptOptions) {
		opts.dropStates = states
	}
}

func PromptMessagef(format string, args ...interface{}) PromptOption {
	return func(opts *promptOptions) {
		opts.message = fmt.Sprintf(format, args...)
	}
}

func PromptState[T any](yesHandler func(), options ...PromptOption) State[T] {
	const (
		Yes    Button = "⚠ Yes"
		Cancel Button = "Cancel"
	)

	opts := &promptOptions{
		dropStates: 1,
		message:    "Are you sure?",
	}

	for _, option := range options {
		option(opts)
	}

	return &functionState[T]{
		activate: func(bs Session[T]) {
			bs.SendMessage(opts.message, SendMessageWithKeyboard(NewButtonKeyboard(newRow(Yes, Cancel))))
		},

		handleMessage: func(bs Session[T], message ChatMessage) {
			switch Button(message.Text()) {
			case Cancel:
				bs.SendMessage("Aborted.")
				bs.DropStates(opts.dropStates)
			case Yes:
				yesHandler()
				bs.DropStates(opts.dropStates)
			}
		},
	}
}

func SelectState[O, T any](text string, items []O, accept func(bs Session[T], item O)) State[T] {
	return &functionState[T]{
		activate: func(bs Session[T]) {
			bs.SendMessage(text)
			bs.SendMessage(fmt.Sprintf("Please enter index (0-%d)", len(items)-1))
		},
		handleMessage: func(bs Session[T], msg ChatMessage) {
			selector := strings.TrimSpace(msg.Text())

			idx, err := strconv.ParseInt(selector, 10, 32)
			if err != nil || idx < 0 || int(idx) >= len(items) {
				bs.SendMessage(fmt.Sprintf("Cannot find Item by '%s'. Enter valid item.", selector))
				return
			}

			accept(bs, items[idx])
			bs.PopState()
		},
	}
}

func TernaryButton(cond bool, trueButton, falseButton InlineButton) InlineButton {
	if cond {
		return trueButton
	}
	return falseButton
}

// func NewMultiMessageHandler[T any](handlers ...InlineMessageHandler[T]) State[T] {
// 	handlersByMsg := map[int]InlineMessageHandler[T]{}

// 	fs := &functionState[T]{
// 		activate: func(bs Session[T]) {
// 			for _, handler := range handlers {
// 				msg, keyboard, err := handler(bs, "")
// 				if err != nil {
// 					bs.SendError(err)
// 					return
// 				}
// 				msg := bs.SendMessage(msg, SendMessageInlineKeyboard(keyboard))
// 				handlersByMsg[msgId] = handler
// 			}
// 		},
// 		callbackQueryHandler: func(bs Session[T], query *tgbotapi.CallbackQuery) bool {
// 			handler := handlersByMsg[query.Message.MessageID]

// 			if handler == nil {
// 				log.Printf("did not find handler for message")
// 				return false
// 			}
// 			content, keyboard, err := handler(bs, query.Data)
// 			if err != nil {
// 				bs.SendError(err)
// 				return true
// 			}
// 			if content != "" && keyboard != nil {
// 				bs.UpdateMessageForCallback(query.ID,
// 					query.Message.MessageID,
// 					content,
// 					SendMessageInlineKeyboard(keyboard),
// 				)
// 			}
// 			return true
// 		},
// 		beforeLeaveHandler: func(bs Session[T]) {
// 			for msgId := range handlersByMsg {
// 				bs.RemoveKeyboardForMessage(msgId)
// 			}
// 		},
// 	}
// 	return fs
// }
// type InlineMessageHandler[T any] func(bs Session[T], query string) (string, InlineKeyboard, error)

// func NewMessageHandler[T any](handleQuery InlineMessageHandler[T]) State[T] {
// 	var lastMessageId int

// 	fs := &functionState[T]{
// 		activate: func(bs Session[T]) {
// 			msg, keyboard, err := handleQuery(bs, "")
// 			if err != nil {
// 				bs.SendError(err)
// 				return
// 			}
// 			lastMessageId = bs.SendMessage(msg, SendMessageInlineKeyboard(keyboard))
// 		},
// 		callbackQueryHandler: func(bs Session[T], query *tgbotapi.CallbackQuery) bool {
// 			log.Printf("callback: %#v", query)
// 			content, keyboard, err := handleQuery(bs, query.Data)
// 			if err != nil {
// 				bs.SendError(err)
// 				return true
// 			}
// 			if content != "" && keyboard != nil {
// 				bs.UpdateMessageForCallback(query.ID,
// 					query.Message.MessageID,
// 					content,
// 					SendMessageInlineKeyboard(keyboard),
// 				)
// 			}
// 			return true
// 		},
// 		beforeLeaveHandler: func(bs Session[T]) {
// 			if lastMessageId != 0 {
// 				bs.RemoveKeyboardForMessage(lastMessageId)
// 			}
// 		},
// 	}
// 	return fs
// }
