package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/ghetzel/cli"
	"github.com/ghetzel/go-stockutil/fileutil"
	"github.com/ghetzel/go-stockutil/log"
	"github.com/ghetzel/go-stockutil/typeutil"
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
			Value:  `info`,
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
				if err := InitializeManifest(c.Args().First()); err != nil {
					log.Fatal(err)
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
			Name:  `status`,
			Usage: `Get the current status of each repository in the current project.`,
			Action: func(c *cli.Context) {
				var statuses []map[string]interface{}

				manifest := loadManifest(c)

				for _, project := range manifest.GetProjects(nil, nil) {
					status := make(map[string]interface{})

					status[`project`] = project.Name

					if ref, err := GitCurrentBranch(project.Path); err == nil {
						status[`ref`] = ref
					} else {
						status[`ref`] = err
					}

					statuses = append(statuses, status)
				}

				switch f := c.String(`format`); f {
				case `json`:
					if out, err := json.MarshalIndent(statuses, ``, `  `); err == nil {
						fmt.Println(string(out))
					} else {
						log.Fatal(err)
					}
				default:
					tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)

					for _, status := range statuses {
						ref := status[`ref`]

						if err, ok := ref.(error); ok {
							log.CFPrintf(tw, "%s\t${white:red}%v${reset}\n", status[`project`], err)
						} else {
							log.CFPrintf(tw, "%s\t${"+manifest.ColorForBranch(typeutil.String(ref))+"+h}%v${reset}\n", status[`project`], ref)
						}
					}

					tw.Flush()
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

	if mf := filepath.Join(`pman.xml`); fileutil.IsNonemptyFile(mf) {
		manifestFile = mf
	} else {
		mandir := filepath.Join(`.repo`, `manifest`)

		if err := GitPull(mandir, `master`); err != nil {
			log.Fatalf("failed to retrieve manifest: %v", err)
		}

		manifestFile = filepath.Join(mandir, `default.xml`)
	}

	if manifest, err := LoadManifest(manifestFile); err == nil {
		log.Debugf("Loaded project manifest from %s", manifestFile)
		return manifest
	} else {
		log.Fatalf("failed to load manifest: %v", err)
	}

	panic("loadManifest: unreachable")
}
