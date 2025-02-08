package botty

type InlineMessage[T any] interface {
	Update(text string, keyboard InlineKeyboard)
	RemoveKeyboard()
	Text() string
	ID() MessageId
	handleQuery(queryId string) bool
}

type inlineMessage[T any] struct {
	*message[T]
	handler func(bs Session[T], msg InlineMessage[T], query string) bool
}

func (im *inlineMessage[T]) Update(text string, keyboard InlineKeyboard) {
	msg := im.session.updateMessage(im.messageId, text, SendMessageInlineKeyboard(keyboard))

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
func (im *inlineMessage[T]) handleQuery(query string) bool {
	return im.handler(im.session, im, query)
}
