package botty

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func UsersList[T any](uStorage UserManager) State[T] {
	var (
		Add    Button = "➕ Add"
		Back   Button = "↩ Back"
		Delete Button = "❌ Delete"
	)

	var users []User

	return &functionState[T]{
		activate: func(bs Session[T]) {
			var err error
			users, err = uStorage.ListUsers()
			if err != nil {
				bs.Fail("Cannot list users", "error reading users: %v", err)
				return
			}

			content, err := RunTemplate(`All Users
{{divider}}
{{- if .users -}}
{{- range $idx, $user:= .users }}
[{{$idx}}] {{$user.Name}} ({{$user.ID}})
{{- end -}}
{{- else }}
- no users registered -
{{- end -}}`, kv("users", users))

			if err != nil {
				bs.Fail("Cannot list users", "error reading users: %v", err)
				return
			}

			bs.SendMessageWithCommands(content, NewButtonKeyboard(newRow(Back),
				newRow(Add, Delete)))
		},
		handleMessage: func(bs Session[T], message *tgbotapi.Message) {
			botName, err := bs.BotName()
			if err != nil {
				bs.Fail("Cannot find bot identity", "error getting bot name: %v", err)
				return
			}

			switch Button(message.Text) {
			case Back:
				bs.PopState()
			case Add:
				text, err := RunTemplate(`The bot is now set to ACCEPT-mode, allowing new users to join.
This will be disabled automatically after 10 minutes.
Tell you friend to contact bot @{{.botName}} now.`, kv("botName", botName))
				if err != nil {
					bs.Fail("error rendering template", "error rendering template: %v", err)
					return
				}
				bs.SendMessageWithCommands(text, nil)
				bs.AcceptUsers(10 * time.Minute)
			case Delete:
				bs.PushState(SelectToDeleteUser[T](uStorage, users))
			}
		},
	}
}

func SelectToDeleteUser[T any](uStorage UserManager, users []User) State[T] {
	var Back Button = "Back"
	return &functionState[T]{
		activate: func(bs Session[T]) {
			bs.SendMessageWithCommands("Select user to delete", NewButtonKeyboard(newRow(Back)))
		},
		handleMessage: func(bs Session[T], msg *tgbotapi.Message) {
			selector := strings.TrimSpace(msg.Text)

			idx, err := strconv.ParseInt(selector, 10, 32)
			if err != nil || idx < 0 || int(idx) >= len(users) {
				bs.SendMessage(fmt.Sprintf("Cannot find user by '%s'. Enter valid index.", selector))
				return
			}

			user := users[idx]

			bs.ReplaceState(PromptState[T](func() {
				err := uStorage.DeleteUser(user.ID)
				if err != nil {
					log.Printf("error deleting item %#v: %v", user, err)
					bs.SendMessage("error deleting user")
				}
			}))
		},
	}
}

func findNameForUser(user *tgbotapi.User) string {
	name := user.UserName
	if name == "" {
		name = user.FirstName
	}
	if name == "" {
		name = user.LastName
	}
	if name == "" {
		name = "Unknown"
	}
	return name
}
