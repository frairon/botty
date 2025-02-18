package botty

type InlineMessage[T any] interface {
	Update(text string, keyboard *InlineKeyboard2[T])
	RemoveKeyboard()
	Text() string
	ID() MessageId
	handleQuery(queryId string) bool
}

type inlineMessage[T any] struct {
	*message[T]
	keyboard *InlineKeyboard2[T]
}

func (im *inlineMessage[T]) Update(text string, keyboard *InlineKeyboard2[T]) {
	msg := im.session.updateMessage(im.messageId, text, SendMessageInlineKeyboard(keyboard.rows))

	im.text = msg.Text()
}
func (im *inlineMessage[T]) Text() string {
	return im.text
}
func (im *inlineMessage[T]) ID() MessageId {
	return im.messageId
}
func (im *inlineMessage[T]) RemoveKeyboard() {
	im.session.RemoveKeyboardForMessage(im.messageId)
}
func (im *inlineMessage[T]) handleQuery(data string) bool {
	return im.keyboard.handle(im.session, im, data)
}
