package resource

import (
	"fmt"
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"strings"

	"github.com/aws/aws-sdk-go/service/prometheusservice"
)

const (
	defaultCallbackSeconds             = 2
	waitForWorkspaceStatusKey          = "Arn" // for backwards compatibility during release
	waitForAlertManagerStatusActiveKey = "waitForAlertManagerActive"
	waitForAlertManagerStatusDeleteKey = "waitForAlertManagerDeleted"

	messageUpdateComplete = "Update Completed"
	messageCreateComplete = "Create Completed"
	messageInProgress     = "In Progress"
)

var alertManagerFailedStates = map[string]struct{}{
	prometheusservice.AlertManagerDefinitionStatusCodeCreationFailed: {},
	prometheusservice.AlertManagerDefinitionStatusCodeUpdateFailed:   {},
}

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
	// wait for workspace to be ACTIVE before managing alert manager configuration
	if arn, ok := req.CallbackContext[waitForWorkspaceStatusKey]; ok {
		currentModel.Arn = aws.String(arn.(string))

		evt, err := validateWorkspaceState(
			client,
			currentModel,
			prometheusservice.WorkspaceStatusCodeActive,
			messageCreateComplete)
		if evt.OperationStatus == handler.InProgress || currentModel.AlertManagerDefinition == nil {
			return evt, err
		}

		return createAlertManagerDefinition(req, client, currentModel)
	}

	// AlertManagerDefinition is always created last. As such we have to continue waiting after the Workspace is created
	if arn, ok := req.CallbackContext[waitForAlertManagerStatusActiveKey]; ok {
		currentModel.Arn = aws.String(arn.(string))

		return validateAlertManagerState(client,
			currentModel,
			prometheusservice.AlertManagerDefinitionStatusCodeActive,
			messageCreateComplete)
	}

	resp, err := client.CreateWorkspace(&prometheusservice.CreateWorkspaceInput{
		Alias: currentModel.Alias,
		Tags:  tagsToStringMap(currentModel.Tags),
	})
	if err != nil {
		return internal.NewFailedEvent(err)
	}
	currentModel.Arn = resp.Arn

	return handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              messageInProgress,
		ResourceModel:        currentModel,
		CallbackDelaySeconds: defaultCallbackSeconds,
		CallbackContext:      buildWaitForWorkspaceStatusCallbackContext(currentModel),
	}, nil
}

func createAlertManagerDefinition(req handler.Request, client internal.APSService, currentModel *Model) (handler.ProgressEvent, error) {
	_, err := client.CreateAlertManagerDefinition(&prometheusservice.CreateAlertManagerDefinitionInput{
		Data:        []byte(aws.StringValue(currentModel.AlertManagerDefinition)),
		WorkspaceId: currentModel.WorkspaceId,
	})

	if err != nil {
		return internal.NewFailedEvent(err)
	}

	return handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              messageInProgress,
		ResourceModel:        currentModel,
		CallbackDelaySeconds: defaultCallbackSeconds,
		CallbackContext:      buildWaitForAlertManagerStatusCallbackContext(currentModel, waitForAlertManagerStatusActiveKey),
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

	if arn, ok := req.CallbackContext[waitForWorkspaceStatusKey]; ok {
		currentModel.Arn = aws.String(arn.(string))

		evt, err := validateWorkspaceState(
			client,
			currentModel,
			prometheusservice.WorkspaceStatusCodeActive,
			messageUpdateComplete)
		if err != nil {
			return internal.NewFailedEvent(err)
		}

		if evt.OperationStatus == handler.InProgress {
			return evt, err
		}

		if !internal.StringDiffers(currentModel.AlertManagerDefinition, prevModel.AlertManagerDefinition) {
			return evt, err
		}

		return manageAlertManagerDefinition(currentModel, prevModel, client)
	}

	// AlertManagerDefinition is always updated last. As such we have to continue waiting after the Workspace is in ACTIVE state again
	if arn, ok := req.CallbackContext[waitForAlertManagerStatusActiveKey]; ok {
		currentModel.Arn = aws.String(arn.(string))

		return validateAlertManagerState(client,
			currentModel,
			prometheusservice.AlertManagerDefinitionStatusCodeActive,
			messageUpdateComplete)
	}

	if arn, ok := req.CallbackContext[waitForAlertManagerStatusDeleteKey]; ok {
		currentModel.Arn = aws.String(arn.(string))

		return validateAlertManagerDeleted(client,
			currentModel,
			messageUpdateComplete)
	}

	if internal.StringDiffers(currentModel.Alias, prevModel.Alias) {
		_, err = client.
			UpdateWorkspaceAlias(&prometheusservice.UpdateWorkspaceAliasInput{
				WorkspaceId: aws.String(workspaceID),
				Alias:       currentModel.Alias,
			})
		if err != nil {
			return internal.NewFailedEvent(err)
		}
	}

	toAdd, toRemove := internal.StringMapDifference(tagsToStringMap(currentModel.Tags), tagsToStringMap(prevModel.Tags))
	if len(toRemove) > 0 {
		_, err = client.UntagResource(&prometheusservice.UntagResourceInput{
			ResourceArn: currentModel.Arn,
			TagKeys:     toRemove,
		})
		if err != nil {
			return internal.NewFailedEvent(err)
		}
	}

	if len(toAdd) > 0 {
		_, err = client.TagResource(&prometheusservice.TagResourceInput{
			ResourceArn: currentModel.Arn,
			Tags:        toAdd,
		})
		if err != nil {
			return internal.NewFailedEvent(err)
		}
	}

	return handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              messageInProgress,
		ResourceModel:        currentModel,
		CallbackDelaySeconds: defaultCallbackSeconds,
		CallbackContext:      buildWaitForWorkspaceStatusCallbackContext(currentModel),
	}, nil
}

// manageAlertManagerDefinition handles AlertManagerDefinition state changes for UPDATE calls
func manageAlertManagerDefinition(
	currentModel *Model,
	prevModel *Model,
	client internal.APSService) (handler.ProgressEvent, error) {
	var err error

	shouldCreateAlertManagerDefinition := currentModel.AlertManagerDefinition != nil &&
		prevModel.AlertManagerDefinition == nil &&
		strings.TrimSpace(aws.StringValue(currentModel.AlertManagerDefinition)) != ""

	shouldDeleteAlertManagerDefinition := currentModel.AlertManagerDefinition == nil
	key := waitForAlertManagerStatusActiveKey

	if shouldCreateAlertManagerDefinition {
		_, err = client.CreateAlertManagerDefinition(&prometheusservice.CreateAlertManagerDefinitionInput{
			Data:        []byte(aws.StringValue(currentModel.AlertManagerDefinition)),
			WorkspaceId: currentModel.WorkspaceId,
		})
	} else if shouldDeleteAlertManagerDefinition {
		_, err = client.DeleteAlertManagerDefinition(&prometheusservice.DeleteAlertManagerDefinitionInput{
			WorkspaceId: currentModel.WorkspaceId,
		})
		if err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				if awsErr.Code() == prometheusservice.ErrCodeResourceNotFoundException {
					err = nil
				}
			}
		}
		key = waitForAlertManagerStatusDeleteKey
	} else {
		_, err = client.PutAlertManagerDefinition(&prometheusservice.PutAlertManagerDefinitionInput{
			Data:        []byte(aws.StringValue(currentModel.AlertManagerDefinition)),
			WorkspaceId: currentModel.WorkspaceId,
		})
	}

	if err != nil {
		return internal.NewFailedEvent(err)
	}

	return handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              messageInProgress,
		ResourceModel:        currentModel,
		CallbackDelaySeconds: defaultCallbackSeconds,
		CallbackContext:      buildWaitForAlertManagerStatusCallbackContext(currentModel, key),
	}, nil
}

// Delete handles the Delete event from the Cloudformation service.
func Delete(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	if currentModel.Arn == nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Delete: workspace ARN cannot be empty",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
		}, nil
	}

	client := internal.NewAPS(req.Session)
	if _, ok := req.CallbackContext[waitForWorkspaceStatusKey]; ok {
		currentModel.Arn = aws.String(req.CallbackContext[waitForWorkspaceStatusKey].(string))
		return validateWorkspaceDeleted(
			client,
			currentModel,
			"Delete Complete")
	}

	_, workspaceID, err := internal.ParseARN(*currentModel.Arn)
	if err != nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Read: invalid workspace ARN format",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
		}, nil
	}

	// no need to delete AlertManagerDefinition here, because APSService deletes this when the workspace is deleted
	_, err = client.
		DeleteWorkspace(&prometheusservice.DeleteWorkspaceInput{
			WorkspaceId: aws.String(workspaceID),
		})
	if err != nil {
		return internal.NewFailedEvent(err)
	}

	return handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              messageInProgress,
		ResourceModel:        currentModel,
		CallbackDelaySeconds: defaultCallbackSeconds,
		CallbackContext:      buildWaitForWorkspaceStatusCallbackContext(currentModel),
	}, nil
}

func validateWorkspaceDeleted(client internal.APSService, currentModel *Model, successMessage string) (handler.ProgressEvent, error) {
	_, err := readWorkspace(client, currentModel)
	if err == nil {
		return handler.ProgressEvent{
			ResourceModel:        currentModel,
			OperationStatus:      handler.InProgress,
			Message:              messageInProgress,
			CallbackDelaySeconds: defaultCallbackSeconds,
			CallbackContext:      buildWaitForWorkspaceStatusCallbackContext(currentModel),
		}, nil
	}

	if awsErr, ok := err.(awserr.Error); ok {
		if awsErr.Code() == prometheusservice.ErrCodeResourceNotFoundException {
			return handler.ProgressEvent{
				OperationStatus: handler.Success,
				Message:         successMessage,
			}, nil
		}
	}

	return handler.ProgressEvent{}, err
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

func readWorkspace(client internal.APSService, currentModel *Model) (*prometheusservice.WorkspaceStatus, error) {
	_, workspaceID, err := internal.ParseARN(*currentModel.Arn)
	if err != nil {
		return nil, err
	}
	data, err := client.DescribeWorkspace(&prometheusservice.DescribeWorkspaceInput{
		WorkspaceId: aws.String(workspaceID),
	})
	if err != nil {
		return nil, err
	}

	currentModel.WorkspaceId = &workspaceID
	currentModel.Arn = data.Workspace.Arn
	currentModel.PrometheusEndpoint = data.Workspace.PrometheusEndpoint
	currentModel.Alias = data.Workspace.Alias
	currentModel.Tags = stringMapToTags(data.Workspace.Tags)

	return data.Workspace.Status, nil
}

func readAlertManagerDefinition(
	client internal.APSService,
	currentModel *Model,
) (*prometheusservice.AlertManagerDefinitionStatus, error) {
	_, workspaceID, err := internal.ParseARN(*currentModel.Arn)
	if err != nil {
		return nil, err
	}

	data, err := client.DescribeAlertManagerDefinition(&prometheusservice.DescribeAlertManagerDefinitionInput{
		WorkspaceId: aws.String(workspaceID),
	})
	if err != nil {
		return nil, err
	}

	currentModel.AlertManagerDefinition = aws.String(string(data.AlertManagerDefinition.Data))
	return data.AlertManagerDefinition.Status, nil
}

func validateAlertManagerState(client internal.APSService, currentModel *Model, targetState string, successMessage string) (handler.ProgressEvent, error) {
	_, err := readWorkspace(client, currentModel)
	if err != nil {
		return handler.ProgressEvent{}, err
	}

	state, err := readAlertManagerDefinition(client, currentModel)
	if err != nil {
		return handler.ProgressEvent{
			ResourceModel:   currentModel,
			OperationStatus: handler.Failed,
			Message:         "AlertManagerDefinition was deleted out-of-band",
		}, err
	}

	if _, ok := alertManagerFailedStates[aws.StringValue(state.StatusCode)]; ok {
		return handler.ProgressEvent{
			ResourceModel:   currentModel,
			OperationStatus: handler.Failed,
			Message:         fmt.Sprintf("AlertManagerDefinition status: %s", aws.StringValue(state.StatusCode)),
		}, err
	}

	if aws.StringValue(state.StatusCode) != targetState {
		return handler.ProgressEvent{
			ResourceModel:        currentModel,
			OperationStatus:      handler.InProgress,
			Message:              messageInProgress,
			CallbackDelaySeconds: defaultCallbackSeconds,
			CallbackContext:      buildWaitForAlertManagerStatusCallbackContext(currentModel, waitForAlertManagerStatusActiveKey),
		}, nil
	}

	return handler.ProgressEvent{
		ResourceModel:   currentModel,
		OperationStatus: handler.Success,
		Message:         successMessage,
	}, nil
}

func validateWorkspaceState(client internal.APSService, currentModel *Model, targetState string, successMessage string) (handler.ProgressEvent, error) {
	state, err := readWorkspace(client, currentModel)
	if err != nil {
		return handler.ProgressEvent{}, err
	}

	if aws.StringValue(state.StatusCode) != targetState {
		return handler.ProgressEvent{
			ResourceModel: &Model{
				Arn: currentModel.Arn,
			},
			OperationStatus:      handler.InProgress,
			Message:              messageInProgress,
			CallbackDelaySeconds: defaultCallbackSeconds,
			CallbackContext:      buildWaitForWorkspaceStatusCallbackContext(currentModel),
		}, nil
	}

	return handler.ProgressEvent{
		ResourceModel:   currentModel,
		OperationStatus: handler.Success,
		Message:         successMessage,
	}, nil
}

func buildWaitForWorkspaceStatusCallbackContext(model *Model) map[string]interface{} {
	return map[string]interface{}{
		waitForWorkspaceStatusKey: aws.StringValue(model.Arn),
	}
}

func buildWaitForAlertManagerStatusCallbackContext(model *Model, key string) map[string]interface{} {
	return map[string]interface{}{
		key: aws.StringValue(model.Arn),
	}
}

func stringMapToTags(m map[string]*string) []Tag {
	res := []Tag{}
	for key, val := range m {
		res = append(res, Tag{
			Key:   aws.String(key),
			Value: val,
		})
	}
	return res
}

func tagsToStringMap(tags []Tag) map[string]*string {
	result := map[string]*string{}
	for _, tag := range tags {
		result[aws.StringValue(tag.Key)] = tag.Value
	}
	return result
}

func validateAlertManagerDeleted(client internal.APSService, currentModel *Model, successMessage string) (handler.ProgressEvent, error) {
	_, err := readWorkspace(client, currentModel)
	if err != nil {
		return handler.ProgressEvent{}, err
	}

	_, err = readAlertManagerDefinition(client, currentModel)
	if err == nil {
		return handler.ProgressEvent{
			ResourceModel:        currentModel,
			OperationStatus:      handler.InProgress,
			Message:              messageInProgress,
			CallbackDelaySeconds: defaultCallbackSeconds,
			CallbackContext:      buildWaitForAlertManagerStatusCallbackContext(currentModel, waitForAlertManagerStatusDeleteKey),
		}, nil
	}

	if awsErr, ok := err.(awserr.Error); ok {
		if awsErr.Code() == prometheusservice.ErrCodeResourceNotFoundException {
			return handler.ProgressEvent{
				OperationStatus: handler.Success,
				Message:         successMessage,
				ResourceModel:   currentModel,
			}, nil
		}
	}

	return handler.ProgressEvent{}, err
}
