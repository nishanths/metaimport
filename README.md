```
usage: metaimport [-godoc] [-o dir] <import> <repo>

metaimport generates HTML files with <meta name="go-import"> tags as expected
by go get. 'repo' specifies the Git repository containing Go source code to
generate meta tags for. 'import' specifies the import path of the root of
the repository.

The program automatically handles generating HTML files for subpackages in the
repository.

Flags
   -branch   Branch to use (default: repository's default branch).
   -godoc    Include <meta name="go-source"> tag as expected by godoc.org.
             Only partial support for repositories not hosted on github.com.
   -o        Directory to write generated HTML (default: html).
             It creates the directory with 0744 permissions if it doesn't exist.

Examples
   metaimport example.org/bar https://github.com/user/bar
   metaimport example.org/exproj http://code.org/r/p/exproj
```
