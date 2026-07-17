package log

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

// ConfigureZerolog configures process-wide Zerolog time and stack formatting.
// Call it during application startup, before concurrent logging begins.
func ConfigureZerolog() {
	zerolog.TimeFieldFormat = time.DateTime
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
}
