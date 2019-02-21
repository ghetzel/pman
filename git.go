package main

import (
	"fmt"

	"github.com/ghetzel/argonaut"
	"github.com/ghetzel/go-stockutil/fileutil"
)

type git struct {
	Subcommand string        `argonaut:",positional"`
	Branch     string        `argonaut:"b,short"`
	Arguments  []interface{} `argonaut:",positional"`
}

func GitClone(repository string, destination ...string) error {
	args := []interface{}{
		repository,
	}

	if len(destination) > 0 {
		args = append(args, destination[0])
	}

	if cmd, err := argonaut.Command(&git{
		Subcommand: `clone`,
		Arguments:  args,
	}); err == nil {
		return cmd.Run()
	} else {
		return err
	}
}

func GitPull(workingDirectory string, ref string) error {
	if fileutil.DirExists(workingDirectory) {
		if checkout, err := argonaut.Command(&git{
			Subcommand: `checkout`,
			Arguments:  []interface{}{ref},
		}); err == nil {
			checkout.Dir = workingDirectory

			if err := checkout.Run(); err == nil {
				if ref != `` {
					if pull, err := argonaut.Command(&git{
						Subcommand: `pull`,
					}); err == nil {
						pull.Dir = workingDirectory
						return pull.Run()
					} else {
						return fmt.Errorf("pull failed: %v", err)
					}
				} else {
					return nil
				}
			} else {
				return fmt.Errorf("checkout failed: %v", err)
			}
		} else {
			return err
		}
	} else {
		return fmt.Errorf("no such directory %q", workingDirectory)
	}
}

func GitBranchTo(workingDirectory string, newBranchName string) error {
	if fileutil.DirExists(workingDirectory) {
		if branch, err := argonaut.Command(&git{
			Subcommand: `checkout`,
			Branch:     newBranchName,
		}); err == nil {
			branch.Dir = workingDirectory

			return branch.Run()
		} else {
			return err
		}
	} else {
		return fmt.Errorf("no such directory %q", workingDirectory)
	}
}
