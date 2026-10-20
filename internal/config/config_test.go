package config_test

import (
	"io/fs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/config"
)

func env(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := vars[key]
		return value, ok
	}
}

func files(contents map[string]string) func(string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		content, ok := contents[name]
		if !ok {
			return nil, fs.ErrNotExist
		}

		return []byte(content), nil
	}
}

type input struct {
	flags config.Flags
	env   map[string]string
	files map[string]string
}

func resolve(in input) (config.Config, error) {
	return config.Resolve(in.flags, env(in.env), files(in.files))
}

var _ = Describe("Resolve", func() {
	const (
		home        = "/home/ada"
		userConfig  = "/home/ada/.config/coroner/config.toml"
		defaultData = "/home/ada/.local/share/coroner/coroner.db"
	)

	DescribeTable("DBPath",
		func(in input, want string) {
			cfg, err := resolve(in)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.DBPath).To(Equal(want))
		},
		Entry("--db beats CORONER_DB and the file",
			input{
				flags: config.Flags{DBPath: new("flag.db")},
				env:   map[string]string{"HOME": home, "CORONER_DB": "/env.db"},
				files: map[string]string{userConfig: `db = "/file.db"`},
			},
			"flag.db"),
		Entry("CORONER_DB beats the file",
			input{
				env:   map[string]string{"HOME": home, "CORONER_DB": "/env.db"},
				files: map[string]string{userConfig: `db = "/file.db"`},
			},
			"/env.db"),
		Entry("the file beats the default",
			input{
				env:   map[string]string{"HOME": home},
				files: map[string]string{userConfig: `db = "/file.db"`},
			},
			"/file.db"),
		Entry("a file named by CORONER_CONFIG beats the default",
			input{
				env:   map[string]string{"HOME": home, "CORONER_CONFIG": "/env.toml"},
				files: map[string]string{"/env.toml": `db = "/file.db"`},
			},
			"/file.db"),
		Entry("an empty CORONER_DB counts as unset",
			input{
				env:   map[string]string{"HOME": home, "CORONER_DB": ""},
				files: map[string]string{userConfig: `db = "/file.db"`},
			},
			"/file.db"),
		Entry("an empty CORONER_CONFIG counts as unset",
			input{
				env:   map[string]string{"HOME": home, "CORONER_CONFIG": ""},
				files: map[string]string{userConfig: `db = "/file.db"`},
			},
			"/file.db"),
		Entry("an absolute XDG_DATA_HOME holds the default",
			input{env: map[string]string{"HOME": home, "XDG_DATA_HOME": "/xdg/data"}},
			"/xdg/data/coroner/coroner.db"),
		Entry("an unset XDG_DATA_HOME falls back to HOME",
			input{env: map[string]string{"HOME": home}},
			defaultData),
		Entry("a relative XDG_DATA_HOME is ignored",
			input{env: map[string]string{"HOME": home, "XDG_DATA_HOME": "rel/data"}},
			defaultData),
		Entry("a macOS HOME still gets the XDG layout, not Library/Application Support",
			input{env: map[string]string{"HOME": "/Users/ada"}},
			"/Users/ada/.local/share/coroner/coroner.db"),
	)

	DescribeTable("ConfigPath",
		func(in input, want string) {
			cfg, err := resolve(in)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.ConfigPath).To(Equal(want))
		},
		Entry("--config beats CORONER_CONFIG",
			input{
				flags: config.Flags{ConfigPath: new("/flag.toml")},
				env:   map[string]string{"HOME": home, "CORONER_CONFIG": "/env.toml"},
				files: map[string]string{"/flag.toml": ""},
			},
			"/flag.toml"),
		Entry("CORONER_CONFIG beats the default",
			input{
				env:   map[string]string{"HOME": home, "CORONER_CONFIG": "/env.toml"},
				files: map[string]string{"/env.toml": ""},
			},
			"/env.toml"),
		Entry("an absolute XDG_CONFIG_HOME holds the default",
			input{env: map[string]string{"HOME": home, "XDG_CONFIG_HOME": "/xdg/config"}},
			"/xdg/config/coroner/config.toml"),
		Entry("a relative XDG_CONFIG_HOME is ignored",
			input{env: map[string]string{"HOME": home, "XDG_CONFIG_HOME": "rel/config"}},
			userConfig),
		Entry("a macOS HOME still gets ~/.config",
			input{env: map[string]string{"HOME": "/Users/ada"}},
			"/Users/ada/.config/coroner/config.toml"),
	)

	DescribeTable("file values",
		func(in input, want config.Config) {
			Expect(resolve(in)).To(Equal(want))
		},
		Entry("a missing file at the default path gives the defaults",
			input{env: map[string]string{"HOME": home}},
			config.Config{
				DBPath:          defaultData,
				ConfigPath:      userConfig,
				DefaultTimezone: "UTC",
			}),
		Entry("the file sets default_timezone and author",
			input{
				env: map[string]string{"HOME": home},
				files: map[string]string{
					userConfig: "default_timezone = \"America/New_York\"\nauthor = \"ada\"\n",
				},
			},
			config.Config{
				DBPath:          defaultData,
				ConfigPath:      userConfig,
				DefaultTimezone: "America/New_York",
				Author:          "ada",
			}),
	)

	DescribeTable("errors",
		func(in input, mentions []string) {
			_, err := resolve(in)
			Expect(err).To(HaveOccurred())
			for _, mention := range mentions {
				Expect(err.Error()).To(ContainSubstring(mention))
			}
		},
		Entry("an empty --db",
			input{flags: config.Flags{DBPath: new("")}, env: map[string]string{"HOME": home}},
			[]string{"--db"}),
		Entry("an empty --config",
			input{flags: config.Flags{ConfigPath: new("")}, env: map[string]string{"HOME": home}},
			[]string{"--config"}),
		Entry("a missing file named by --config",
			input{
				flags: config.Flags{ConfigPath: new("/missing.toml")},
				env:   map[string]string{"HOME": home},
			},
			[]string{"/missing.toml"}),
		Entry("a missing file named by CORONER_CONFIG",
			input{env: map[string]string{"HOME": home, "CORONER_CONFIG": "/missing.toml"}},
			[]string{"/missing.toml", "CORONER_CONFIG"}),
		Entry("an unknown key",
			input{
				env:   map[string]string{"HOME": home},
				files: map[string]string{userConfig: `default_timzone = "UTC"`},
			},
			[]string{"default_timzone", userConfig}),
		Entry("malformed TOML",
			input{
				env:   map[string]string{"HOME": home},
				files: map[string]string{userConfig: `author = `},
			},
			[]string{userConfig}),
		Entry("a relative db in the file",
			input{
				env:   map[string]string{"HOME": home},
				files: map[string]string{userConfig: `db = "~/coroner.db"`},
			},
			[]string{"db", userConfig}),
		Entry("no HOME and no XDG directories",
			input{},
			[]string{"HOME"}),
	)

	It("does not need HOME when --db and --config are both given", func() {
		cfg, err := resolve(input{
			flags: config.Flags{DBPath: new("/flag.db"), ConfigPath: new("/flag.toml")},
			files: map[string]string{"/flag.toml": ""},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).To(Equal(config.Config{
			DBPath:          "/flag.db",
			ConfigPath:      "/flag.toml",
			DefaultTimezone: "UTC",
		}))
	})

	It("reports a read failure at the default path instead of falling back to defaults", func() {
		readFile := func(string) ([]byte, error) {
			return nil, fs.ErrPermission
		}

		_, err := config.Resolve(config.Flags{}, env(map[string]string{"HOME": home}), readFile)

		Expect(err).To(MatchError(fs.ErrPermission))
		Expect(err.Error()).To(ContainSubstring(userConfig))
	})
})

var _ = Describe("BlobDir", func() {
	DescribeTable("places blobs",
		func(dbPath, want string) {
			Expect(config.BlobDir(dbPath)).To(Equal(want))
		},
		Entry("for the default database, in its data directory's blobs",
			"/home/ada/.local/share/coroner/coroner.db", "/home/ada/.local/share/coroner/blobs"),
		Entry("for any other database, beside it and named after it",
			"/srv/inc/other.db", "/srv/inc/other.db.blobs"),
	)

	It("gives two databases in one directory different blob directories", func() {
		Expect(config.BlobDir("/srv/inc/coroner.db")).NotTo(Equal(config.BlobDir("/srv/inc/other.db")))
	})
})
