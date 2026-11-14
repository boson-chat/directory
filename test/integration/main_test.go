package integration

import (
	"os"
	"testing"

	"github.com/boson-chat/directory/internal/logger"
)

func TestMain(m *testing.M) {
	logger.Logger(logger.WithLevel("error"))
	os.Exit(m.Run())
}
