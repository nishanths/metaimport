## metaimport

`go get github.com/nishanths/metaimport`

Specify a Git repository, and `metaimport` will generate a directory of
HTML files containing `<meta name="go-import">` tags for the Go packages
in the repository, for your vanity URL.

These tags are used by commands such as `go get` to determine how to fetch 
source code. See `go help importpath` for details.

## Example

```
$ metaimport -o html example.org/myrepo https://github.com/user/myrepo
```

Once the HTML files are generated, you can serve them at the root of your domain 
(`example.org` in this example) with something like:

```
$ cd html/example.org
$ python -m SimpleHTTPServer 443
$ go get example.org/myrepo # should now work
```

## Usage

See `metaimport -h`.

```
usage: metaimport [-branch branch] [-godoc] [-o dir] <import> <repo>

metaimport generates HTML files with <meta name="go-import"> tags as expected
by go get. 'repo' specifies the Git repository containing Go source code to
generate meta tags for. 'import' specifies the import path of the root of
the repository.

Flags
   -branch   Branch to use (default: repository's default branch).
   -godoc    Include <meta name="go-source"> tag as expected by godoc.org.
             Only partial support for repositories not hosted on github.com.
   -o        Directory to write generated HTML files (default: html).
             The directory is created with 0744 permissions if it doesn't exist.
```
