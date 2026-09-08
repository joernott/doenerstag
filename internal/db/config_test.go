package db

import (
	"testing"

	"github.com/joernott/doenerstag/internal/config"
)

// The mapping is trivial and therefore easy to get wrong by transposing two
// string fields, which would be invisible until something failed to connect.
func TestOptionsFromConfig(t *testing.T) {
	opts := OptionsFromConfig(config.DatabaseConfig{
		Server:            "db.example.invalid",
		Port:              6543,
		Name:              "doenerstag",
		User:              "doener",
		Password:          "hunter2",
		SSLMode:           "require",
		MaxConnectionPool: 25,
	})

	want := Options{
		Host:           "db.example.invalid",
		Port:           6543,
		Database:       "doenerstag",
		User:           "doener",
		Password:       "hunter2",
		SSLMode:        "require",
		MaxConnections: 25,
	}

	if opts != want {
		t.Errorf("OptionsFromConfig() = %+v, want %+v", opts, want)
	}
	if err := opts.Validate(); err != nil {
		t.Errorf("mapped options do not validate: %v", err)
	}
}

// Whatever the configuration package defaults to must be usable as-is.
func TestConfigDefaultsProduceValidOptions(t *testing.T) {
	defaults := config.DatabaseConfig{}
	for _, setting := range config.SettingsInScope(config.ScopeGlobal) {
		switch setting.Flag {
		case "database-server":
			defaults.Server = setting.Default.(string)
		case "database-port":
			defaults.Port = setting.Default.(int)
		case "database-name":
			defaults.Name = setting.Default.(string)
		case "database-user":
			defaults.User = setting.Default.(string)
		case "database-sslmode":
			defaults.SSLMode = setting.Default.(string)
		case "max-connection-pool":
			defaults.MaxConnectionPool = setting.Default.(int)
		}
	}

	if err := OptionsFromConfig(defaults).Validate(); err != nil {
		t.Errorf("the documented defaults do not produce a usable connection: %v", err)
	}
}
