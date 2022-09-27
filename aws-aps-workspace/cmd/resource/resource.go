package resource

import (
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/prometheusservice"
)

const (
	shortCallbackSeconds              = 2
	longCallbackSeconds               = 10
	ErrCodeWorkspaceNotFoundException = "WorkspaceNotFoundException"

	messageUpdateComplete = "Update Completed"
	messageCreateComplete = "Create Completed"
	messageInProgress     = "In Progress"
	stageKey              = "stage"
)

var (
	createResourceHandlers = []AMPResourceHandler{
		CreateWorkspace,
		CreateAlertManager,
		CreateLoggingConfiguration,
	}

	updateResourceHandlers = []AMPResourceHandler{
		UpdateWorkspace,
		UpdateAlertManager,
		UpdateLoggingConfiguration,
	}
)

// Create handles the Create event from the Cloudformation service.
func Create(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	if currentModel.WorkspaceId != nil && len(req.CallbackContext) == 0 {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Create: cannot create a resource using readOnly workspaceId property",
			HandlerErrorCode: cloudformation.HandlerErrorCodeInvalidRequest,
		}, nil
	}

	client := internal.NewAPS(req.Session)

	currentStage := currentStage(req)
	for stage := currentStage; stage < len(createResourceHandlers); stage++ {
		handler := createResourceHandlers[stage]
		if proceed, evt, err := handler(req, client, prevModel, currentModel); !proceed {
			addStageToCallbackContext(evt, stage)
			return evt, err
		}
	}

	// all done
	return handler.ProgressEvent{
		OperationStatus: handler.Success,
		Message:         messageCreateComplete,
		ResourceModel:   currentModel,
	}, nil

}

// Read handles the Read event from the Cloudformation service.
func Read(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	// contract test: contract_read_without_create
	if currentModel.Arn == nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Read: workspace Arn cannot be empty",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
		}, nil
	}

	client := internal.NewAPS(req.Session)
	if _, err := readWorkspace(client, currentModel); err != nil {
		return internal.NewFailedEvent(err)
	}
	if _, err := readAlertManagerDefinition(client, currentModel); err != nil {

		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() != prometheusservice.ErrCodeResourceNotFoundException {
				return internal.NewFailedEvent(err)
			}
		} else {
			return internal.NewFailedEvent(err)
		}
	}

	if _, err := readLoggingConfiguration(client, currentModel); err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			if awsErr.Code() != prometheusservice.ErrCodeResourceNotFoundException {
				return internal.NewFailedEvent(err)
			}
		} else {
			return internal.NewFailedEvent(err)
		}
	}

	return handler.ProgressEvent{
		OperationStatus: handler.Success,
		Message:         "Read Complete",
		ResourceModel:   currentModel,
	}, nil
}

// Update handles the Update event from the Cloudformation service.
func Update(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	// contract test: contract_read_without_create
	if currentModel.Arn == nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Update: workspace ARN cannot be empty",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
		}, nil
	}

	client := internal.NewAPS(req.Session)
	_, workspaceID, err := internal.ParseARN(*currentModel.Arn)
	if err != nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Read: invalid workspace ARN format",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
		}, nil
	}
	currentModel.WorkspaceId = aws.String(workspaceID)
	currentStage := currentStage(req)
	for stage := currentStage; stage < len(updateResourceHandlers); stage++ {
		handler := updateResourceHandlers[stage]
		if proceed, evt, err := handler(req, client, prevModel, currentModel); !proceed {
			addStageToCallbackContext(evt, stage)
			return evt, err
		}
	}

	return handler.ProgressEvent{
		ResourceModel:   currentModel,
		OperationStatus: handler.Success,
		Message:         messageUpdateComplete,
	}, nil

}

func addStageToCallbackContext(evt handler.ProgressEvent, stage int) {
	if evt.CallbackContext == nil {
		evt.CallbackContext = make(map[string]interface{})
	}
	evt.CallbackContext[stageKey] = stage
}

func currentStage(req handler.Request) int {
	if stg := req.CallbackContext[stageKey]; stg != nil {
		return int(stg.(float64))
	}
	return 0
}

// List handles the List event from the Cloudformation service.
func List(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	var nextToken *string

	if req.RequestContext.NextToken != "" {
		nextToken = &req.RequestContext.NextToken
	}

	resp, err := internal.NewAPS(req.Session).ListWorkspaces(&prometheusservice.ListWorkspacesInput{
		NextToken: nextToken,
	})
	if err != nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Internal Failure",
			HandlerErrorCode: cloudformation.HandlerErrorCodeGeneralServiceException,
		}, err
	}

	models := make([]interface{}, 0, len(resp.Workspaces))
	for _, ws := range resp.Workspaces {
		models = append(models, Model{
			WorkspaceId: ws.WorkspaceId,
			Alias:       ws.Alias,
			Arn:         ws.Arn,
			Tags:        stringMapToTags(ws.Tags),
		})
	}

	var responseNextToken string
	if resp.NextToken != nil {
		responseNextToken = *resp.NextToken
	}

	return handler.ProgressEvent{
		OperationStatus: handler.Success,
		Message:         "List complete",
		ResourceModels:  models,
		NextToken:       responseNextToken,
	}, nil
}
