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

// prow.go defines types and functions specific to prow logics
// All paths used in this package are gcs paths unless specified otherwise

package prow

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"knative.dev/pkg/test/gcs"
)

const (
	// OrgName is the name of knative org
	OrgName = "knative"

	// BucketName is the gcs bucket for all knative builds
	BucketName = "knative-prow"
	// Latest is the filename storing latest build number
	Latest = "latest-build.txt"
	// BuildLog is the filename for build log
	BuildLog = "build-log.txt"
	// StartedJSON is the json file containing build started info
	StartedJSON = "started.json"
	// FinishedJSON is the json file containing build finished info
	FinishedJSON = "finished.json"
	// ArtifactsDir is the dir containing artifacts
	ArtifactsDir = "artifacts"

	// PresubmitJob means it runs on unmerged PRs.
	PresubmitJob = "presubmit"
	// PostsubmitJob means it runs on each new commit.
	PostsubmitJob = "postsubmit"
	// PeriodicJob means it runs on a time-basis, unrelated to git changes.
	PeriodicJob = "periodic"
	// BatchJob tests multiple unmerged PRs at the same time.
	BatchJob = "batch"
)

// defined here so that it can be mocked for unit testing
var logFatalf = log.Fatalf

var ctx = context.Background()

// Job struct represents a job directory in gcs.
// gcs job StoragePath will be derived from Type if it's defined,
type Job struct {
	Name        string
	Type        string
	Bucket      string  // optional
	Repo        string  // optional
	StoragePath string  // optional
	PullID      int     // only for Presubmit jobs
	Builds      []Build // optional
}

// Build points to a build stored under a particular gcs path.
type Build struct {
	JobName     string
	StoragePath string
	BuildID     int
	Bucket      string // optional
	StartTime   *int64
	FinishTime  *int64
}

// Started holds the started.json values of the build.
type Started struct {
	Timestamp   int64             `json:"timestamp"` // epoch seconds
	RepoVersion string            `json:"repo-version"`
	Node        string            `json:"node"`
	Pull        string            `json:"pull"`
	Repos       map[string]string `json:"repos"` // {repo: branch_or_pull} map
}

// Finished holds the finished.json values of the build
type Finished struct {
	// Timestamp is epoch seconds
	Timestamp  int64    `json:"timestamp"`
	Passed     bool     `json:"passed"`
	JobVersion string   `json:"job-version"`
	Metadata   Metadata `json:"metadata"`
}

// Metadata contains metadata in finished.json
type Metadata map[string]interface{}

// IsCI returns whether the current environment is a CI environment.
func IsCI() bool {
	return strings.EqualFold(os.Getenv("CI"), "true")
}

/* Local logics */

// GetLocalArtifactsDir gets the artifacts directory where prow looks for artifacts.
// By default, it will look at the env var ARTIFACTS.
func GetLocalArtifactsDir() string {
	dir := os.Getenv("ARTIFACTS")
	if dir == "" {
		log.Printf("Env variable ARTIFACTS not set. Using %s instead.", ArtifactsDir)
		dir = ArtifactsDir
	}
	return dir
}

/* GCS related logics */

// Initialize wraps gcs authentication, have to be invoked before any other functions
func Initialize(serviceAccount string) error {
	return gcs.Authenticate(ctx, serviceAccount)
}

// NewJob creates new job struct
// pullID is only saved by Presubmit job for determining StoragePath
func NewJob(jobName, jobType, repoName string, pullID int) *Job {
	job := Job{
		Name:   jobName,
		Type:   jobType,
		Bucket: BucketName,
	}

	switch jobType {
	case PeriodicJob, PostsubmitJob:
		job.StoragePath = path.Join("logs", jobName)
	case PresubmitJob:
		job.PullID = pullID
		job.StoragePath = path.Join("pr-logs", "pull", OrgName+"_"+repoName, strconv.Itoa(pullID), jobName)
	case BatchJob:
		job.StoragePath = path.Join("pr-logs", "pull", "batch", jobName)
	default:
		logFatalf("unknown job spec type: %v", jobType)
	}
	return &job
}

// PathExists checks if the storage path of a job exists in gcs or not
func (j *Job) PathExists() bool {
	return gcs.Exists(ctx, BucketName, j.StoragePath)
}

// GetLatestBuildNumber gets the latest build number for job
func (j *Job) GetLatestBuildNumber() (int, error) {
	logFilePath := path.Join(j.StoragePath, Latest)
	contents, err := gcs.Read(ctx, BucketName, logFilePath)
	if err != nil {
		return 0, err
	}
	latestBuild, err := strconv.Atoi(strings.TrimSuffix(string(contents), "\n"))
	if err != nil {
		return 0, err
	}
	return latestBuild, nil
}

// NewBuild gets build struct based on job info
// No gcs operation is performed by this function
func (j *Job) NewBuild(buildID int) *Build {
	build := Build{
		Bucket:      BucketName,
		JobName:     j.Name,
		StoragePath: path.Join(j.StoragePath, strconv.Itoa(buildID)),
		BuildID:     buildID,
	}

	if startTime, err := build.GetStartTime(); err == nil {
		build.StartTime = &startTime
	}
	if finishTime, err := build.GetFinishTime(); err == nil {
		build.FinishTime = &finishTime
	}
	return &build
}

// GetFinishedBuilds gets all builds that have finished,
// by looking at existence of "finished.json" file
func (j *Job) GetFinishedBuilds() []Build {
	var finishedBuilds []Build
	builds := j.GetBuilds()
	for _, build := range builds {
		if build.IsFinished() {
			finishedBuilds = append(finishedBuilds, build)
		}
	}
	return finishedBuilds
}

// GetBuilds gets all builds from this job on gcs, precomputes start/finish time of builds
// by parsing "Started.json" and "Finished.json" on gcs, could be very expensive if there are
// large number of builds
func (j *Job) GetBuilds() []Build {
	var builds []Build
	for _, ID := range j.GetBuildIDs() {
		builds = append(builds, *j.NewBuild(ID))
	}
	return builds
}

// GetBuildIDs gets all build IDs from this job on gcs, scans all direct child of gcs directory
// for job, keeps the ones that can be parsed as integer
func (j *Job) GetBuildIDs() []int {
	var buildIDs []int
	gcsBuildPaths := gcs.ListDirectChildren(ctx, j.Bucket, j.StoragePath)
	for _, gcsBuildPath := range gcsBuildPaths {
		if buildID, err := getBuildIDFromBuildPath(gcsBuildPath); err == nil {
			buildIDs = append(buildIDs, buildID)
		}
	}
	return buildIDs
}

// GetLatestBuilds get latest builds from gcs, sort by start time from newest to oldest,
// will return count number of builds
func (j *Job) GetLatestBuilds(count int) []Build {
	// The timestamp of gcs directories are not usable,
	// as they are all set to '0001-01-01 00:00:00 +0000 UTC',
	// so use 'started.json' creation date for latest builds
	builds := j.GetFinishedBuilds()
	sort.Slice(builds, func(i, j int) bool {
		if builds[i].StartTime == nil {
			return false
		}
		if builds[j].StartTime == nil {
			return true
		}
		return *builds[i].StartTime > *builds[j].StartTime
	})
	if len(builds) < count {
		return builds
	}
	return builds[:count]
}

// IsStarted check if build has started by looking at "started.json" file
func (b *Build) IsStarted() bool {
	return gcs.Exists(ctx, BucketName, path.Join(b.StoragePath, StartedJSON))
}

// IsFinished check if build has finished by looking at "finished.json" file
func (b *Build) IsFinished() bool {
	return gcs.Exists(ctx, BucketName, path.Join(b.StoragePath, FinishedJSON))
}

// GetStartTime gets started timestamp of a build,
// returning -1 if the build didn't start or if it failed to get the timestamp
func (b *Build) GetStartTime() (int64, error) {
	var started Started
	if err := unmarshalJSONFile(path.Join(b.StoragePath, StartedJSON), &started); err != nil {
		return -1, err
	}
	return started.Timestamp, nil
}

// GetFinishTime gets finished timestamp of a build,
// returning -1 if the build didn't finish or if it failed to get the timestamp
func (b *Build) GetFinishTime() (int64, error) {
	var finished Finished
	if err := unmarshalJSONFile(path.Join(b.StoragePath, FinishedJSON), &finished); err != nil {
		return -1, err
	}
	return finished.Timestamp, nil
}

// GetArtifacts gets gcs path for all artifacts of current build
func (b *Build) GetArtifacts() []string {
	return gcs.ListChildrenFiles(ctx, BucketName, b.GetArtifactsDir())
}

// GetArtifactsDir gets gcs path for artifacts of current build
func (b *Build) GetArtifactsDir() string {
	return path.Join(b.StoragePath, ArtifactsDir)
}

// GetBuildLogPath gets "build-log.txt" path for current build
func (b *Build) GetBuildLogPath() string {
	return path.Join(b.StoragePath, BuildLog)
}

// ReadFile reads given file of current build,
// relPath is the file path relative to build directory
func (b *Build) ReadFile(relPath string) ([]byte, error) {
	return gcs.Read(ctx, BucketName, path.Join(b.StoragePath, relPath))
}

// ParseLog parses the build log and returns the lines where the checkLog func does not return an empty slice,
// checkLog function should take in the log statement and return a part from that statement that should be in the log output.
func (b *Build) ParseLog(checkLog func(s []string) *string) ([]string, error) {
	var logs []string

	f, err := gcs.NewReader(ctx, b.Bucket, b.GetBuildLogPath())
	if err != nil {
		return logs, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if s := checkLog(strings.Fields(scanner.Text())); s != nil {
			logs = append(logs, *s)
		}
	}
	return logs, nil
}

// getBuildIDFromBuildPath digests gcs build path and return last portion of path
func getBuildIDFromBuildPath(buildPath string) (int, error) {
	_, buildIDStr := path.Split(strings.TrimRight(buildPath, " /"))
	return strconv.Atoi(buildIDStr)
}

// unmarshalJSONFile reads a file from gcs, parses it with xml and write to v.
// v must be an arbitrary struct, slice, or string.
func unmarshalJSONFile(storagePath string, v interface{}) error {
	contents, err := gcs.Read(ctx, BucketName, storagePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(contents, v)
}
