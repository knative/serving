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

// Extendiability option for queue
//
// Provide MainWithPlugs with a list of extensions offering the interface QPExtension
//
// Example:
//		package main
//
//		import (
//			"package/path/to/ext1"
//			"package/path/to/ext2"
//		)
//
//		func main() {
//		  MainWithPlugs(ext1.QPExtension1, ext2.QPExtension2)
//		}

package main

func main() {
	MainWithPlugs()
}
