package core

import "github.com/rs/zerolog"

type Log interface {
	Info() *zerolog.Event
	Debug() *zerolog.Event
	Warn() *zerolog.Event
	Error() *zerolog.Event
}
