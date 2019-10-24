/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"log"

	"knative.dev/pkg/testutils/metahelper/client"
)

var (
	getKeyOpt  string
	saveKeyOpt string
	valOpt     string
)

func main() {
	flag.StringVar(&getKeyOpt, "get", "", "get val for a key")
	flag.StringVar(&saveKeyOpt, "set", "", "save val for a key, must have --val supplied")
	flag.StringVar(&valOpt, "val", "", "val to be modified, only useful when --save is passed")
	flag.Parse()
	// Create with default path of metahelper/client, so that the path is
	// consistent with all other consumers of metahelper/client that run within
	// the same context of this tool
	c, err := client.NewClient("")
	if err != nil {
		log.Fatal(err)
	}

	var res string
	switch {
	case getKeyOpt != "" && saveKeyOpt != "":
		log.Fatal("--get and --save can't be used at the same time")
	case getKeyOpt != "":
		gotVal, err := c.Get(getKeyOpt)
		if err != nil {
			log.Fatalf("Failed getting value for %q from %q: '%v'", getKeyOpt, c.Path, err)
		}
		res = gotVal
	case saveKeyOpt != "":
		if valOpt == "" {
			log.Fatal("--val must be supplied when using --save")
		}
		log.Printf("Writing files to %s", c.Path)
		if err := c.Set(saveKeyOpt, valOpt); err != nil {
			log.Fatalf("Failed saving %q:%q to %q: '%v'", saveKeyOpt, valOpt, c.Path, err)
		}
	}
	fmt.Print(res)
}
