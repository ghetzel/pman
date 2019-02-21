package main

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghetzel/go-stockutil/fileutil"
	"github.com/ghetzel/go-stockutil/log"
	"github.com/ghetzel/go-stockutil/maputil"
	"github.com/ghetzel/go-stockutil/sliceutil"
)

type ManifestRemote struct {
	Name            string `json:"name,omitempty"   xml:"name,attr,omitempty"`
	Fetch           string `json:"fetch,omitempty"  xml:"fetch,attr,omitempty"`
	SyncConcurrency int    `json:"sync-j,omitempty" xml:"sync-j,attr,omitempty"`
}

type ManifestProject struct {
	Name       string            `json:"name"               xml:"name,attr,omitempty"`
	Path       string            `json:"path,omitempty"     xml:"path,attr,omitempty"`
	Remote     string            `json:"remote,omitempty"   xml:"remote,attr,omitempty"`
	Fetch      string            `json:"fetch,omitempty"    xml:"fetch,attr,omitempty"`
	Projects   []ManifestProject `json:"project,omitempty"  xml:"project,omitempty"`
	Revision   string            `json:"revision,omitempty" xml:"revision,attr,omitempty"`
	GroupNames string            `json:"groups,omitempty"   xml:"groups,attr,omitempty"`
}

func (self ManifestProject) Clone(force bool) error {
	if fileutil.DirExists(self.Path) {
		if fileutil.DirExists(filepath.Join(self.Path, `.git`)) {
			_, err := self.Checkout(self.Revision, false)
			return err
		} else if force {
			if err := os.RemoveAll(self.Path); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("unmanaged directory already exists at %q", self.Path)
		}
	}

	return GitClone(self.Fetch, self.Path)
}

// Attempt to checkout and pull the given revision.  If checkout fails, fallback to
// checkout and pulling the project revision.
func (self ManifestProject) Checkout(revision string, useFallback bool) (string, error) {
	if err := GitPull(self.Path, revision); err == nil {
		return revision, err
	} else if useFallback && strings.HasPrefix(err.Error(), `checkout failed:`) {
		return self.Revision, GitPull(self.Path, self.Revision)
	} else {
		return ``, err
	}
}

// Checkout a branch, pull it, and create a new fork off of that branch.
func (self ManifestProject) Fork(revision string, branchFrom string) error {
	if branchFrom != `` {
		if _, err := self.Checkout(branchFrom, true); err != nil {
			return err
		}
	}

	return GitBranchTo(self.Path, revision)
}

func (self ManifestProject) SkipInclude() bool {
	return sliceutil.ContainsString(strings.Split(self.GroupNames, ` `), `notdefault`)
}

type Manifest struct {
	Remotes  []ManifestRemote  `json:"remotes,omitempty"  xml:"remote,omitempty"`
	Default  ManifestProject   `json:"default,omitempty"  xml:"default,omitempty"`
	Projects []ManifestProject `json:"projects,omitempty" xml:"project,omitempty"`
}

func (self *Manifest) GetRemote(name string) *ManifestRemote {
	for _, remote := range self.Remotes {
		if remote.Name == name {
			return &remote
		}
	}

	return nil
}

// Sync all projects in this manifest.
func (self *Manifest) Sync(force bool) error {
	var merr error

	for _, project := range self.GetProjects(nil, nil) {
		if err := project.Clone(force); err == nil {
			log.Infof("Synced %v", project.Name)
		} else {
			merr = log.AppendError(merr, fmt.Errorf("Error syncing %v: %v", project.Name, err))
		}
	}

	return merr
}

func (self *Manifest) Checkout(branch string, force bool) error {
	var merr error

	for _, project := range self.GetProjects(nil, nil) {
		if nowOnBranch, err := project.Checkout(branch, true); err == nil {
			log.Debugf("Project %s now on branch %s", project.Name, nowOnBranch)
		} else {
			merr = log.AppendError(merr, fmt.Errorf("Error checking out %s: %v", project.Name, err))
		}
	}

	return merr
}

func (self *Manifest) GetProjects(from []ManifestProject, parent *ManifestProject) []ManifestProject {
	projects := make([]ManifestProject, 0)

	var root []ManifestProject
	var remote ManifestRemote

	// first remote is the default one
	if len(self.Remotes) > 0 {
		remote = self.Remotes[0]
	} else {
		log.Errorf("No remotes specified")
		return nil
	}

	if len(from) == 0 {
		root = self.Projects
	} else {
		root = from
	}

	if parent == nil {
		parent = &self.Default
	}

	// expand parent envvars
	expandEnvProject(parent)

	for _, pdef := range root {
		var project ManifestProject

		// expand current project envvars
		expandEnvProject(&pdef)

		pmap := maputil.M(*parent)

		// get the specific remote for this project
		if pdef.Remote != `` {
			if r := self.GetRemote(pdef.Remote); r != nil {
				remote = *r
			} else {
				log.Warningf("Project %s: Remote %q does not exist", pdef.Name, pdef.Remote)
				continue
			}
		}

		// project names are path-joined arrays of [default.name] / [parent project.name ..] / project.name
		pmap.Set(`Name`, filepath.Join(pmap.String(`Name`), pdef.Name))

		// remotes are inherited from default and parent projects
		pmap.SetValueIfNonZero(`Remote`, pdef.Remote)
		pmap.SetValueIfNonZero(`Revision`, pdef.Revision)

		// populate fetch if it's not already
		pmap.SetIfZero(`Fetch`, remote.Fetch)

		// ...then join the current fetch path component (fallback to using name) to the fetch URL path
		// e.g:  remote fetch="http://a/b"; project fetch="/c/d/" -> "http://a/b/c/d/"
		pmap.Set(`Fetch`, UrlPathJoin(pmap.String(`Fetch`), sliceutil.OrString(pdef.Fetch, pdef.Name)))

		// do the same thing for paths
		pmap.Set(`Path`, UrlPathJoin(pmap.String(`Path`), sliceutil.OrString(pdef.Path, pdef.Name)))

		// make sure child projects are copied over
		pmap.Set(`Projects`, pdef.Projects)

		// populate group names
		groupNames := strings.Split(pmap.String(`GroupNames`), ` `)
		groupNames = sliceutil.CompactString(groupNames)
		groupNames = sliceutil.UniqueStrings(groupNames)
		pmap.Set(`GroupNames`, pdef.GroupNames)

		// get all these changes into the project struct we're about to append
		if err := maputil.StructFromMap(pmap.MapNative(), &project); err == nil {
			// depth-first recursion to handle subprojects
			if subprojects := project.Projects; len(subprojects) > 0 {
				projects = append(projects, self.GetProjects(subprojects, &project)...)
			}

			// don't add this project if it's in the "notdefault" group
			if !pdef.SkipInclude() {
				// now that the values are finalized, do some last minute processing

				// expand ~ in path
				project.Path = fileutil.MustExpandUser(project.Path)

				// append ".git" to fetch URLs if it's not there already
				if fetch, err := url.Parse(project.Fetch); err == nil {
					if !strings.HasSuffix(fetch.Path, `.git`) {
						fetch.Path += `.git`
					}

					project.Fetch = fetch.String()
				}

				projects = append(projects, project)
			}
		} else {
			log.Warningf("failed to populate project: %v", err)
		}
	}

	return projects
}

func LoadManifest(filename string) (*Manifest, error) {
	if file, err := os.Open(filename); err == nil {
		defer file.Close()

		var manifest Manifest

		if err := xml.NewDecoder(file).Decode(&manifest); err == nil {
			return &manifest, nil
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

func UrlPathJoin(uri string, path string) string {
	if f, err := url.Parse(uri); err == nil {
		f.Path = filepath.Join(f.Path, path)
		return f.String()
	} else {
		return filepath.Join(uri, path)
	}
}

func expandEnvProject(project *ManifestProject) {
	if project != nil {
		// resolve environment variables in all fields
		project.Name = os.ExpandEnv(project.Name)
		project.Path = os.ExpandEnv(project.Path)
		project.Fetch = os.ExpandEnv(project.Fetch)
		project.Revision = os.ExpandEnv(project.Revision)
		project.Remote = os.ExpandEnv(project.Remote)
		project.GroupNames = os.ExpandEnv(project.GroupNames)
	}
}
