package main

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

func main() {
	// Create the directory to store the unpacked YAML files.
	err := os.MkdirAll("config/networking", 0755)
	if err != nil {
		log.Fatalf("Failed to create directory: %v", err)
	}

	// Read the networking YAML files from the vendor directory.
	files, err := ioutil.ReadDir("vendor/knative.dev/networking/config")
	if err != nil {
		log.Fatalf("Failed to read directory: %v", err)
	}

	// Unpack the networking YAML files.
	for _, file := range files {
		// Skip directories.
		if file.IsDir() {
			continue
		}

		// Read the networking YAML file.
		data, err := ioutil.ReadFile(filepath.Join("vendor/knative.dev/networking/config", file.Name()))
		if err != nil {
			log.Fatalf("Failed to read file: %v", err)
		}

		// Write the networking YAML file to the unpacked directory.
		err = ioutil.WriteFile(filepath.Join("config/networking", file.Name()), data, 0644)
		if err != nil {
			log.Fatalf("Failed to write file: %v", err)
		}
	}
}
