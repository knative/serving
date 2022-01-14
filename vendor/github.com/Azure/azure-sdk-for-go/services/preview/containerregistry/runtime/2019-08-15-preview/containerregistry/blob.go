package containerregistry

// Copyright (c) Microsoft and contributors.  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Code generated by Microsoft (R) AutoRest Code Generator.
// Changes may cause incorrect behavior and will be lost if the code is regenerated.

import (
	"context"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/tracing"
	"io"
	"net/http"
)

// BlobClient is the metadata API definition for the Azure Container Registry runtime
type BlobClient struct {
	BaseClient
}

// NewBlobClient creates an instance of the BlobClient client.
func NewBlobClient(loginURI string) BlobClient {
	return BlobClient{New(loginURI)}
}

// CancelUpload cancel outstanding upload processes, releasing associated resources. If this is not called, the
// unfinished uploads will eventually timeout.
// Parameters:
// location - link acquired from upload start or previous chunk. Note, do not include initial / (must do
// substring(1) )
func (client BlobClient) CancelUpload(ctx context.Context, location string) (result autorest.Response, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.CancelUpload")
		defer func() {
			sc := -1
			if result.Response != nil {
				sc = result.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.CancelUploadPreparer(ctx, location)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "CancelUpload", nil, "Failure preparing request")
		return
	}

	resp, err := client.CancelUploadSender(req)
	if err != nil {
		result.Response = resp
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "CancelUpload", resp, "Failure sending request")
		return
	}

	result, err = client.CancelUploadResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "CancelUpload", resp, "Failure responding to request")
	}

	return
}

// CancelUploadPreparer prepares the CancelUpload request.
func (client BlobClient) CancelUploadPreparer(ctx context.Context, location string) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"nextBlobUuidLink": location,
	}

	preparer := autorest.CreatePreparer(
		autorest.AsDelete(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/{nextBlobUuidLink}", pathParameters))
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// CancelUploadSender sends the CancelUpload request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) CancelUploadSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// CancelUploadResponder handles the response to the CancelUpload request. The method always
// closes the http.Response Body.
func (client BlobClient) CancelUploadResponder(resp *http.Response) (result autorest.Response, err error) {
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK, http.StatusNoContent),
		autorest.ByClosing())
	result.Response = resp
	return
}

// Check same as GET, except only the headers are returned.
// Parameters:
// name - name of the image (including the namespace)
// digest - digest of a BLOB
func (client BlobClient) Check(ctx context.Context, name string, digest string) (result autorest.Response, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.Check")
		defer func() {
			sc := -1
			if result.Response != nil {
				sc = result.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.CheckPreparer(ctx, name, digest)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Check", nil, "Failure preparing request")
		return
	}

	resp, err := client.CheckSender(req)
	if err != nil {
		result.Response = resp
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Check", resp, "Failure sending request")
		return
	}

	result, err = client.CheckResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Check", resp, "Failure responding to request")
	}

	return
}

// CheckPreparer prepares the Check request.
func (client BlobClient) CheckPreparer(ctx context.Context, name string, digest string) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"digest": autorest.Encode("path", digest),
		"name":   autorest.Encode("path", name),
	}

	preparer := autorest.CreatePreparer(
		autorest.AsHead(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/v2/{name}/blobs/{digest}", pathParameters))
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// CheckSender sends the Check request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) CheckSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// CheckResponder handles the response to the Check request. The method always
// closes the http.Response Body.
func (client BlobClient) CheckResponder(resp *http.Response) (result autorest.Response, err error) {
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK, http.StatusTemporaryRedirect),
		autorest.ByClosing())
	result.Response = resp
	return
}

// CheckChunk same as GET, except only the headers are returned.
// Parameters:
// name - name of the image (including the namespace)
// digest - digest of a BLOB
// rangeParameter - format : bytes=<start>-<end>,  HTTP Range header specifying blob chunk.
func (client BlobClient) CheckChunk(ctx context.Context, name string, digest string, rangeParameter string) (result autorest.Response, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.CheckChunk")
		defer func() {
			sc := -1
			if result.Response != nil {
				sc = result.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.CheckChunkPreparer(ctx, name, digest, rangeParameter)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "CheckChunk", nil, "Failure preparing request")
		return
	}

	resp, err := client.CheckChunkSender(req)
	if err != nil {
		result.Response = resp
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "CheckChunk", resp, "Failure sending request")
		return
	}

	result, err = client.CheckChunkResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "CheckChunk", resp, "Failure responding to request")
	}

	return
}

// CheckChunkPreparer prepares the CheckChunk request.
func (client BlobClient) CheckChunkPreparer(ctx context.Context, name string, digest string, rangeParameter string) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"digest": autorest.Encode("path", digest),
		"name":   autorest.Encode("path", name),
	}

	preparer := autorest.CreatePreparer(
		autorest.AsHead(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/v2/{name}/blobs/{digest}", pathParameters),
		autorest.WithHeader("Range", autorest.String(rangeParameter)))
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// CheckChunkSender sends the CheckChunk request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) CheckChunkSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// CheckChunkResponder handles the response to the CheckChunk request. The method always
// closes the http.Response Body.
func (client BlobClient) CheckChunkResponder(resp *http.Response) (result autorest.Response, err error) {
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK),
		autorest.ByClosing())
	result.Response = resp
	return
}

// Delete removes an already uploaded blob.
// Parameters:
// name - name of the image (including the namespace)
// digest - digest of a BLOB
func (client BlobClient) Delete(ctx context.Context, name string, digest string) (result ReadCloser, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.Delete")
		defer func() {
			sc := -1
			if result.Response.Response != nil {
				sc = result.Response.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.DeletePreparer(ctx, name, digest)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Delete", nil, "Failure preparing request")
		return
	}

	resp, err := client.DeleteSender(req)
	if err != nil {
		result.Response = autorest.Response{Response: resp}
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Delete", resp, "Failure sending request")
		return
	}

	result, err = client.DeleteResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Delete", resp, "Failure responding to request")
	}

	return
}

// DeletePreparer prepares the Delete request.
func (client BlobClient) DeletePreparer(ctx context.Context, name string, digest string) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"digest": autorest.Encode("path", digest),
		"name":   autorest.Encode("path", name),
	}

	preparer := autorest.CreatePreparer(
		autorest.AsDelete(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/v2/{name}/blobs/{digest}", pathParameters))
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// DeleteSender sends the Delete request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) DeleteSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// DeleteResponder handles the response to the Delete request. The method always
// closes the http.Response Body.
func (client BlobClient) DeleteResponder(resp *http.Response) (result ReadCloser, err error) {
	result.Value = &resp.Body
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK, http.StatusAccepted))
	result.Response = autorest.Response{Response: resp}
	return
}

// EndUpload complete the upload, providing all the data in the body, if necessary. A request without a body will just
// complete the upload with previously uploaded content.
// Parameters:
// digest - digest of a BLOB
// location - link acquired from upload start or previous chunk. Note, do not include initial / (must do
// substring(1) )
// value - optional raw data of blob
func (client BlobClient) EndUpload(ctx context.Context, digest string, location string, value io.ReadCloser) (result autorest.Response, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.EndUpload")
		defer func() {
			sc := -1
			if result.Response != nil {
				sc = result.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.EndUploadPreparer(ctx, digest, location, value)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "EndUpload", nil, "Failure preparing request")
		return
	}

	resp, err := client.EndUploadSender(req)
	if err != nil {
		result.Response = resp
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "EndUpload", resp, "Failure sending request")
		return
	}

	result, err = client.EndUploadResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "EndUpload", resp, "Failure responding to request")
	}

	return
}

// EndUploadPreparer prepares the EndUpload request.
func (client BlobClient) EndUploadPreparer(ctx context.Context, digest string, location string, value io.ReadCloser) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"nextBlobUuidLink": location,
	}

	queryParameters := map[string]interface{}{
		"digest": autorest.Encode("query", digest),
	}

	preparer := autorest.CreatePreparer(
		autorest.AsContentType("application/octet-stream"),
		autorest.AsPut(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/{nextBlobUuidLink}", pathParameters),
		autorest.WithQueryParameters(queryParameters))
	if value != nil {
		preparer = autorest.DecoratePreparer(preparer,
			autorest.WithFile(value))
	}
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// EndUploadSender sends the EndUpload request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) EndUploadSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// EndUploadResponder handles the response to the EndUpload request. The method always
// closes the http.Response Body.
func (client BlobClient) EndUploadResponder(resp *http.Response) (result autorest.Response, err error) {
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK, http.StatusCreated),
		autorest.ByClosing())
	result.Response = resp
	return
}

// Get retrieve the blob from the registry identified by digest.
// Parameters:
// name - name of the image (including the namespace)
// digest - digest of a BLOB
func (client BlobClient) Get(ctx context.Context, name string, digest string) (result ReadCloser, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.Get")
		defer func() {
			sc := -1
			if result.Response.Response != nil {
				sc = result.Response.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.GetPreparer(ctx, name, digest)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Get", nil, "Failure preparing request")
		return
	}

	resp, err := client.GetSender(req)
	if err != nil {
		result.Response = autorest.Response{Response: resp}
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Get", resp, "Failure sending request")
		return
	}

	result, err = client.GetResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Get", resp, "Failure responding to request")
	}

	return
}

// GetPreparer prepares the Get request.
func (client BlobClient) GetPreparer(ctx context.Context, name string, digest string) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"digest": autorest.Encode("path", digest),
		"name":   autorest.Encode("path", name),
	}

	preparer := autorest.CreatePreparer(
		autorest.AsGet(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/v2/{name}/blobs/{digest}", pathParameters))
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// GetSender sends the Get request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) GetSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// GetResponder handles the response to the Get request. The method always
// closes the http.Response Body.
func (client BlobClient) GetResponder(resp *http.Response) (result ReadCloser, err error) {
	result.Value = &resp.Body
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK, http.StatusTemporaryRedirect))
	result.Response = autorest.Response{Response: resp}
	return
}

// GetChunk retrieve the blob from the registry identified by `digest`. This endpoint may also support RFC7233
// compliant range requests. Support can be detected by issuing a HEAD request. If the header `Accept-Range: bytes` is
// returned, range requests can be used to fetch partial content.
// Parameters:
// name - name of the image (including the namespace)
// digest - digest of a BLOB
// rangeParameter - format : bytes=<start>-<end>,  HTTP Range header specifying blob chunk.
func (client BlobClient) GetChunk(ctx context.Context, name string, digest string, rangeParameter string) (result ReadCloser, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.GetChunk")
		defer func() {
			sc := -1
			if result.Response.Response != nil {
				sc = result.Response.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.GetChunkPreparer(ctx, name, digest, rangeParameter)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "GetChunk", nil, "Failure preparing request")
		return
	}

	resp, err := client.GetChunkSender(req)
	if err != nil {
		result.Response = autorest.Response{Response: resp}
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "GetChunk", resp, "Failure sending request")
		return
	}

	result, err = client.GetChunkResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "GetChunk", resp, "Failure responding to request")
	}

	return
}

// GetChunkPreparer prepares the GetChunk request.
func (client BlobClient) GetChunkPreparer(ctx context.Context, name string, digest string, rangeParameter string) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"digest": autorest.Encode("path", digest),
		"name":   autorest.Encode("path", name),
	}

	preparer := autorest.CreatePreparer(
		autorest.AsGet(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/v2/{name}/blobs/{digest}", pathParameters),
		autorest.WithHeader("Range", autorest.String(rangeParameter)))
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// GetChunkSender sends the GetChunk request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) GetChunkSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// GetChunkResponder handles the response to the GetChunk request. The method always
// closes the http.Response Body.
func (client BlobClient) GetChunkResponder(resp *http.Response) (result ReadCloser, err error) {
	result.Value = &resp.Body
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK, http.StatusPartialContent))
	result.Response = autorest.Response{Response: resp}
	return
}

// GetStatus retrieve status of upload identified by uuid. The primary purpose of this endpoint is to resolve the
// current status of a resumable upload.
// Parameters:
// location - link acquired from upload start or previous chunk. Note, do not include initial / (must do
// substring(1) )
func (client BlobClient) GetStatus(ctx context.Context, location string) (result autorest.Response, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.GetStatus")
		defer func() {
			sc := -1
			if result.Response != nil {
				sc = result.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.GetStatusPreparer(ctx, location)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "GetStatus", nil, "Failure preparing request")
		return
	}

	resp, err := client.GetStatusSender(req)
	if err != nil {
		result.Response = resp
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "GetStatus", resp, "Failure sending request")
		return
	}

	result, err = client.GetStatusResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "GetStatus", resp, "Failure responding to request")
	}

	return
}

// GetStatusPreparer prepares the GetStatus request.
func (client BlobClient) GetStatusPreparer(ctx context.Context, location string) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"nextBlobUuidLink": location,
	}

	preparer := autorest.CreatePreparer(
		autorest.AsGet(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/{nextBlobUuidLink}", pathParameters))
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// GetStatusSender sends the GetStatus request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) GetStatusSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// GetStatusResponder handles the response to the GetStatus request. The method always
// closes the http.Response Body.
func (client BlobClient) GetStatusResponder(resp *http.Response) (result autorest.Response, err error) {
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK, http.StatusNoContent),
		autorest.ByClosing())
	result.Response = resp
	return
}

// Mount mount a blob identified by the `mount` parameter from another repository.
// Parameters:
// name - name of the image (including the namespace)
// from - name of the source repository.
// mount - digest of blob to mount from the source repository.
func (client BlobClient) Mount(ctx context.Context, name string, from string, mount string) (result autorest.Response, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.Mount")
		defer func() {
			sc := -1
			if result.Response != nil {
				sc = result.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.MountPreparer(ctx, name, from, mount)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Mount", nil, "Failure preparing request")
		return
	}

	resp, err := client.MountSender(req)
	if err != nil {
		result.Response = resp
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Mount", resp, "Failure sending request")
		return
	}

	result, err = client.MountResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Mount", resp, "Failure responding to request")
	}

	return
}

// MountPreparer prepares the Mount request.
func (client BlobClient) MountPreparer(ctx context.Context, name string, from string, mount string) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"name": autorest.Encode("path", name),
	}

	queryParameters := map[string]interface{}{
		"from":  autorest.Encode("query", from),
		"mount": autorest.Encode("query", mount),
	}

	preparer := autorest.CreatePreparer(
		autorest.AsPost(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/v2/{name}/blobs/uploads/", pathParameters),
		autorest.WithQueryParameters(queryParameters))
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// MountSender sends the Mount request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) MountSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// MountResponder handles the response to the Mount request. The method always
// closes the http.Response Body.
func (client BlobClient) MountResponder(resp *http.Response) (result autorest.Response, err error) {
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK, http.StatusCreated),
		autorest.ByClosing())
	result.Response = resp
	return
}

// StartUpload initiate a resumable blob upload with an empty request body.
// Parameters:
// name - name of the image (including the namespace)
func (client BlobClient) StartUpload(ctx context.Context, name string) (result autorest.Response, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.StartUpload")
		defer func() {
			sc := -1
			if result.Response != nil {
				sc = result.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.StartUploadPreparer(ctx, name)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "StartUpload", nil, "Failure preparing request")
		return
	}

	resp, err := client.StartUploadSender(req)
	if err != nil {
		result.Response = resp
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "StartUpload", resp, "Failure sending request")
		return
	}

	result, err = client.StartUploadResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "StartUpload", resp, "Failure responding to request")
	}

	return
}

// StartUploadPreparer prepares the StartUpload request.
func (client BlobClient) StartUploadPreparer(ctx context.Context, name string) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"name": autorest.Encode("path", name),
	}

	preparer := autorest.CreatePreparer(
		autorest.AsPost(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/v2/{name}/blobs/uploads/", pathParameters))
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// StartUploadSender sends the StartUpload request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) StartUploadSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// StartUploadResponder handles the response to the StartUpload request. The method always
// closes the http.Response Body.
func (client BlobClient) StartUploadResponder(resp *http.Response) (result autorest.Response, err error) {
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK, http.StatusAccepted),
		autorest.ByClosing())
	result.Response = resp
	return
}

// Upload upload a stream of data without completing the upload.
// Parameters:
// value - raw data of blob
// location - link acquired from upload start or previous chunk. Note, do not include initial / (must do
// substring(1) )
func (client BlobClient) Upload(ctx context.Context, value io.ReadCloser, location string) (result autorest.Response, err error) {
	if tracing.IsEnabled() {
		ctx = tracing.StartSpan(ctx, fqdn+"/BlobClient.Upload")
		defer func() {
			sc := -1
			if result.Response != nil {
				sc = result.Response.StatusCode
			}
			tracing.EndSpan(ctx, sc, err)
		}()
	}
	req, err := client.UploadPreparer(ctx, value, location)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Upload", nil, "Failure preparing request")
		return
	}

	resp, err := client.UploadSender(req)
	if err != nil {
		result.Response = resp
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Upload", resp, "Failure sending request")
		return
	}

	result, err = client.UploadResponder(resp)
	if err != nil {
		err = autorest.NewErrorWithError(err, "containerregistry.BlobClient", "Upload", resp, "Failure responding to request")
	}

	return
}

// UploadPreparer prepares the Upload request.
func (client BlobClient) UploadPreparer(ctx context.Context, value io.ReadCloser, location string) (*http.Request, error) {
	urlParameters := map[string]interface{}{
		"url": client.LoginURI,
	}

	pathParameters := map[string]interface{}{
		"nextBlobUuidLink": location,
	}

	preparer := autorest.CreatePreparer(
		autorest.AsContentType("application/octet-stream"),
		autorest.AsPatch(),
		autorest.WithCustomBaseURL("{url}", urlParameters),
		autorest.WithPathParameters("/{nextBlobUuidLink}", pathParameters),
		autorest.WithFile(value))
	return preparer.Prepare((&http.Request{}).WithContext(ctx))
}

// UploadSender sends the Upload request. The method will close the
// http.Response Body if it receives an error.
func (client BlobClient) UploadSender(req *http.Request) (*http.Response, error) {
	return client.Send(req, autorest.DoRetryForStatusCodes(client.RetryAttempts, client.RetryDuration, autorest.StatusCodesForRetry...))
}

// UploadResponder handles the response to the Upload request. The method always
// closes the http.Response Body.
func (client BlobClient) UploadResponder(resp *http.Response) (result autorest.Response, err error) {
	err = autorest.Respond(
		resp,
		azure.WithErrorUnlessStatusCode(http.StatusOK, http.StatusAccepted),
		autorest.ByClosing())
	result.Response = resp
	return
}
