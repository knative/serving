// Code generated by smithy-go-codegen DO NOT EDIT.

package ecr

import (
	"context"
	"fmt"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Creates or updates the image manifest and tags associated with an image.
//
// When an image is pushed and all new image layers have been uploaded, the
// PutImage API is called once to create or update the image manifest and the tags
// associated with the image.
//
// This operation is used by the Amazon ECR proxy and is not generally used by
// customers for pulling and pushing images. In most cases, you should use the
// docker CLI to pull, tag, and push images.
func (c *Client) PutImage(ctx context.Context, params *PutImageInput, optFns ...func(*Options)) (*PutImageOutput, error) {
	if params == nil {
		params = &PutImageInput{}
	}

	result, metadata, err := c.invokeOperation(ctx, "PutImage", params, optFns, c.addOperationPutImageMiddlewares)
	if err != nil {
		return nil, err
	}

	out := result.(*PutImageOutput)
	out.ResultMetadata = metadata
	return out, nil
}

type PutImageInput struct {

	// The image manifest corresponding to the image to be uploaded.
	//
	// This member is required.
	ImageManifest *string

	// The name of the repository in which to put the image.
	//
	// This member is required.
	RepositoryName *string

	// The image digest of the image manifest corresponding to the image.
	ImageDigest *string

	// The media type of the image manifest. If you push an image manifest that does
	// not contain the mediaType field, you must specify the imageManifestMediaType in
	// the request.
	ImageManifestMediaType *string

	// The tag to associate with the image. This parameter is required for images that
	// use the Docker Image Manifest V2 Schema 2 or Open Container Initiative (OCI)
	// formats.
	ImageTag *string

	// The Amazon Web Services account ID associated with the registry that contains
	// the repository in which to put the image. If you do not specify a registry, the
	// default registry is assumed.
	RegistryId *string

	noSmithyDocumentSerde
}

type PutImageOutput struct {

	// Details of the image uploaded.
	Image *types.Image

	// Metadata pertaining to the operation's result.
	ResultMetadata middleware.Metadata

	noSmithyDocumentSerde
}

func (c *Client) addOperationPutImageMiddlewares(stack *middleware.Stack, options Options) (err error) {
	if err := stack.Serialize.Add(&setOperationInputMiddleware{}, middleware.After); err != nil {
		return err
	}
	err = stack.Serialize.Add(&awsAwsjson11_serializeOpPutImage{}, middleware.After)
	if err != nil {
		return err
	}
	err = stack.Deserialize.Add(&awsAwsjson11_deserializeOpPutImage{}, middleware.After)
	if err != nil {
		return err
	}
	if err := addProtocolFinalizerMiddlewares(stack, options, "PutImage"); err != nil {
		return fmt.Errorf("add protocol finalizers: %v", err)
	}

	if err = addlegacyEndpointContextSetter(stack, options); err != nil {
		return err
	}
	if err = addSetLoggerMiddleware(stack, options); err != nil {
		return err
	}
	if err = addClientRequestID(stack); err != nil {
		return err
	}
	if err = addComputeContentLength(stack); err != nil {
		return err
	}
	if err = addResolveEndpointMiddleware(stack, options); err != nil {
		return err
	}
	if err = addComputePayloadSHA256(stack); err != nil {
		return err
	}
	if err = addRetry(stack, options); err != nil {
		return err
	}
	if err = addRawResponseToMetadata(stack); err != nil {
		return err
	}
	if err = addRecordResponseTiming(stack); err != nil {
		return err
	}
	if err = addSpanRetryLoop(stack, options); err != nil {
		return err
	}
	if err = addClientUserAgent(stack, options); err != nil {
		return err
	}
	if err = smithyhttp.AddErrorCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = addSetLegacyContextSigningOptionsMiddleware(stack); err != nil {
		return err
	}
	if err = addTimeOffsetBuild(stack, c); err != nil {
		return err
	}
	if err = addUserAgentRetryMode(stack, options); err != nil {
		return err
	}
	if err = addOpPutImageValidationMiddleware(stack); err != nil {
		return err
	}
	if err = stack.Initialize.Add(newServiceMetadataMiddleware_opPutImage(options.Region), middleware.Before); err != nil {
		return err
	}
	if err = addRecursionDetection(stack); err != nil {
		return err
	}
	if err = addRequestIDRetrieverMiddleware(stack); err != nil {
		return err
	}
	if err = addResponseErrorMiddleware(stack); err != nil {
		return err
	}
	if err = addRequestResponseLogging(stack, options); err != nil {
		return err
	}
	if err = addDisableHTTPSMiddleware(stack, options); err != nil {
		return err
	}
	if err = addSpanInitializeStart(stack); err != nil {
		return err
	}
	if err = addSpanInitializeEnd(stack); err != nil {
		return err
	}
	if err = addSpanBuildRequestStart(stack); err != nil {
		return err
	}
	if err = addSpanBuildRequestEnd(stack); err != nil {
		return err
	}
	return nil
}

func newServiceMetadataMiddleware_opPutImage(region string) *awsmiddleware.RegisterServiceMetadata {
	return &awsmiddleware.RegisterServiceMetadata{
		Region:        region,
		ServiceID:     ServiceID,
		OperationName: "PutImage",
	}
}
