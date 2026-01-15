package writer

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Console 创建控制台输出 writer
func Console() zerolog.ConsoleWriter {
	output := zerolog.ConsoleWriter{
		Out:         os.Stdout,
		TimeFormat:  time.DateTime,
		FormatLevel: formatLevel,
	}
	return output
}

// formatLevel 格式化日志级别显示
func formatLevel(i any) string {
	return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
}
