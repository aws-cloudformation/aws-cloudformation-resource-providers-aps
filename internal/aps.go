package internal

import (
	"errors"
	"log"
	"os"
	"strings"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/cloudformation"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/prometheusservice"
)

var (
	requestIDHeader   = "x-amzn-RequestId"
	xrayTraceIDHeader = "X-Amzn-Trace-Id"

	ErrInvalidResourceType = errors.New("invalid resource type")
)

// generalFailedEvent prevents us from leaking internal information to
// customers. there are no none cases where this could be a problem. This is
// just to be safe.
var generalFailedEvent = handler.ProgressEvent{
	OperationStatus:  handler.Failed,
	Message:          "Internal Failure",
	HandlerErrorCode: cloudformation.HandlerErrorCodeGeneralServiceException,
}

var (
	serviceErrorsToHandleErrors = map[string]string{
		"BadRequestException":                                  cloudformation.HandlerErrorCodeInvalidRequest,
		request.InvalidParameterErrCode:                        cloudformation.HandlerErrorCodeInvalidRequest,
		"InvalidRequest":                                       cloudformation.HandlerErrorCodeInvalidRequest,
		"TooManyRequestsException":                             cloudformation.HandlerErrorCodeThrottling,
		prometheusservice.ErrCodeThrottlingException:           cloudformation.HandlerErrorCodeThrottling,
		"NotFoundException":                                    cloudformation.HandlerErrorCodeNotFound,
		prometheusservice.ErrCodeResourceNotFoundException:     cloudformation.HandlerErrorCodeNotFound,
		prometheusservice.ErrCodeAccessDeniedException:         cloudformation.HandlerErrorCodeAccessDenied,
		prometheusservice.ErrCodeValidationException:           cloudformation.HandlerErrorCodeInvalidRequest,
		prometheusservice.ErrCodeConflictException:             cloudformation.HandlerErrorCodeResourceConflict,
		prometheusservice.ErrCodeServiceQuotaExceededException: cloudformation.HandlerErrorCodeServiceLimitExceeded,
	}
)

func NewFailedEvent(err error) (handler.ProgressEvent, error) {
	// log all errors in test mode
	if os.Getenv("MODE") == "Test" {
		log.Println(err)
	} // otherwise, only log unhandled errors

	var awsErr awserr.Error
	if !errors.As(err, &awsErr) {
		log.Printf("unhandled non awserr error: %v", err)
		return generalFailedEvent, nil
	}

	handlerErr, ok := serviceErrorsToHandleErrors[awsErr.Code()]
	if !ok {
		log.Printf("unhandled awserr error: %v", err)
		return generalFailedEvent, nil
	}

	message := awsErr.Message()
	if awsErr.Code() == request.InvalidParameterErrCode {
		message = awsErr.Error()
		message = strings.TrimPrefix(message, "InvalidParameter: ")
		// remove \n so validation errors are seen in Console / API
		message = strings.Replace(message, "\n", "", -1)
		// remove list notation
		message = strings.Replace(message, ".- ", ". ", -1)
	} else if awsErr.Code() == "TooManyRequestsException" {
		// TooManyRequestsException doesn't have a message by default
		// so the customer sees "Internal Failure" when message isn't set.
		message = "API rate limit exceeded"
	}

	message = awsErr.Code() + ": " + message

	return handler.ProgressEvent{
		OperationStatus:  handler.Failed,
		Message:          message,
		HandlerErrorCode: handlerErr,
	}, nil
}

var (
	supportedResourceTypes = map[string]struct{}{
		"workspace":           {},
		"rulegroupsnamespace": {},
	}
)

func ParseARN(value string) (*arn.ARN, string, error) {
	v, err := arn.Parse(value)
	if err != nil {
		return nil, "", err
	}

	resourceParts := strings.Split(v.Resource, "/")
	resourceType, resourceID := resourceParts[0], resourceParts[1]
	_, ok := supportedResourceTypes[resourceType]
	if !ok {
		return nil, "", ErrInvalidResourceType
	}

	return &v, resourceID, nil
}

type APSService interface {
	DescribeWorkspace(input *prometheusservice.DescribeWorkspaceInput) (*prometheusservice.DescribeWorkspaceOutput, error)
	DescribeAlertManagerDefinition(input *prometheusservice.DescribeAlertManagerDefinitionInput) (*prometheusservice.DescribeAlertManagerDefinitionOutput, error)
	CreateAlertManagerDefinition(input *prometheusservice.CreateAlertManagerDefinitionInput) (*prometheusservice.CreateAlertManagerDefinitionOutput, error)
	DeleteAlertManagerDefinition(input *prometheusservice.DeleteAlertManagerDefinitionInput) (*prometheusservice.DeleteAlertManagerDefinitionOutput, error)
	PutAlertManagerDefinition(input *prometheusservice.PutAlertManagerDefinitionInput) (*prometheusservice.PutAlertManagerDefinitionOutput, error)
}

func NewAPS(sess *session.Session) *prometheusservice.PrometheusService {
	sess.Handlers.Complete.PushBack(func(r *request.Request) {
		// Only consider requests with responses that have 5XX status codes
		if r.HTTPResponse != nil && r.HTTPResponse.StatusCode < 500 {
			return
		}

		// log operation, requestID, and traceID for debugging.
		log.Printf("%s:%s failed. requestID: %q, traceID: %q.\n",
			r.ClientInfo.ServiceName,
			r.Operation.Name,
			r.HTTPResponse.Header.Get(requestIDHeader),
			r.HTTPResponse.Header.Get(xrayTraceIDHeader),
		)
	})
	return prometheusservice.New(sess)
}

func StringDiffers(current, previous *string) bool {
	if (current == nil && previous != nil) || (current != nil && previous == nil) {
		return true
	}
	if current == nil && previous == nil {
		return false
	}
	return *current != *previous
}

func StringMapDifference(current, previous map[string]*string) (toChange map[string]*string, toRemove []*string) {
	toChange = map[string]*string{}
	toRemove = []*string{}

	for k, _ := range previous {
		// key no longer in current map. needs to be deleted
		if _, ok := current[k]; !ok {
			toRemove = append(toRemove, aws.String(k))
		}
	}

	for k, v := range current {
		oldV, ok := previous[k]
		if !ok || StringDiffers(v, oldV) {
			toChange[k] = v
		}
	}

	return
}
