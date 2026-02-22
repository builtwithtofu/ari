package world

import "errors"

var (
	ErrNotFound     = errors.New("world record not found")
	ErrInvalidInput = errors.New("invalid world input")
)
