package domain

import "slices"

// Role is the author of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one message of a chat.
type Message struct {
	Role    Role
	Content string
}

// Conversation is an ordered list of messages. Its methods never change the
// receiver.
type Conversation []Message

// With returns a new conversation with msgs at the end.
func (c Conversation) With(msgs ...Message) Conversation {
	return append(slices.Clone(c), msgs...)
}
