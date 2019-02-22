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

var DefaultLocalManifestFile = `pman.xml`
var ManifestRepoWorkingDirectory = `.repo/manifest`
var ManifestRepoRef = sliceutil.OrString(os.Getenv(`PMAN_MANIFEST_REPO_BRANCH`), `master`)
var ManifestRepoFilename = sliceutil.OrString(os.Getenv(`PMAN_MANIFEST_REPO_FILENAME`), `default.xml`)

type ManifestBranch struct {
	Name     string `json:"name,omitempty"     xml:"name,attr,omitempty"`
	Color    string `json:"color,omitempty"    xml:"color,attr,omitempty"`
	Prefixes string `json:"prefixes,omitempty" xml:"prefixes,attr,omitempty"`
}

func (self ManifestBranch) MatchBranchName(branch string) bool {
	if branch == self.Name {
		return true
	} else {
		for _, prefix := range strings.Split(self.Prefixes, ` `) {
			if strings.HasPrefix(branch, strings.TrimSpace(prefix)) {
				return true
			}
		}
	}

	return false
}

type ManifestConfig struct {
	Branches []ManifestBranch `json:"branches,omitempty" xml:"branch,omitempty"`
}

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
	Default  *ManifestProject  `json:"default,omitempty"  xml:"default,omitempty"`
	Projects []ManifestProject `json:"projects,omitempty" xml:"project,omitempty"`
	Config   *ManifestConfig   `json:"config,omitempty"   xml:"config,omitempty"`
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
		if self.Default != nil {
			parent = self.Default
		} else {
			parent = &ManifestProject{}
		}
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

func (self *Manifest) ColorForBranch(branch string) string {
	if config := self.Config; config != nil {
		for _, bconfig := range config.Branches {
			if bconfig.MatchBranchName(branch) {
				if bconfig.Color != `` {
					return bconfig.Color
				}
			}
		}
	}

	return `reset`
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

func InitializeManifest(manifestUri string) error {
	// if we have a checked-out repo serving as our manifest, clone or pull it.
	if fileutil.DirExists(filepath.Join(ManifestRepoWorkingDirectory, `.git`)) {
		log.Infof("Pulling project manifest repository in %s", ManifestRepoWorkingDirectory)

		if err := GitPull(ManifestRepoWorkingDirectory, `master`); err != nil {
			return fmt.Errorf("failed to retrieve manifest: %v", err)
		}

	} else if strings.HasPrefix(manifestUri, `git://`) || strings.Contains(manifestUri, `git@`) {
		// if we're here, there isn't a manifest repo already and no local manifest file...

		// ...ensure the parent dir exists
		if !fileutil.Exists(filepath.Dir(ManifestRepoWorkingDirectory)) {
			if err := os.Mkdir(filepath.Dir(ManifestRepoWorkingDirectory), 0700); err != nil {
				return fmt.Errorf("failed to create project manifest parent: %v", err)
			}
		}

		// and clone the git URI
		if err := GitClone(manifestUri, ManifestRepoWorkingDirectory); err != nil {
			return fmt.Errorf("failed to retrieve manifest: %v", err)
		}
	} else if err := initLocalProjectManifest(manifestUri); err != nil {
		return err
	}

	if mf := LocateManifestFile(); mf != `` {
		log.Noticef("Project manifest file present at %v", mf)
		return nil
	} else {
		return fmt.Errorf("Unable to initialize: could not find project manifest file")
	}
}

func LocateManifestFile() string {
	for _, pmf := range []string{
		DefaultLocalManifestFile,
		filepath.Join(ManifestRepoWorkingDirectory, ManifestRepoFilename),
	} {
		if fileutil.IsNonemptyFile(pmf) {
			return pmf
		}
	}

	return ``
}

func initLocalProjectManifest(manifestUri string) error {
	log.Debugf("Copying manifest %v to %v", manifestUri, DefaultLocalManifestFile)
	return fileutil.CopyFile(manifestUri, DefaultLocalManifestFile)
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
