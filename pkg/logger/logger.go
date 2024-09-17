package logger

// internal/internal/pkg/logger/logger.go
import (
	"os"

	"github.com/rs/zerolog"
)

func NewLogger(level string) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		logLevel = zerolog.InfoLevel
	}
	return zerolog.New(os.Stdout).With().Timestamp().Logger().Level(logLevel)
}
