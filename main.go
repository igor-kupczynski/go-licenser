// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/elastic/go-licenser/licensing"
)

const (
	defaultExt      = ".go"
	defaultPath     = "."
	defaultLicense  = "ASL2"
	defaultLicensor = "Elasticsearch B.V."
	defaultFormat   = "%s: is missing the license header\n"
)

const (
	exitDefault = iota
	exitSourceNeedsToBeRewritten
	exitFailedToStatTree
	exitFailedToStatFile
	exitFailedToWalkPath
	exitFailedToOpenWalkFile
	errFailedRewrittingFile
	errUnknownLicense
)

var usageText = `
Usage: go-licenser [flags] [path]

  go-licenser walks the specified path recursively and appends a license Header if the current
  header doesn't match the one found in the file.

Options:

`[1:]

var (
	dryRun             bool
	showVersion        bool
	extension          string
	args               []string
	license            string
	licensor           string
	exclude            sliceFlag
	defaultExludedDirs = []string{"vendor", ".git"}
)

type sliceFlag []string

func (f *sliceFlag) String() string {
	var s string
	for _, i := range *f {
		s += i + " "
	}
	return s
}

func (f *sliceFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func initFlags() {
	var licenseTypes []string
	for k := range licensing.Headers {
		licenseTypes = append(licenseTypes, k)
	}
	sort.Strings(licenseTypes)

	flag.Var(&exclude, "exclude", `path to exclude (can be specified multiple times).`)
	flag.BoolVar(&dryRun, "d", false, `skips rewriting files and returns exitcode 1 if any discrepancies are found.`)
	flag.BoolVar(&showVersion, "version", false, `prints out the binary version.`)
	flag.StringVar(&extension, "ext", defaultExt, "sets the file extension to scan for.")
	flag.StringVar(&license, "license", defaultLicense, fmt.Sprintf("sets the license type to check: %s", strings.Join(licenseTypes, ", ")))
	flag.StringVar(&licensor, "licensor", defaultLicensor, "sets the name of the licensor")
	flag.Usage = usageFlag
	flag.Parse()
	args = flag.Args()
}

func main() {
	initFlags()

	if showVersion {
		fmt.Printf("go-licenser %s (%s)\n", version, commit)
		return
	}

	err := run(args, license, licensor, exclude, extension, dryRun, os.Stdout)
	if err != nil && err.Error() != "<nil>" {
		fmt.Fprint(os.Stderr, err)
	}

	os.Exit(Code(err))
}

func run(args []string, license, licensor string, exclude []string, ext string, dry bool, out io.Writer) error {
	header, ok := licensing.Headers[license]
	if !ok {
		return &Error{err: fmt.Errorf("unknown license: %s", license), code: errUnknownLicense}
	}

	var headerBytes []byte
	for i, line := range header {
		if strings.Contains(line, "%s") {
			header[i] = fmt.Sprintf(line, licensor)
		}
		headerBytes = append(headerBytes, []byte(header[i])...)
		headerBytes = append(headerBytes, []byte("\n")...)
	}

	var path = defaultPath
	if len(args) > 0 {
		path = args[0]
	}

	if _, err := os.Stat(path); err != nil {
		return &Error{err: err, code: exitFailedToStatTree}
	}

	return walk(path, ext, license, headerBytes, exclude, dry, out)
}

func reportFile(out io.Writer, f string) {
	cwd, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	rel, err := filepath.Rel(cwd, f)
	if err != nil {
		rel = f
	}
	fmt.Fprintf(out, defaultFormat, rel)
}

func walk(p, ext, license string, headerBytes []byte, exclude []string, dry bool, out io.Writer) error {
	var err error
	filepath.WalkDir(p, func(path string, info fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			err = &Error{err: walkErr, code: exitFailedToWalkPath}
			return walkErr
		}

		var currentPath = cleanPathPrefixes(
			strings.TrimLeft(path, p),
			[]string{string(os.PathSeparator)},
		)

		var excludedDir = info.IsDir() && stringInSlice(info.Name(), defaultExludedDirs)
		if needsExclusion(currentPath, exclude) || excludedDir {
			return filepath.SkipDir
		}

		if e := addOrCheckLicense(path, ext, license, headerBytes, info, dry, out); e != nil {
			err = e
		}

		return nil
	})

	return err
}

func addOrCheckLicense(path, ext, license string, headerBytes []byte, info fs.DirEntry, dry bool, out io.Writer) error {
	if info.IsDir() || filepath.Ext(path) != ext {
		return nil
	}

	f, e := os.Open(path)
	if e != nil {
		return &Error{err: e, code: exitFailedToOpenWalkFile}
	}
	defer f.Close()

	if licensing.ContainsHeader(f, licensing.Headers[license]) {
		return nil
	}

	if dry {
		reportFile(out, path)
		return &Error{code: exitSourceNeedsToBeRewritten}
	}

	if err := licensing.RewriteFileWithHeader(path, headerBytes); err != nil {
		return &Error{err: err, code: errFailedRewrittingFile}
	}

	return nil
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func usageFlag() {
	fmt.Fprint(os.Stderr, usageText)
	flag.PrintDefaults()
}
