/*
Copyright 2020 The Knative Authors

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

package gcs

import (
	"context"

	"cloud.google.com/go/storage"
)

type Client interface {
	// NewStorageBucket creates a new bucket in GCS with uniform access policy
	NewStorageBucket(ctx context.Context, bkt, project string) error
	// DeleteStorageBucket removes all children objects, force if not empty
	DeleteStorageBucket(ctx context.Context, bkt string, force bool) error
	// Exists check if an object exists under a bucket, assuming bucket exists
	Exists(ctx context.Context, bkt, objPath string) bool
	// ListChildrenFiles recursively lists all children files
	ListChildrenFiles(ctx context.Context, bkt, dirPath string) ([]string, error)
	// ListDirectChildren lists direct children paths (incl. files and dir)
	ListDirectChildren(ctx context.Context, bkt, dirPath string) ([]string, error)
	// AttrObject returns the object attributes
	AttrObject(ctx context.Context, bkt, objPath string) (*storage.ObjectAttrs, error)
	// CopyObject copies objects from one location to another, assuming both src and dst
	// buckets both exist
	CopyObject(ctx context.Context, srcBkt, srcObjPath, dstBkt, dstObjPath string) error
	// ReadObject reads a GCS object and returns then contents in []byte
	ReadObject(ctx context.Context, bkt, objPath string) ([]byte, error)
	// WriteObject writes []byte content to a GCS object
	WriteObject(ctx context.Context, bkt, objPath string, content []byte) (int, error)
	// DeleteObject deletes an object
	DeleteObject(ctx context.Context, bkt, objPath string) error
	// Download downloads GCS object to a local file, assuming bucket exists
	Download(ctx context.Context, bktName, objPath, filePath string) error
	// Upload uploads a local file to a GCS object, assuming bucket exists
	Upload(ctx context.Context, bktName, objPath, filePath string) error
}
