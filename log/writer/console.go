package writer

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// NewConsole creates a console output writer.
func NewConsole() zerolog.ConsoleWriter {
	return zerolog.ConsoleWriter{
		Out:         os.Stdout,
		TimeFormat:  time.DateTime,
		FormatLevel: formatLevel,
	}
}

// formatLevel formats a log level for display.
func formatLevel(i any) string {
	return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
}
