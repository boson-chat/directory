package commands

import (
	"github.com/boson-chat/directory/config"
	"github.com/boson-chat/directory/internal/logger"
)

func loadConfig() (config.Config, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return cfg, err
	}
	logger.Logger(
		logger.WithServerName(cfg.AppConfig.APPName),
		logger.WithVersion(cfg.AppConfig.Version),
		logger.WithEnvironment(cfg.AppConfig.Env),
		logger.WithLevel(cfg.AppConfig.LogLevel),
	)
	return cfg, nil
}
