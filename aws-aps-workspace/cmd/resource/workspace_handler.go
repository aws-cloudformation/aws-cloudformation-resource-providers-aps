package resource

import (
	"fmt"
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/prometheusservice"
)

var (
	waitForWorkspaceStatusKey = "Arn" // for backwards compatibility during release
)

func CreateWorkspace(req handler.Request, client internal.APSService, prevModel, currentModel *Model) (bool, handler.ProgressEvent, error) {
	if arn, ok := req.CallbackContext[waitForWorkspaceStatusKey]; ok {
		currentModel.Arn = aws.String(arn.(string))
		evt, err := validateWorkspaceState(
			client,
			currentModel,
			prometheusservice.WorkspaceStatusCodeActive,
			messageCreateComplete)
		return proceedOnSuccess(evt, err)
	}
	if len(req.CallbackContext) == 0 {
		// create new workspace
		systemTags := internal.ToAWSStringMap(req.RequestContext.SystemTags)
		tags := tagsToStringMap(currentModel.Tags)
		internal.MergeMaps(systemTags, tags)
		resp, err := client.CreateWorkspace(&prometheusservice.CreateWorkspaceInput{
			Alias: currentModel.Alias,
			Tags:  tags,
		})
		if err != nil {
			return proceedOnSuccess(internal.NewFailedEvent(err))
		}
		currentModel.Arn = resp.Arn
		return proceedOnSuccess(handler.ProgressEvent{
			OperationStatus:      handler.InProgress,
			Message:              messageInProgress,
			ResourceModel:        currentModel,
			CallbackDelaySeconds: shortCallbackSeconds,
			CallbackContext:      buildWaitForWorkspaceStatusCallbackContext(currentModel),
		}, nil)
	}
	return proceed(currentModel)
}

func UpdateWorkspace(req handler.Request, client internal.APSService, prevModel, currentModel *Model) (bool, handler.ProgressEvent, error) {
	if arn, ok := req.CallbackContext[waitForWorkspaceStatusKey]; ok {
		currentModel.Arn = aws.String(arn.(string))
		evt, err := validateWorkspaceState(
			client,
			currentModel,
			prometheusservice.WorkspaceStatusCodeActive,
			messageUpdateComplete)
		if err != nil {
			return proceedOnSuccess(internal.NewFailedEvent(err))
		}
		return proceedOnSuccess(evt, err)
	}
	if internal.StringDiffers(currentModel.Alias, prevModel.Alias) {
		_, err := client.UpdateWorkspaceAlias(&prometheusservice.UpdateWorkspaceAliasInput{
			WorkspaceId: currentModel.WorkspaceId,
			Alias:       currentModel.Alias,
		})

		if err != nil {
			return proceedOnSuccess(internal.NewFailedEvent(err))
		}
	}

	return updateTags(req, client, prevModel, currentModel)
}

func updateTags(req handler.Request, client internal.APSService, prevModel, currentModel *Model) (bool, handler.ProgressEvent, error) {
	currentModelTags := tagsToStringMap(currentModel.Tags)
	systemTags := internal.ToAWSStringMap(req.RequestContext.SystemTags)
	internal.MergeMaps(systemTags, currentModelTags)
	toAdd, toRemove := internal.StringMapDifference(currentModelTags, tagsToStringMap(prevModel.Tags))
	if len(toRemove) > 0 {
		_, err := client.UntagResource(&prometheusservice.UntagResourceInput{
			ResourceArn: currentModel.Arn,
			TagKeys:     toRemove,
		})
		if err != nil {
			return proceedOnSuccess(internal.NewFailedEvent(err))
		}
	}

	if len(toAdd) > 0 {
		_, err := client.TagResource(&prometheusservice.TagResourceInput{
			ResourceArn: currentModel.Arn,
			Tags:        toAdd,
		})
		if err != nil {
			return proceedOnSuccess(internal.NewFailedEvent(err))
		}
	}

	return proceedOnSuccess(handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              messageInProgress,
		ResourceModel:        currentModel,
		CallbackDelaySeconds: shortCallbackSeconds,
		CallbackContext:      buildWaitForWorkspaceStatusCallbackContext(currentModel),
	}, nil)
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

	// no need to delete AlertManagerDefinition or logging config here, because APSService deletes this when the workspace is deleted
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
		CallbackDelaySeconds: longCallbackSeconds,
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
			CallbackDelaySeconds: longCallbackSeconds,
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

func validateWorkspaceState(client internal.APSService, currentModel *Model, targetState string, successMessage string) (handler.ProgressEvent, error) {
	state, err := readWorkspace(client, currentModel)
	if err != nil {
		return handler.ProgressEvent{
			OperationStatus: handler.Failed,
			ResourceModel:   currentModel,
			Message:         err.Error(),
		}, err
	}

	statusCode := aws.StringValue(state.StatusCode)
	switch statusCode {
	case targetState:
		return handler.ProgressEvent{
			ResourceModel:   currentModel,
			OperationStatus: handler.Success,
			Message:         successMessage,
		}, nil
	case prometheusservice.WorkspaceStatusCodeCreating, prometheusservice.WorkspaceStatusCodeUpdating:
		return handler.ProgressEvent{
			ResourceModel: &Model{
				Arn: currentModel.Arn,
			},
			OperationStatus:      handler.InProgress,
			Message:              messageInProgress,
			CallbackDelaySeconds: shortCallbackSeconds,
			CallbackContext:      buildWaitForWorkspaceStatusCallbackContext(currentModel),
		}, nil
	case prometheusservice.WorkspaceStatusCodeCreationFailed:
		return handler.ProgressEvent{
			OperationStatus: handler.Failed,
			ResourceModel:   currentModel,
			Message:         fmt.Sprintf("Workspace status: %s", aws.StringValue(state.StatusCode)),
		}, nil
	}

	return handler.ProgressEvent{}, nil
}

func buildWaitForWorkspaceStatusCallbackContext(model *Model) map[string]interface{} {
	return map[string]interface{}{
		waitForWorkspaceStatusKey: aws.StringValue(model.Arn),
	}
}
