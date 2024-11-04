package botty

import (
	"strings"
)

type CommandHandler[T any] interface {
	Handle(bs Session[T], command string, args ...string) bool
}

type FuncCommandHandler[T any] func(bs Session[T], command string, args ...string) bool

func (f FuncCommandHandler[T]) Handle(bs Session[T], command string, args ...string) bool {
	return f(bs, command, args...)
}

type HandlerMap[T any] map[string]CommandHandler[T]

func (hm HandlerMap[T]) Handle(bs Session[T], command string, args ...string) bool {
	cmd, ok := hm[command]
	if !ok {
		return false
	}
	return cmd.Handle(bs, command, args...)
}

func (hm HandlerMap[T]) Set(command string, sc CommandHandler[T]) {
	hm[strings.TrimPrefix(command, "/")] = sc
}
