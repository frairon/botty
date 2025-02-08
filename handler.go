package botty

type ButtonHandler[T any] interface {
	Button() Button
	Handle(Session[T], ChatMessage)
}

type KeyboardBuilder[T any] interface {
	NextRow() KeyboardBuilder[T]
	AddButton(button Button, handler func(Session[T], ChatMessage)) KeyboardBuilder[T]
	AutoLayout(cols int) KeyboardBuilder[T]
	Keyboard() Keyboard
	ButtonHandlers() []ButtonHandler[T]
}

type keyboardBuilder[T any] struct {
	rows     []ButtonRow
	handlers map[string]func(Session[T], ChatMessage)
}

// Creates a new keyboard
func MakeKeyboard[T any]() KeyboardBuilder[T] {
	return &keyboardBuilder[T]{
		handlers: map[string]func(Session[T], ChatMessage){},
	}
}

// Breaks the current button-row and adds a next row, which
// following calls to AddButton will be added to
func (mb *keyboardBuilder[T]) NextRow() KeyboardBuilder[T] {
	if len(mb.rows) == 0 || len(mb.rows[len(mb.rows)-1]) == 0 {
		mb.rows = append(mb.rows, []Button{})
	}
	return mb
}

// Adds a new button
func (mb *keyboardBuilder[T]) AddButton(button Button, handler func(Session[T], ChatMessage)) KeyboardBuilder[T] {
	mb.handlers[string(button)] = handler

	if len(mb.rows) == 0 {
		// no rows exist, create one with the passed button
		mb.rows = []ButtonRow{ButtonRow{button}}
	} else {
		// append to last row
		mb.rows[len(mb.rows)-1] = append(mb.rows[len(mb.rows)-1], button)
	}
	return mb
}

// Creates a coloumn layout by breaking the buttons into rows according to the passed number
// of columns.
// If previously NextRow() was used, this will be ignored
func (mb *keyboardBuilder[T]) AutoLayout(cols int) KeyboardBuilder[T] {
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

func (mb *keyboardBuilder[T]) Keyboard() Keyboard {
	return NewButtonKeyboard(mb.rows...)
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

func (mb *keyboardBuilder[T]) ButtonHandlers() []ButtonHandler[T] {
	h := make([]ButtonHandler[T], 0, len(mb.handlers))
	for button, handler := range mb.handlers {
		h = append(h, &buttonHandler[T]{
			button:  Button(button),
			handler: handler,
		})
	}
	return h
}
