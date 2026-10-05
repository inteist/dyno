package prompt

import "crypto/rand"

// IDSource gives unique tokens. The scenarios put them into prompts, and the
// runner puts one into the session ID.
type IDSource interface{ Next() string }

// idLen is the number of characters of a RandomIDs token.
const idLen = 8

// RandomIDs gives random tokens such as "K3F9A2QX".
type RandomIDs struct{}

// Next returns a new random token of idLen base32 characters.
func (RandomIDs) Next() string { return rand.Text()[:idLen] }
