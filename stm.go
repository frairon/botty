package botty

type (
	Button         string
	ButtonRow      []Button
	buttonKeyboard []ButtonRow
)

type Keyboard interface {
	Buttons() []ButtonRow
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
	Activate(bs Session[T])
	Return(bs Session[T])
	HandleMessage(bs Session[T], msg ChatMessage) bool
	HandleCommand(bs Session[T], command string, args ...string) bool
	HandleCallbackQuery(bs Session[T], query CallbackQuery) bool

	// called before leaving the state (either by pushing another state on top of it or popping it)
	BeforeLeave(bs Session[T])
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
	activate             func(bs Session[T])
	returner             func(bs Session[T])
	handleMessage        func(bs Session[T], message ChatMessage)
	buttonHandler        map[Button]func(bs Session[T], message ChatMessage)
	commandHandler       func(bs Session[T], command string, args ...string) bool
	callbackQueryHandler func(bs Session[T], query CallbackQuery) bool
	queryDataHandler     map[string]func(bs Session[T], query CallbackQuery) bool
	beforeLeaveHandler   func(bs Session[T])
}

func (fs *functionState[T]) Activate(bs Session[T]) {
	fs.activate(bs)
}

func (fs *functionState[T]) Return(bs Session[T]) {
	if fs.returner != nil {
		fs.returner(bs)
	} else {
		fs.activate(bs)
	}
}

func (fs *functionState[T]) HandleMessage(bs Session[T], message ChatMessage) bool {
	if fs.handleMessage == nil {
		return false
	}

	if buttonHandler, ok := fs.buttonHandler[Button(message.Text())]; ok {
		buttonHandler(bs, message)
		return true
	}

	fs.handleMessage(bs, message)
	return true
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

func (fs *functionState[T]) BeforeLeave(bs Session[T]) {
	if fs.beforeLeaveHandler != nil {
		fs.beforeLeaveHandler(bs)
	}
}

type StateBuilder[T any] struct {
	fs *functionState[T]
}

func NewStateBuilder[T any]() *StateBuilder[T] {
	return &StateBuilder[T]{
		fs: &functionState[T]{
			buttonHandler:    make(map[Button]func(bs Session[T], message ChatMessage)),
			queryDataHandler: make(map[string]func(bs Session[T], query CallbackQuery) bool),
		},
	}
}

func (sb *StateBuilder[T]) OnActivate(activator func(bs Session[T])) *StateBuilder[T] {
	sb.fs.activate = activator
	return sb
}

func (sb *StateBuilder[T]) OnMessage(handleMessage func(bs Session[T], message ChatMessage)) *StateBuilder[T] {
	sb.fs.handleMessage = handleMessage
	return sb
}

func (sb *StateBuilder[T]) OnButton(button Button, handler func(bs Session[T], message ChatMessage)) *StateBuilder[T] {
	sb.fs.buttonHandler[button] = handler
	// TODO handle the button in the handler
	return sb
}

func (sb *StateBuilder[T]) OnBeforeLeave(handler func(bs Session[T])) *StateBuilder[T] {
	// TODO	implement
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
	if sb.fs.activate == nil {
		sb.fs.activate = func(bs Session[T]) {
			bs.SendMessage("Default State")
		}
	}
	return sb.fs
}
