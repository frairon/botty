package botty

type ButtonHandler[T any] interface {
	Button() Button
	Handle(Session[T], ChatMessage)
}

type KeyHandler[T any] interface {
	NextRow() KeyHandler[T]
	AddButton(button Button, handler func(Session[T], ChatMessage)) KeyHandler[T]
	AutoLayout(cols int) KeyHandler[T]
	Reset()

	// returns the keyboard to be attached to a message
	Keyboard() Keyboard

	handle(bs Session[T], msg ChatMessage) bool

	// returns all current button handlers.
	ButtonHandlers() []ButtonHandler[T]
}

type InlineKeyboard2[T any] struct {
	rows     []InlineRow
	handlers map[string]func(Session[T], InlineMessage[T]) bool
}

func NewInlineKeyboard2[T any]() *InlineKeyboard2[T] {
	return &InlineKeyboard2[T]{
		handlers: map[string]func(Session[T], InlineMessage[T]) bool{},
	}
}

func (ik *InlineKeyboard2[T]) NextRow() *InlineKeyboard2[T] {
	if len(ik.rows) == 0 || len(ik.rows[len(ik.rows)-1]) == 0 {
		ik.rows = append(ik.rows, []InlineButton{})
	}
	return ik
}
func (ik *InlineKeyboard2[T]) AddButton(label string, handler func(Session[T], InlineMessage[T]) bool) *InlineKeyboard2[T] {
	data := label
	ik.handlers[data] = handler

	if len(ik.rows) == 0 {
		// no rows exist, create one with the passed button
		ik.rows = []InlineRow{NewInlineRow(NewInlineButton(label, data))}
	} else {
		// append to last row
		ik.rows[len(ik.rows)-1] = append(ik.rows[len(ik.rows)-1], NewInlineButton(label, data))
	}
	return ik
}
func (ik *InlineKeyboard2[T]) AutoLayout(cols int) *InlineKeyboard2[T] {
	var newRows []InlineRow

	if cols <= 0 {
		panic("cannot layout with zero columns")
	}

	for _, r := range ik.rows {
		for _, b := range r {
			if len(newRows) == 0 || len(newRows[len(newRows)-1]) >= cols {
				newRows = append(newRows, nil)
			}
			newRows[len(newRows)-1] = append(newRows[len(newRows)-1], b)
		}
	}
	ik.rows = newRows
	return ik
}
func (ik *InlineKeyboard2[T]) Reset() *InlineKeyboard2[T] {
	ik.rows = nil
	ik.handlers = map[string]func(Session[T], InlineMessage[T]) bool{}
	return ik
}
func (ik *InlineKeyboard2[T]) handle(bs Session[T], msg InlineMessage[T], data string) bool {
	if handler, ok := ik.handlers[data]; ok {
		return handler(bs, msg)
	}
	return false
}

type keyHandler[T any] struct {
	rows     []ButtonRow
	handlers map[string]func(Session[T], ChatMessage)
}

// Creates a new keyboard
func NewKeyHandler[T any]() KeyHandler[T] {
	return &keyHandler[T]{
		handlers: map[string]func(Session[T], ChatMessage){},
	}
}

// Breaks the current button-row and adds a next row, which
// following calls to AddButton will be added to
func (mb *keyHandler[T]) NextRow() KeyHandler[T] {
	if len(mb.rows) == 0 || len(mb.rows[len(mb.rows)-1]) == 0 {
		mb.rows = append(mb.rows, []Button{})
	}
	return mb
}

func (mb *keyHandler[T]) Reset() {
	mb.rows = nil
	mb.handlers = map[string]func(Session[T], ChatMessage){}
}

// Adds a new button
func (mb *keyHandler[T]) AddButton(button Button, handler func(Session[T], ChatMessage)) KeyHandler[T] {
	mb.handlers[string(button)] = handler

	if len(mb.rows) == 0 {
		// no rows exist, create one with the passed button
		mb.rows = []ButtonRow{{button}}
	} else {
		// append to last row
		mb.rows[len(mb.rows)-1] = append(mb.rows[len(mb.rows)-1], button)
	}
	return mb
}

// Creates a coloumn layout by breaking the buttons into rows according to the passed number
// of columns.
// If previously NextRow() was used, this will be ignored
func (mb *keyHandler[T]) AutoLayout(cols int) KeyHandler[T] {
	var newRows []ButtonRow

	if cols <= 0 {
		panic("cannot layout with zero columns")
	}

	for _, r := range mb.rows {
		for _, b := range r {
			if len(newRows) == 0 || len(newRows[len(newRows)-1]) >= cols {
				newRows = append(newRows, nil)
			}
			newRows[len(newRows)-1] = append(newRows[len(newRows)-1], b)
		}
	}
	mb.rows = newRows
	return mb
}

func (mb *keyHandler[T]) Keyboard() Keyboard {
	return NewButtonKeyboard(mb.rows...)
}

func (mb *keyHandler[T]) handle(bs Session[T], msg ChatMessage) bool {
	if handler, ok := mb.handlers[msg.Text()]; ok {
		handler(bs, msg)
		return true
	}
	return false
}

func NewButtonHandler[T any](button Button, handler func(Session[T], ChatMessage)) ButtonHandler[T] {
	return &buttonHandler[T]{
		button:  button,
		handler: handler,
	}
}

type buttonHandler[T any] struct {
	button  Button
	handler func(Session[T], ChatMessage)
}

func (bh *buttonHandler[T]) Button() Button {
	return bh.button
}
func (bh *buttonHandler[T]) Handle(s Session[T], msg ChatMessage) {
	bh.handler(s, msg)
}

func (mb *keyHandler[T]) ButtonHandlers() []ButtonHandler[T] {
	h := make([]ButtonHandler[T], 0, len(mb.handlers))
	for button, handler := range mb.handlers {
		h = append(h, &buttonHandler[T]{
			button:  Button(button),
			handler: handler,
		})
	}
	return h
}
