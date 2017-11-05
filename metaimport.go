package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	git "gopkg.in/src-d/go-git.v3"
)

const help = `usage: metaimport [-godoc] [-o dir] <import> <repo>

metaimport generates HTML files with <meta name="go-import"> tags as expected
by go get. 'repo' specifies the Git repository containing Go source code to
generate meta tags for. 'import' specifies the import path to use for the root 
of the repository.

The program automatically handles generating HTML files for subpackages in the
repository.

Flags
   -godoc   Include <meta name="go-source"> tag as expected by godoc.org
   -o       Directory to write generated HTML (default: metaimport)

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

	// godoc := flag.Bool("godoc", false, "Include go-source meta tag used by godoc.org")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) != 2 {
		usage()
	}

	// importPath := args[0]

	repo, err := git.NewRepository(args[1], nil)
	if err != nil {
		log.Fatalf("making repository: %s", err)
	}
	err = repo.PullDefault()
	if err != nil {
		log.Fatalf("pulling default branch: %s", err)
	}

	hash, err := repo.Head("")
	if err != nil {
		log.Fatalf("getting HEAD commit: %s", err)
	}
	tree, err := repo.Tree(hash)
	if err != nil {
		log.Fatalf("getting tree at HEAD commit %s: %s", hash, err)
	}

	s, err := packageDirs(tree)
	fmt.Println(s, err)
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
		fmt.Println(f.Name)
	}

	return dirs, nil
}
