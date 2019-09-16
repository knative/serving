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

// gcs.go defines functions to use GCS

package gcs

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

var client *storage.Client

// Authenticate explicitly sets up authentication for the rest of run
func Authenticate(ctx context.Context, serviceAccount string) error {
	var err error
	client, err = storage.NewClient(ctx, option.WithCredentialsFile(serviceAccount))
	return err
}

// Exists checks if path exist under gcs bucket,
// this path can either be a directory or a file.
func Exists(ctx context.Context, bucketName, storagePath string) bool {
	// Check if this is a file
	handle := createStorageObject(bucketName, storagePath)
	if _, err := handle.Attrs(ctx); err == nil {
		return true
	}
	// Check if this is a directory,
	// gcs directory paths are virtual paths, they automatically got deleted if there is no child file
	_, err := getObjectsIter(ctx, bucketName, strings.TrimRight(storagePath, " /")+"/", "").Next()
	return err == nil
}

// ListChildrenFiles recursively lists all children files.
func ListChildrenFiles(ctx context.Context, bucketName, storagePath string) []string {
	return list(ctx, bucketName, strings.TrimRight(storagePath, " /")+"/", "")
}

// ListDirectChildren lists direct children paths (including files and directories).
func ListDirectChildren(ctx context.Context, bucketName, storagePath string) []string {
	// If there are 2 directories named "foo" and "foobar",
	// then given storagePath "foo" will get files both under "foo" and "foobar".
	// Add trailling slash to storagePath, so that only gets children under given directory.
	return list(ctx, bucketName, strings.TrimRight(storagePath, " /")+"/", "/")
}

// Copy file from within gcs
func Copy(ctx context.Context, srcBucketName, srcPath, dstBucketName, dstPath string) error {
	src := createStorageObject(srcBucketName, srcPath)
	dst := createStorageObject(dstBucketName, dstPath)

	_, err := dst.CopierFrom(src).Run(ctx)
	return err
}

// Download file from gcs
func Download(ctx context.Context, bucketName, srcPath, dstPath string) error {
	handle := createStorageObject(bucketName, srcPath)
	if _, err := handle.Attrs(ctx); err != nil {
		return err
	}

	dst, err := os.OpenFile(dstPath, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return err
	}
	src, err := handle.NewReader(ctx)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(dst, src)
	return err
}

// Upload file to gcs
func Upload(ctx context.Context, bucketName, dstPath, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	dst := createStorageObject(bucketName, dstPath).NewWriter(ctx)
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

// Read reads the specified file
func Read(ctx context.Context, bucketName, filePath string) ([]byte, error) {
	var contents []byte
	f, err := NewReader(ctx, bucketName, filePath)
	if err != nil {
		return contents, err
	}
	defer f.Close()
	return ioutil.ReadAll(f)
}

// ReadURL reads from a gsUrl and return a log structure
func ReadURL(ctx context.Context, gcsURL string) ([]byte, error) {
	bucket, obj, err := linkToBucketAndObject(gcsURL)
	if err != nil {
		return nil, err
	}

	return Read(ctx, bucket, obj)
}

// NewReader creates a new Reader of a gcs file.
// Important: caller must call Close on the returned Reader when done reading
func NewReader(ctx context.Context, bucketName, filePath string) (*storage.Reader, error) {
	o := createStorageObject(bucketName, filePath)
	if _, err := o.Attrs(ctx); err != nil {
		return nil, err
	}
	return o.NewReader(ctx)
}

// BuildLogPath returns the build log path from the test result gcsURL
func BuildLogPath(gcsURL string) (string, error) {
	u, err := url.Parse(gcsURL)
	if err != nil {
		return gcsURL, err
	}
	u.Path = path.Join(u.Path, "build-log.txt")
	return u.String(), nil
}

// GetConsoleURL returns the gcs link renderable directly from a browser
func GetConsoleURL(gcsURL string) (string, error) {
	u, err := url.Parse(gcsURL)
	if err != nil {
		return gcsURL, err
	}
	u.Path = path.Join("storage/browser", u.Host, u.Path)
	u.Scheme = "https"
	u.Host = "console.cloud.google.com"
	return u.String(), nil
}

// create storage object handle, this step doesn't access internet
func createStorageObject(bucketName, filePath string) *storage.ObjectHandle {
	return client.Bucket(bucketName).Object(filePath)
}

// Query items under given gcs storagePath, use exclusionFilter to eliminate some files.
func getObjectsAttrs(ctx context.Context, bucketName, storagePath, exclusionFilter string) []*storage.ObjectAttrs {
	var allAttrs []*storage.ObjectAttrs
	it := getObjectsIter(ctx, bucketName, storagePath, exclusionFilter)

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("Error iterating: %v", err)
		}
		allAttrs = append(allAttrs, attrs)
	}
	return allAttrs
}

// list child under storagePath, use exclusionFilter for skipping some files.
// This function gets all child files recursively under given storagePath,
// then filter out filenames containing giving exclusionFilter.
// If exclusionFilter is empty string, returns all files but not directories,
// if exclusionFilter is "/", returns all direct children, including both files and directories.
// see https://godoc.org/cloud.google.com/go/storage#Query
func list(ctx context.Context, bucketName, storagePath, exclusionFilter string) []string {
	var filePaths []string
	objsAttrs := getObjectsAttrs(ctx, bucketName, storagePath, exclusionFilter)
	for _, attrs := range objsAttrs {
		filePaths = append(filePaths, path.Join(attrs.Prefix, attrs.Name))
	}
	return filePaths
}

// get objects iterator under given storagePath and bucketName, use exclusionFilter to eliminate some files.
func getObjectsIter(ctx context.Context, bucketName, storagePath, exclusionFilter string) *storage.ObjectIterator {
	return client.Bucket(bucketName).Objects(ctx, &storage.Query{
		Prefix:    storagePath,
		Delimiter: exclusionFilter,
	})
}

// get the bucket and object from the gsURL
func linkToBucketAndObject(gsURL string) (string, string, error) {
	var bucket, obj string
	gsURL = strings.Replace(gsURL, "gs://", "", 1)

	sIdx := strings.IndexByte(gsURL, '/')
	if sIdx == -1 || sIdx+1 >= len(gsURL) {
		return bucket, obj, fmt.Errorf("the gsUrl (%s) cannot be converted to bucket/object", gsURL)
	}

	return gsURL[:sIdx], gsURL[sIdx+1:], nil
}
