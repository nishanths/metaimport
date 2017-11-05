package main

import (
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"

	git "gopkg.in/src-d/go-git.v3"
)

const help = `usage: metaimport [-godoc] [-o dir] <import> <repo>

metaimport generates HTML files with <meta name="go-import"> tags as expected
by go get. 'repo' specifies the Git repository containing Go source code to
generate meta tags for. 'import' specifies the import path of the root of
the repository.

The program automatically handles generating HTML files for subpackages in the
repository.

Flags
   -branch   Branch to use (default: repository's default branch)
   -godoc    Include <meta name="go-source"> tag as expected by godoc.org.
             Only partial support for repositories not hosted on github.com.
   -o        Directory to write generated HTML (default: metaimport)

Examples
   metaimport example.org/bar https://github.com/user/bar
   metaimport example.org/exproj http://code.org/r/p/exproj
`

func usage() {
	fmt.Fprintf(os.Stderr, help)
	os.Exit(2)
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("metaimport: ")

	godoc := flag.Bool("godoc", false, "")
	branch := flag.String("branch", "", "")
	ouputDir := flag.String("o", "metaimport", "")

	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) != 2 {
		usage()
	}
	baseImportPrefix := args[0]
	repoURL := args[1]

	htmlTmpl := template.Must(template.New("").Parse(tmpl))

	repo, err := git.NewRepository(repoURL, nil)
	if err != nil {
		log.Fatalf("making repository: %s", err)
	}

	// Pull branch.
	if *branch == "" {
		err = repo.PullDefault()
	} else {
		err = repo.Pull("origin", fmt.Sprintf("refs/heads/%s", *branch))
	}
	if err != nil {
		log.Fatalf("pulling branch: %s", err)
	}

	// Get the tree for the HEAD of the branch.
	head, err := repo.Head("origin") // why doesn't local head work?
	if err != nil {
		log.Fatalf("getting HEAD: %s", err)
	}
	headCommit, err := repo.Commit(head)
	if err != nil {
		log.Fatalf("getting HEAD commit: %s", err)
	}
	tree := headCommit.Tree()

	// Determine the Go package directories.
	dirs, err := packageDirs(tree)
	if err != nil {
		log.Fatalf("determining go package directories: %s", err)
	}

	var godocSpec GodocSpec // can be nil
	if *godoc {
		godocSpec = determineGodocSpec(repoURL, *branch, repo)
	}

	_ = *ouputDir

	for d := range dirs {
		if d == "." {
			d = ""
		}
		normalized := filepath.ToSlash(d)

		args := TemplateArgs{
			ImportPrefix: baseImportPrefix, // This can always be the base. See https://npf.io/2016/10/vanity-imports-with-hugo/.
			VCS:          "git",
			RepoRoot:     repoURL,
		}
		if godocSpec != nil {
			args.GoSource = &GoSource{
				Prefix:    baseImportPrefix,
				Home:      godocSpec.home(),
				Directory: godocSpec.directory(),
				File:      godocSpec.file(),
			}
		}

		fullImportPrefix := path.Join(baseImportPrefix, normalized)
		println(fullImportPrefix)
		htmlTmpl.Execute(os.Stdout, args)
	}
}

// Notes
// -----
//
// For details on the meta tags, see
//   go help importpath
//   https://github.com/golang/gddo/wiki/Source-Code-Links
//
// GitHub's formats for go-source are straightforward.
//   directory: https://github.com/go-yaml/yaml/tree/some/directory
//   file and line: https://github.com/go-yaml/yaml/blob/some/directory/somefile#L42
//
// Bitbucket's formats as of 2017-11-05 use the hash in the URL, but the hash
// can be substituted with HEAD to use the HEAD of the default branch. So
// we can directory and file for go-source only if the default branch was pulled.
//   directory: https://bitbucket.org/multicores/hw3/src/HEAD/q5/queue
//   file and line: https://bitbucket.org/multicores/hw3/src/HEAD/q5/queue/LockQueue.java?fileviewer=file-view-default#LockQueue.java-11

func determineGodocSpec(repoURL, requestedBranch string, repo *git.Repository) GodocSpec {
	if u, err := url.Parse(repoURL); err == nil {
		switch u.Host {
		case "github.com":
			return GitHub{repoURL, requestedBranch}
		case "bitbucket.org":
			if repo.Remotes["origin"].DefaultBranch() == requestedBranch {
				return BitBucket{repoURL}
			}
		}
	}
	return Default{repoURL}
}

type GodocSpec interface {
	home() string
	directory() string
	file() string
}

type GitHub struct {
	repoURL string
	branch  string
}

func (g GitHub) home() string      { return "_" }
func (g GitHub) directory() string { return fmt.Sprintf("%s/tree/%s{/dir}", g.repoURL, g.branch) }
func (g GitHub) file() string {
	return fmt.Sprintf("%s/tree/%s{/dir}/{file}#L{line}", g.repoURL, g.branch)
}

type BitBucket struct {
	repoURL string
}

func (b BitBucket) home() string      { return "_" }
func (b BitBucket) directory() string { return fmt.Sprintf("%s/src/HEAD{/dir}", b.repoURL) }
func (b BitBucket) file() string {
	return fmt.Sprintf("%s/src/HEAD{/dir}/{file}?fileviewer=file-view-default#{file}-{line}", b.repoURL)
}

type Default struct {
	repoURL string
}

func (d Default) home() string      { return d.repoURL }
func (d Default) directory() string { return d.repoURL }
func (d Default) file() string      { return d.repoURL }

const tmpl = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="utf-8">
		<meta name="go-import" content="{{ .ImportPrefix }} {{ .VCS }} {{ .RepoRoot }}">
		{{- with .GoSource }}<meta name="go-source" content="{{ .Prefix }} {{ .Home }} {{ .Directory }} {{ .File }}">{{ end }}
		<style>
			html { font-family: monospace; }
		</style>
	</head>
	<body>
		{{ .RepoRoot }}
	</body>
</html>
`

type TemplateArgs struct {
	ImportPrefix, VCS, RepoRoot string
	GoSource                    *GoSource
}

type GoSource struct {
	Prefix    string
	Home      string
	Directory string
	File      string
}

func packageDirs(tree *git.Tree) (map[string]struct{}, error) {
	iter := tree.Files()
	defer iter.Close()
	dirs := make(map[string]struct{})

	for {
		f, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("getting next file in tree: %s", err)
		}
		if filepath.Ext(f.Name) != ".go" {
			// if it's not a go file we can't add the file's directory
			// to dirs, so move on.
			continue
		}
		d := filepath.Dir(f.Name)
		if _, ok := dirs[d]; ok {
			// already accounted for
			continue
		}
		dirs[d] = struct{}{}
	}

	return dirs, nil
}
