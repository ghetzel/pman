package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/fileutil"
	"github.com/ghetzel/go-stockutil/log"
)

func main() {
	app := cli.NewApp()
	app.Name = `pman`
	app.Usage = `Like repo, but less opinionated.`
	app.Version = `0.0.1`

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   `log-level, L`,
			Usage:  `Level of log output verbosity`,
			Value:  `debug`,
			EnvVar: `LOGLEVEL`,
		},
	}

	app.Before = func(c *cli.Context) error {
		log.SetLevelString(c.String(`log-level`))
		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:      `init`,
			Usage:     `Initialize a project.`,
			ArgsUsage: `MANIFEST_URL`,
			Flags:     []cli.Flag{},
			Action: func(c *cli.Context) {
				overrideManifestFile := filepath.Join(`.repo`, `default.xml`)

				if !fileutil.IsNonemptyFile(overrideManifestFile) {
					if c.NArg() > 0 {
						if !fileutil.Exists(`.repo`) {
							if err := os.Mkdir(`.repo`, 0700); err != nil {
								log.Fatal(err)
							}
						}

						manifestUri := c.Args().First()
						mandir := filepath.Join(`.repo`, `manifest`)

						// clone or pull manifest repo
						if !fileutil.DirExists(filepath.Join(mandir, `.git`)) {
							if err := os.RemoveAll(mandir); err == nil {
								if err := GitClone(manifestUri, mandir); err != nil {
									log.Fatalf("failed to retrieve manifest: %v", err)
								}
							} else {
								log.Fatalf("failed to cleanup stale manifest directory: %v", err)
							}
						}
					} else {
						cli.ShowCommandHelp(c, `init`)
					}
				} else {
					log.Noticef("Override manifest file already present at %v", overrideManifestFile)
				}
			},
		}, {
			Name:  `sync`,
			Usage: `Synchronize all repositories in a given developer manifest.`,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  `force, f`,
					Usage: `Force overwriting existing working directories.`,
				},
			},
			Action: func(c *cli.Context) {
				if err := loadManifest(c).Sync(c.Bool(`force`)); err != nil {
					log.Fatal(err)
				}
			},
		}, {
			Name:      `checkout`,
			ArgsUsage: `BRANCH`,
			Usage:     `Checkout all repositories to a named branch, or the project-level branch if the named branch does not exist.`,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  `force, f`,
					Usage: `Force overwriting existing working directories.`,
				},
			},
			Action: func(c *cli.Context) {
				if c.NArg() > 0 {
					branch := c.Args().First()
					if err := loadManifest(c).Checkout(branch, c.Bool(`force`)); err != nil {
						log.Fatal(err)
					}
				} else {
					cli.ShowCommandHelp(c, `checkout`)
				}
			},
		}, {
			Name:  `dump-projects`,
			Usage: `Dump the evaluated project manifest.`,
			Action: func(c *cli.Context) {
				if out, err := json.MarshalIndent(loadManifest(c).GetProjects(nil, nil), ``, `  `); err == nil {
					fmt.Println(string(out))
				} else {
					log.Fatal(err)
				}
			},
		},
	}

	app.Run(os.Args)
}

func loadManifest(c *cli.Context) *Manifest {
	var manifestFile string

	if mf := filepath.Join(`.repo`, `default.xml`); fileutil.IsNonemptyFile(mf) {
		manifestFile = mf
	} else {
		mandir := filepath.Join(`.repo`, `manifest`)

		if err := GitPull(mandir, `master`); err != nil {
			log.Fatalf("failed to retrieve manifest: %v", err)
		}

		manifestFile = filepath.Join(mandir, `default.xml`)
	}

	if manifest, err := LoadManifest(manifestFile); err == nil {
		return manifest
	} else {
		log.Fatalf("failed to load manifest: %v", err)
	}

	panic("loadManifest: unreachable")
}
