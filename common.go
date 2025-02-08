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
		onEnter: func(bs Session[T]) {
			bs.SendMessage(opts.message, SendMessageWithKeyboard(NewButtonKeyboard(NewRow(Yes, Cancel))))
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
		onEnter: func(bs Session[T]) {
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

// der hier ist Mist.
// Besser ist wenn wir in der session eine inline-message erstellen, mit einem update-interface.
// Gleichzeitig registriert sich die message in der session als handler und auch in einer art leave-hook
// um sich selbst zu deaktivieren.
// Vielleicht eine Art shutdown-hook der sowohl beim leave als auch beim shutdown ausgeführt wird, jedoch der leave-hook ja nicht beim shutdown, weil nicht der komplette
// stack abgebaut wird, wenn sich die app schließt.
func NewMultiInlineMessageState[T any](handlers ...InlineMessageHandler[T]) State[T] {
	handlersByMsg := map[MessageId]InlineMessageHandler[T]{}

	return NewStateBuilder[T]().
		OnEnter(func(bs Session[T]) {

			// execute all handlers, which essentially provide message text and an inline-keyboard
			for _, handler := range handlers {
				// the initial state of the inline-message is triggered by calling the handler with an empty query
				msg, keyboard, err := handler(bs, "")
				if err != nil {
					bs.SendError(err)
					return
				}

				// store the messages in a map along with the handlers
				msgId := bs.SendMessage(msg, SendMessageInlineKeyboard(keyboard)).ID()
				handlersByMsg[msgId] = handler
			}
		}).
		OnCallbackQuery(func(bs Session[T], query CallbackQuery) bool {
			handler := handlersByMsg[query.MessageID()]

			if handler == nil {
				bs.SendErrorf("did not find handler for message")
				return false
			}
			content, keyboard, err := handler(bs, query.Data())
			if err != nil {
				bs.SendErrorf("error executing query handler: %w", err)
				return false
			}
			if content != "" && keyboard != nil {
				bs.updateInlineMessage(query.ID(),
					query.MessageID(),
					content,
					SendMessageInlineKeyboard(keyboard),
				)
			}
			return true
		}).
		OnLeave(func(bs Session[T]) {
			// on leaving, remove all keyboards from all messages
			for msgId := range handlersByMsg {
				bs.RemoveKeyboardForMessage(MessageId(msgId))
			}
		}).Build()
}

// func NewMessageHandler[T any](handleQuery InlineMessageHandler[T]) State[T] {
// 	var lastMessageId MessageId

// 	return NewStateBuilder[T]().
// 		OnEnter(func(bs Session[T]) {
// 			msg, keyboard, err := handleQuery(bs, "")
// 			if err != nil {
// 				bs.SendError(err)
// 				return
// 			}
// 			lastMessageId = bs.SendMessage(msg, SendMessageInlineKeyboard(keyboard)).ID()
// 		}).
// 		OnCallbackQuery(func(bs Session[T], query CallbackQuery) bool {
// 			log.Printf("callback: %#v", query)
// 			content, keyboard, err := handleQuery(bs, query.Data())
// 			if err != nil {
// 				bs.SendError(err)
// 				return true
// 			}
// 			if content != "" && keyboard != nil {
// 				bs.updateInlineMessage(query.ID(),
// 					query.MessageID(),
// 					content,
// 					SendMessageInlineKeyboard(keyboard),
// 				)
// 			}
// 			return true
// 		}).
// 		OnLeave(func(bs Session[T]) {
// 			if lastMessageId != 0 {
// 				bs.RemoveKeyboardForMessage(MessageId(lastMessageId))
// 			}
// 		}).
// 		Build()

// }

type InlineMessageHandler[T any] func(bs Session[T], query string) (string, InlineKeyboard, error)
