// Package config resolves where coroner's database and config file live, and the user's
// defaults, with the precedence flags, then CORONER_* environment, then the config file, then
// defaults.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Flags holds the global flag values. A nil field means the flag was not given, so an
// explicitly empty value can be rejected.
type Flags struct {
	DBPath, ConfigPath *string
}

// Config is the resolved configuration.
type Config struct {
	DBPath, ConfigPath string
	// DefaultTimezone is "local" or an IANA time zone name.
	DefaultTimezone string
	Author          string
}

type fileConfig struct {
	DB              string `toml:"db"`
	DefaultTimezone string `toml:"default_timezone"`
	Author          string `toml:"author"`
}

type setting struct {
	flag      *string
	flagName  string
	envKey    string
	fileValue string
}

// Resolve works out the configuration from the flags, the environment and the config file,
// reading them only through lookupEnv and readFile.
func Resolve(
	flags Flags, lookupEnv func(string) (string, bool), readFile func(string) ([]byte, error),
) (Config, error) {
	configPath, explicit, err := pick(setting{
		flag: flags.ConfigPath, flagName: "--config", envKey: "CORONER_CONFIG",
	}, lookupEnv)
	if err != nil {
		return Config{}, err
	}

	if configPath == "" {
		configPath, err = defaultPath(lookupEnv, "XDG_CONFIG_HOME", ".config", "config.toml")
		if err != nil {
			return Config{}, err
		}
	}

	file, err := load(configPath, explicit, readFile)
	if err != nil {
		return Config{}, err
	}

	dbPath, _, err := pick(setting{
		flag: flags.DBPath, flagName: "--db", envKey: "CORONER_DB", fileValue: file.DB,
	}, lookupEnv)
	if err != nil {
		return Config{}, err
	}

	if dbPath == "" {
		dbPath, err = defaultPath(lookupEnv, "XDG_DATA_HOME", ".local/share", "coroner.db")
		if err != nil {
			return Config{}, err
		}
	}

	timezone := file.DefaultTimezone
	if timezone == "" {
		timezone = "local"
	}

	return Config{
		DBPath:          dbPath,
		ConfigPath:      configPath,
		DefaultTimezone: timezone,
		Author:          file.Author,
	}, nil
}

// pick returns the first of the flag, the environment variable and the file value that is
// set, reporting whether it came from the flag or the environment. An empty variable counts
// as unset, as XDG_* do; "" means none is set.
func pick(s setting, lookupEnv func(string) (string, bool)) (string, bool, error) {
	if s.flag != nil {
		if *s.flag == "" {
			return "", false, fmt.Errorf("%s must not be empty", s.flagName)
		}

		return *s.flag, true, nil
	}

	value, _ := lookupEnv(s.envKey)
	if value != "" {
		return value, true, nil
	}

	return s.fileValue, false, nil
}

// defaultPath returns name inside the coroner directory under the XDG base directory named
// by xdgKey, or under HOME's homeSubdir when that variable is unset or not absolute.
func defaultPath(lookupEnv func(string) (string, bool), xdgKey, homeSubdir, name string) (string, error) {
	base, _ := lookupEnv(xdgKey)
	if !filepath.IsAbs(base) {
		home, _ := lookupEnv("HOME")
		if home == "" {
			return "", fmt.Errorf("HOME is not set: set HOME or %s", xdgKey)
		}

		base = filepath.Join(home, homeSubdir)
	}

	return filepath.Join(base, "coroner", name), nil
}

// load reads and decodes the config file at path. A missing file is an error only when the
// path was given explicitly.
func load(path string, explicit bool, readFile func(string) ([]byte, error)) (fileConfig, error) {
	var file fileConfig

	data, err := readFile(path)
	if errors.Is(err, fs.ErrNotExist) && !explicit {
		return file, nil
	}

	if err != nil {
		return file, fmt.Errorf("read config: %w", err)
	}

	meta, err := toml.Decode(string(data), &file)
	if err != nil {
		return file, fmt.Errorf("%s: %w", path, err)
	}

	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = fmt.Sprintf("%q", key.String())
		}

		return file, fmt.Errorf("%s: unknown config key %s", path, strings.Join(keys, ", "))
	}

	// A relative path would resolve against whichever directory coroner runs in.
	if file.DB != "" && !filepath.IsAbs(file.DB) {
		return file, fmt.Errorf("%s: db must be an absolute path, got %q", path, file.DB)
	}

	return file, nil
}
