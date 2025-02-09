package botty

import "fmt"

type (
	Button         string
	ButtonRow      []Button
	ButtonRows     []ButtonRow
	buttonKeyboard []ButtonRow
)

type Keyboard interface {
	Buttons() []ButtonRow
}

func Buttonf(format string, args ...interface{}) Button {
	return Button(fmt.Sprintf(format, args...))
}

func (bk buttonKeyboard) Buttons() []ButtonRow {
	return bk
}
func (c Button) Is(val string) bool {
	return string(c) == val
}

func (c Button) S() string {
	return string(c)
}

type StateFactory[T any] func() State[T]

type State[T any] interface {
	Enter(bs Session[T])
	// called before leaving the state (either by pushing another state on top of it or popping it)
	Leave(bs Session[T])

	// called before returning to this state by popping the "next" state.
	// If there is no specific return function defined, will call Enter again.
	Return(bs Session[T])

	HandleMessage(bs Session[T], msg ChatMessage) bool
	HandleCommand(bs Session[T], command string, args ...string) bool
	HandleCallbackQuery(bs Session[T], query CallbackQuery) bool
}

func NewButtonKeyboard(rows ...ButtonRow) Keyboard {
	return buttonKeyboard(rows)
}

var NoButtons buttonKeyboard = nil

func NewConditionalRow(condition func() bool, row ButtonRow) ButtonRow {
	if condition() {
		return row
	}
	return nil
}

func NewRow(commands ...Button) ButtonRow {
	return ButtonRow(commands)
}

func ConditionalButton(condition func() bool, trueButton, falseButton Button) Button {
	if condition() {
		return trueButton
	}
	return falseButton
}

type (
	InlineButton struct {
		Label string
		Data  string
	}
	InlineRow      []InlineButton
	InlineKeyboard []InlineRow
)

func NewInlineKeyboard(rows ...InlineRow) InlineKeyboard {
	return InlineKeyboard(rows)
}

func NewInlineRow(buttons ...InlineButton) InlineRow {
	return InlineRow(buttons)
}

func NewInlineButton(label, data string) InlineButton {
	return InlineButton{
		Label: label,
		Data:  data,
	}
}

type InlineButtonAction[T any] struct {
	Label  string
	Data   string
	Action func(param T) error
}

func NewInlineButtonAction[T any](label, data string, action func(param T) error) *InlineButtonAction[T] {
	return &InlineButtonAction[T]{
		Label:  label,
		Data:   data,
		Action: action,
	}
}

type DynamicKeyboard[T any] struct {
	handlers map[Button]func(bs Session[T])
	rows     []ButtonRow
}

func NewDynamicKeyboard[T any]() *DynamicKeyboard[T] {
	return &DynamicKeyboard[T]{
		handlers: map[Button]func(bs Session[T]){},
	}
}

func (d *DynamicKeyboard[T]) AddButton(label string, handler func(bs Session[T]), startRowAfter int) {
	d.handlers[Button(label)] = handler
	if len(d.rows) == 0 {
		d.rows = append(d.rows, NewRow(Button(label)))
	} else {
		last := d.rows[len(d.rows)-1]

		if startRowAfter > 0 && len(last) >= startRowAfter {
			d.rows = append(d.rows, NewRow(Button(label)))
		} else {
			last = append(last, Button(label))
			d.rows[len(d.rows)-1] = last
		}
	}
}

func (d *DynamicKeyboard[T]) Reset() {
	d.handlers = map[Button]func(bs Session[T]){}
	d.rows = nil
}

func (d *DynamicKeyboard[T]) Handle(bs Session[T], button Button) bool {
	handler, ok := d.handlers[button]
	if ok {
		handler(bs)
		return true
	}
	return false
}

func (d *DynamicKeyboard[T]) Rows() []ButtonRow {
	// TODO: make a copy
	return d.rows
}

type functionState[T any] struct {
	onEnter  func(bs Session[T])
	onReturn func(bs Session[T])
	onLeave  func(bs Session[T])

	// generic handle message
	handleMessage func(bs Session[T], message ChatMessage)

	// handle commands
	commandHandler func(bs Session[T], command string, args ...string) bool

	// handlin callback queries
	callbackQueryHandler func(bs Session[T], query CallbackQuery) bool

	// handling query data
	queryDataHandler map[string]func(bs Session[T], query CallbackQuery) bool
}

func (fs *functionState[T]) Enter(bs Session[T]) {
	fs.onEnter(bs)
}

func (fs *functionState[T]) Return(bs Session[T]) {
	if fs.onReturn != nil {
		fs.onReturn(bs)
	} else {
		fs.onEnter(bs)
	}
}

func (fs *functionState[T]) HandleMessage(bs Session[T], message ChatMessage) bool {

	if fs.handleMessage != nil {
		fs.handleMessage(bs, message)
		return true
	}
	return false
}

func (fs *functionState[T]) HandleCommand(bs Session[T], command string, args ...string) bool {
	if fs.commandHandler != nil {
		return fs.commandHandler(bs, command, args...)
	}
	return false
}

func (fs *functionState[T]) HandleCallbackQuery(bs Session[T], query CallbackQuery) bool {
	if handler, ok := fs.queryDataHandler[query.Data()]; ok {
		return handler(bs, query)
	}
	if fs.callbackQueryHandler != nil {
		return fs.callbackQueryHandler(bs, query)
	}
	return false
}

func (fs *functionState[T]) Leave(bs Session[T]) {
	if fs.onLeave != nil {
		fs.onLeave(bs)
	}
}

type StateBuilder[T any] struct {
	// handlers by button
	buttonHandler map[Button]func(bs Session[T], message ChatMessage)

	keyHandlers []func(bs Session[T], message ChatMessage) bool

	fs *functionState[T]
}

func NewStateBuilder[T any]() *StateBuilder[T] {
	return &StateBuilder[T]{
		fs: &functionState[T]{
			queryDataHandler: make(map[string]func(bs Session[T], query CallbackQuery) bool),
		},
		buttonHandler: make(map[Button]func(bs Session[T], message ChatMessage)),
	}
}

func (sb *StateBuilder[T]) OnEnter(onEnter func(bs Session[T])) *StateBuilder[T] {
	sb.fs.onEnter = onEnter
	return sb
}

func (sb *StateBuilder[T]) AddMessageHandler(handleMessage func(bs Session[T], message ChatMessage) bool) *StateBuilder[T] {
	sb.keyHandlers = append(sb.keyHandlers, handleMessage)
	return sb
}

func (sb *StateBuilder[T]) OnButton(button Button, handler func(bs Session[T], message ChatMessage)) *StateBuilder[T] {
	sb.buttonHandler[button] = handler
	return sb
}
func (sb *StateBuilder[T]) OnButtonHandler(bhs ...ButtonHandler[T]) *StateBuilder[T] {
	for _, bh := range bhs {
		sb.buttonHandler[Button(bh.Button())] = bh.Handle
	}
	return sb
}

func (sb *StateBuilder[T]) OnLeave(handler func(bs Session[T])) *StateBuilder[T] {
	sb.fs.onLeave = handler
	return sb
}

func (sb *StateBuilder[T]) AddKeyHandler(keyHandler KeyHandler[T]) *StateBuilder[T] {
	sb.keyHandlers = append(sb.keyHandlers, keyHandler.handle)
	return sb
}

func (sb *StateBuilder[T]) OnCallbackQuery(handler func(bs Session[T], query CallbackQuery) bool) *StateBuilder[T] {
	sb.fs.callbackQueryHandler = handler
	return sb
}

func (sb *StateBuilder[T]) OnInlineButton(button InlineButton, handler func(bs Session[T], query CallbackQuery) bool) *StateBuilder[T] {
	sb.fs.queryDataHandler[button.Data] = handler
	return sb
}

func (sb *StateBuilder[T]) Build() State[T] {

	sb.fs.handleMessage = func(bs Session[T], message ChatMessage) {

		// try to handle with button handler
		if buttonHandler, ok := sb.buttonHandler[Button(message.Text())]; ok {
			buttonHandler(bs, message)
			return
		}

		// try all message handlers, if one of them will handle it
		for _, msgHandler := range sb.keyHandlers {
			if msgHandler(bs, message) {
				return
			}
		}
	}
	if sb.fs.onEnter == nil {
		sb.fs.onEnter = func(bs Session[T]) {
			bs.SendMessage("Default State")
		}
	}
	return sb.fs
}
