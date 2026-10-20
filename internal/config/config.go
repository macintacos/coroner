// Package config resolves where coroner's database and config file live, and the user's
// defaults. The config file path is taken from --config, then CORONER_CONFIG, then the XDG
// config directory. The database path is taken from --db, then CORONER_DB, then the config
// file's db, then the XDG data directory.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Renaming it moves every default user's blob directory, stranding their blobs.
const defaultDBName = "coroner.db"

// Flags holds the global flag values. A nil field means the flag was not given, so an
// explicitly empty value can be rejected.
type Flags struct {
	DBPath, ConfigPath *string
}

// Config is Resolve's result. ConfigPath is the path in effect whether or not a file exists
// there; Author is empty when unset.
type Config struct {
	DBPath, ConfigPath string
	// DefaultTimezone is the file's default_timezone, unvalidated, or "UTC" when unset.
	DefaultTimezone string
	Author          string
}

type fileConfig struct {
	DB              string `toml:"db"`
	DefaultTimezone string `toml:"default_timezone"`
	Author          string `toml:"author"`
}

// Resolve works out the configuration from the flags, the environment and the config file,
// reading them only through lookupEnv and readFile. An empty CORONER_* or XDG_* variable
// counts as unset.
func Resolve(
	flags Flags, lookupEnv func(string) (string, bool), readFile func(string) ([]byte, error),
) (Config, error) {
	err := rejectEmpty(flags)
	if err != nil {
		return Config{}, err
	}

	env := func(key string) string {
		value, _ := lookupEnv(key)
		return value
	}

	configPath, file, err := loadConfigFile(flags.ConfigPath, env, readFile)
	if err != nil {
		return Config{}, err
	}

	dbPath := cmp.Or(deref(flags.DBPath), env("CORONER_DB"), file.DB)
	if dbPath == "" {
		dataHome, err := xdgHome(env, "XDG_DATA_HOME", ".local/share")
		if err != nil {
			return Config{}, err
		}

		dbPath = filepath.Join(dataHome, "coroner", defaultDBName)
	}

	return Config{
		DBPath:          dbPath,
		ConfigPath:      configPath,
		DefaultTimezone: cmp.Or(file.DefaultTimezone, "UTC"),
		Author:          file.Author,
	}, nil
}

// BlobDir returns the directory the database at dbPath keeps attachment blobs in: blobs
// beside a database named coroner.db, as the default is, and <dbPath>.blobs for any other.
// No two database paths share one, so collecting one database's garbage never deletes
// another's blobs. It reads only the path's spelling: a symlink or a differently cased
// name gives the same database another blob directory.
func BlobDir(dbPath string) string {
	if filepath.Base(dbPath) == defaultDBName {
		return filepath.Join(filepath.Dir(dbPath), "blobs")
	}

	return dbPath + ".blobs"
}

func rejectEmpty(flags Flags) error {
	if flags.ConfigPath != nil && *flags.ConfigPath == "" {
		return errors.New("--config must not be empty")
	}

	if flags.DBPath != nil && *flags.DBPath == "" {
		return errors.New("--db must not be empty")
	}

	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// loadConfigFile returns the config file's path and decoded contents. A missing file is an
// error only when --config or CORONER_CONFIG named it.
func loadConfigFile(
	configFlag *string, env func(string) string, readFile func(string) ([]byte, error),
) (string, fileConfig, error) {
	path := cmp.Or(deref(configFlag), env("CORONER_CONFIG"))
	named := path != ""
	if !named {
		configHome, err := xdgHome(env, "XDG_CONFIG_HOME", ".config")
		if err != nil {
			return "", fileConfig{}, err
		}

		path = filepath.Join(configHome, "coroner", "config.toml")
	}

	data, err := readFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !named:
		return path, fileConfig{}, nil
	case errors.Is(err, fs.ErrNotExist) && configFlag == nil:
		return "", fileConfig{}, fmt.Errorf("config file %s does not exist (set by CORONER_CONFIG)", path)
	case errors.Is(err, fs.ErrNotExist):
		return "", fileConfig{}, fmt.Errorf("config file %s does not exist", path)
	case err != nil:
		return "", fileConfig{}, fmt.Errorf("read config file %s: %w", path, err)
	}

	file, err := decode(path, data)
	if err != nil {
		return "", fileConfig{}, err
	}

	return path, file, nil
}

// xdgHome returns the XDG base directory named by xdgKey, or HOME's homeSubdir when that
// variable is unset or not absolute.
func xdgHome(env func(string) string, xdgKey, homeSubdir string) (string, error) {
	base := env(xdgKey)
	if filepath.IsAbs(base) {
		return base, nil
	}

	home := env("HOME")
	if home == "" {
		return "", fmt.Errorf("HOME is not set and %s is not an absolute path", xdgKey)
	}

	return filepath.Join(home, homeSubdir), nil
}

// decode parses the config file at path, rejecting unknown keys and a relative db.
func decode(path string, data []byte) (fileConfig, error) {
	var file fileConfig

	meta, err := toml.Decode(string(data), &file)
	if err != nil {
		return fileConfig{}, fmt.Errorf("config file %s: %w", path, err)
	}

	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}

		return fileConfig{}, fmt.Errorf("config file %s: unknown config key %s", path, strings.Join(keys, ", "))
	}

	// A relative path would resolve against whichever directory coroner runs in.
	if file.DB != "" && !filepath.IsAbs(file.DB) {
		return fileConfig{}, fmt.Errorf("config file %s: db must be an absolute path, got %q", path, file.DB)
	}

	return file, nil
}
