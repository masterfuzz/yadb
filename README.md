# yadb

Like `yq` except more recursive. 

Find matches, get values, and edit a directory tree of YAMLs. 

`yadb` treats a directory of YAMLs like one giant YAML. So a file at `foo/bar/baz.yaml` becomes `foo.bar.baz`.

That means you can do

```
$ yadb get foo.bar.baz.config.port
9090
```

It also supports wildcards so you can search multiple files at a time

## Usage

```
yadb [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  find        List files that have a field (optionally with a specific value)
  get         Print the value of a field
  help        Help about any command
  keys        List the keys available directly under a path
  set         Set the value of fields in one or more files
  unset       Delete fields from one or more files

Flags:
  -e, --ext strings   YAML file extensions (default .yaml,.yml)
  -h, --help          help for yadb
  -C, --root string   root directory to scan (default ".")
```

Fields may use wildcards to address multiple files:

- `*` matches exactly one path segment
- `**` matches zero or more path segments

### Examples

```sh
yadb get   foo.bar.baz.config.port       # query one value
yadb get   '**.name'                     # every top-level name field
yadb find  '**.replicas'                 # files that set replicas
yadb find  'services.*.image=nginx'      # files where image == nginx
yadb set   foo.bar.baz.config.port=9090  # set one value
yadb set   '**.enabled=true'             # set across many files
yadb unset 'foo.featureFlat.enabled'     # delete keys
yadb keys                                 # top-level namespace segments
yadb keys  foo.bar.baz.config             # keys available under a path
```

Use `yadb [command] --help` for more information about a command.

## Building

Requires Go 1.25+.

```sh
make build     # build the binary to bin/yadb
make run       # run from source
make test      # run the test suite
make install   # install with go install
```
