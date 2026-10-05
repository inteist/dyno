package domain

import "slices"

// Order selects the sequence of the steps in a session.
type Order string

const (
	OrderModel      Order = "model"       // all runs of a model, then the next model
	OrderRoundRobin Order = "round-robin" // run 1 of every model, then run 2, ...
)

// Orders returns every order.
func Orders() []Order { return []Order{OrderModel, OrderRoundRobin} }

// ParseOrder converts user input into an Order.
func ParseOrder(s string) (Order, error) { return ParseEnum("order", s, Orders()) }

// Valid reports whether o is a known order.
func (o Order) Valid() bool { return slices.Contains(Orders(), o) }
