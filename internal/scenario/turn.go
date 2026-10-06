package scenario

import (
	"dyno/internal/domain"
	"dyno/internal/prompt"
)

// idPosition tells where the unique ID goes. The position decides what the
// server can take from its cache.
type idPosition int

const (
	beforePrefix   idPosition = iota // nothing is reusable
	beforeQuestion                   // the system part is reusable
)

// marker returns the text that carries id in a prompt, for example
// "[run K3F9A2QX] ".
func marker(id string) string { return "[run " + id + "] " }

// firstTurn builds the first turn of a new chat with the marker of id in it.
// The marker goes in front of the system part for beforePrefix, and in front
// of the question for beforeQuestion or for a set without a system part.
func firstTurn(set prompt.Set, id string, pos idPosition) domain.Conversation {
	system, question := set.System, set.Question
	if pos == beforePrefix && system != "" {
		system = marker(id) + system
	} else {
		question = marker(id) + question
	}
	var conv domain.Conversation
	if system != "" {
		conv = append(conv, domain.Message{Role: domain.RoleSystem, Content: system})
	}
	return append(conv, domain.Message{Role: domain.RoleUser, Content: question})
}

// turns makes first turns of one prompt set, each with a new ID.
type turns struct {
	set prompt.Set
	ids prompt.IDSource
}

// next returns a first turn with a new ID at pos.
func (t turns) next(pos idPosition) domain.Conversation {
	return firstTurn(t.set, t.ids.Next(), pos)
}
