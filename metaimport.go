// Command metaimport generates HTML files containing <meta name="go-import">
// tags for remote Git repositories.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	git "gopkg.in/src-d/go-git.v3"
	gitcore "gopkg.in/src-d/go-git.v3/core"
)

const help = `usage: metaimport [-branch branch] [-godoc] [-o dir] [-redirect] <import-prefix> <repo>

metaimport generates HTML files with <meta name="go-import"> tags as expected
by go get. 'repo' specifies the Git repository containing Go source code to
generate meta tags for. 'import-prefix' is the import path corresponding to
the repository root.

Flags
   -branch    Branch to use (default: remote's default branch).
   -godoc     Include <meta name="go-source"> tag as expected by godoc.org (default: false).
              Only partial support for repositories not hosted on github.com.
   -o         Output directory for generated HTML files (default: html).
              The directory is created with 0755 permissions if it doesn't exist.
   -redirect  Redirect to godoc.org documentation when visited in a browser (default: true).

Examples
   metaimport example.org/myrepo https://github.com/user/myrepo
   metaimport example.org/exproj http://code.org/r/p/exproj
`

func usage() {
	fmt.Fprintf(os.Stderr, help)
	os.Exit(2)
}

const (
	permDir  = 0755
	permFile = 0644
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("metaimport: ")

	godoc := flag.Bool("godoc", false, "")
	branch := flag.String("branch", "", "")
	outputDir := flag.String("o", "", "")
	godocRedirect := flag.Bool("redirect", true, "")

	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) != 2 {
		usage()
	}

	baseImportPrefix := args[0]
	repoURL := args[1]
	htmlTmpl := template.Must(template.New("").Parse(tmpl))
	useDefaultBranch := *branch == ""

	repo, err := git.NewRepository(repoURL, nil)
	if err != nil {
		log.Fatalf("making repository: %s", err)
	}

	// Pull branch.
	if useDefaultBranch {
		err = repo.PullDefault()
	} else {
		err = repo.Pull(git.DefaultRemoteName, fmt.Sprintf("refs/heads/%s", *branch))
	}
	if err != nil {
		log.Fatalf("pulling branch: %s", err)
	}

	// Get the tree for the HEAD of the branch.
	var head gitcore.Hash
	if useDefaultBranch {
		head, err = repo.Head(git.DefaultRemoteName)
	} else {
		head, err = repo.Remotes[git.DefaultRemoteName].Ref(fmt.Sprintf("refs/heads/%s", *branch))
	}
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
		godocSpec = determineGodocSpec(repoURL, *branch, useDefaultBranch, repo)
	}

	type File struct {
		path     string
		contents bytes.Buffer
	}
	var files []File

	for d := range dirs {
		if d == "." {
			d = ""
		}
		forwardSlashed := filepath.ToSlash(d)
		fullImportPrefix := path.Join(baseImportPrefix, forwardSlashed)
		file := File{path: fullImportPrefix}

		args := TemplateArgs{
			// See https://npf.io/2016/10/vanity-imports-with-hugo/ and Issue#1
			// on GitHub, for why this shouldn't be fullImportPrefix.
			GoImport: GoImport{
				ImportPrefix: baseImportPrefix,
				VCS:          "git",
				RepoRoot:     repoURL,
			},
			GodocURL:      fmt.Sprintf("https://godoc.org/%s", fullImportPrefix),
			GodocRedirect: *godocRedirect,
		}
		if *godoc {
			args.GoSource = &GoSource{
				Prefix:    baseImportPrefix,
				Home:      godocSpec.home(),
				Directory: godocSpec.directory(),
				File:      godocSpec.file(),
			}
		}

		if err := htmlTmpl.Execute(&file.contents, args); err != nil {
			log.Fatalf("executing template for path %s: %s", file.path, err)
		}
		files = append(files, file)
	}

	// Make the output directory.
	if *outputDir == "" {
		*outputDir = "html"
	}
	if err := os.MkdirAll(*outputDir, permDir); err != nil {
		log.Fatalf("making directory %s: %s", *outputDir, err)
	}

	// Write output files.
	for _, file := range files {
		// This would fail if the repository had a structure like:
		//   a/
		//     a.go
		//     index.html/
		//       b.go
		// because we would need to have both 'a/index.html' (for the package at a)
		// and 'a/index.html/index.html' (for package at a/index.html).
		dir := filepath.Join(*outputDir, filepath.FromSlash(file.path))
		if err := os.MkdirAll(dir, permDir); err != nil {
			log.Fatalf("making directory %s: %s", dir, err)
		}
		f := filepath.Join(dir, "index.html")
		if err := ioutil.WriteFile(f, file.contents.Bytes(), permFile); err != nil {
			log.Fatalf("writing file %s: %s", f, err)
		}
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
// Bitbucket's formats as of 2017-11-05 has the hash in the URL, but the hash
// can be substituted with HEAD to use the HEAD of the default branch. So
// we can use the directory and file for godoc's tag only if the default branch was pulled.
//   directory: https://bitbucket.org/multicores/hw3/src/HEAD/q5/queue
//   file and line: https://bitbucket.org/multicores/hw3/src/HEAD/q5/queue/LockQueue.java?fileviewer=file-view-default#LockQueue.java-11

func shortBranch(long string) string {
	return strings.TrimPrefix(long, "refs/heads/")
}

func determineGodocSpec(repoURL, requestedBranch string, usedDefaultBranch bool, repo *git.Repository) GodocSpec {
	if u, err := url.Parse(repoURL); err == nil {
		switch u.Host {
		case "github.com":
			b := requestedBranch
			if usedDefaultBranch {
				b = shortBranch(repo.Remotes[git.DefaultRemoteName].DefaultBranch())
			}
			return GitHub{repoURL, b}
		case "bitbucket.org":
			if usedDefaultBranch || shortBranch(repo.Remotes[git.DefaultRemoteName].DefaultBranch()) == requestedBranch {
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

const tmpl = `<!DOCTYPE html>
<html>
	<head>
		<meta charset="utf-8">
		{{ with .GoImport }}<meta name="go-import" content="{{ .ImportPrefix }} {{ .VCS }} {{ .RepoRoot }}">{{ end }}
		{{ with .GoSource }}<meta name="go-source" content="{{ .Prefix }} {{ .Home }} {{ .Directory }} {{ .File }}">{{ end }}
		{{ if .GodocRedirect }}<meta http-equiv="refresh" content="0; url='{{ .GodocURL }}'">{{ end }}
	</head>
	<body>
		{{ if .GodocRedirect -}}
		Redirecting to <a href="{{ .GodocURL }}">{{ .GodocURL }}</a>
		{{- else -}}
		Repository: <a href="{{ .GoImport.RepoRoot }}">{{ .GoImport.RepoRoot }}</a>
		<br>
		Godoc: <a href="{{ .GodocURL }}">{{ .GodocURL }}</a>
		{{- end }}
	</body>
</html>
`

type TemplateArgs struct {
	GoImport      GoImport
	GoSource      *GoSource
	GodocRedirect bool
	GodocURL      string
}

type GoImport struct {
	ImportPrefix, VCS, RepoRoot string
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
		// 'go help packages' says:
		//   Directory and file names that begin with "." or "_" are ignored
		//   by the go tool, as are directories named "testdata".
		d := filepath.Dir(f.Name)
		if filepath.Base(d) == "testdata" {
			continue
		}
		if strings.HasPrefix(f.Name, ".") || strings.HasPrefix(f.Name, "_") || !strings.HasSuffix(f.Name, ".go") {
			// if it's not a go file we can't add the file's directory
			// to dirs, so move on.
			continue
		}
		if _, ok := dirs[d]; ok {
			// already accounted for
			continue
		}
		dirs[d] = struct{}{}
	}

	return dirs, nil
}
