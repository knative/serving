// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// The license_serializer program normalizes and serializes the known
// licenseclassifier licenses into a compressed archive. The hash values for
// the licenses are calculated and added to the archive. These can then be used
// to determine where in unknown text is a good offset to run through the
// Levenshtein Distance algorithm.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/licenseclassifier"
	"github.com/google/licenseclassifier/serializer"
)

var (
	forbiddenOnly = flag.Bool("forbidden", false, "serialize only forbidden licenses")
	outputDir     = flag.String("output", "", "output directory")
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [OPTIONS]

Calculate the hash values for files and serialize them into a database.
See go/license-classifier

Options:
`, filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()

	archiveName := licenseclassifier.LicenseArchive
	if *forbiddenOnly {
		archiveName = licenseclassifier.ForbiddenLicenseArchive
	}

	fn := filepath.Join(*outputDir, archiveName)
	out, err := os.Create(fn)
	if err != nil {
		log.Fatalf("error: cannot create file %q: %v", fn, err)
	}
	defer out.Close()

	lics, err := licenseclassifier.ReadLicenseDir()
	if err != nil {
		log.Fatalf("error: cannot read licenses directory: %v", err)
	}

	var licenses []string
	for _, lic := range lics {
		if !*forbiddenOnly || licenseclassifier.LicenseType(strings.TrimSuffix(lic.Name(), ".txt")) == "FORBIDDEN" {
			licenses = append(licenses, lic.Name())
		}
	}

	if err := serializer.ArchiveLicenses(licenses, out); err != nil {
		log.Fatalf("error: cannot create database: %v", err)
	}
}
