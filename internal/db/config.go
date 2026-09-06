package db

import "github.com/joernott/doenerstag/internal/config"

// OptionsFromConfig maps the resolved configuration onto connection options.
//
// The two structs are deliberately separate: this package should be usable
// without dragging in cobra and viper, and the mapping is one obvious function
// rather than an import cycle waiting to happen.
func OptionsFromConfig(cfg config.DatabaseConfig) Options {
	return Options{
		Host:           cfg.Server,
		Port:           cfg.Port,
		Database:       cfg.Name,
		User:           cfg.User,
		Password:       cfg.Password,
		SSLMode:        cfg.SSLMode,
		MaxConnections: cfg.MaxConnectionPool,
	}
}
