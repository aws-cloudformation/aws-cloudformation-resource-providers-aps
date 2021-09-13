package resource

import (
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/prometheusservice"
)

const defaultCallbackSeconds = 2

// Create handles the Create event from the Cloudformation service.
func Create(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	if currentModel.WorkspaceId != nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Create: cannot create a resource using ReadOnly properties",
			HandlerErrorCode: cloudformation.HandlerErrorCodeInvalidRequest,
		}, nil
	}

	client := internal.NewAMP(req.Session)
	if _, ok := req.CallbackContext["Arn"]; ok {
		currentModel.Arn = aws.String(req.CallbackContext["Arn"].(string))
		return validateWorkspaceState(
			client,
			currentModel,
			prometheusservice.WorkspaceStatusCodeActive,
			"Create Completed")
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
		Message:              "In Progress",
		ResourceModel:        currentModel,
		CallbackDelaySeconds: defaultCallbackSeconds,
		CallbackContext:      buildCallbackContext(currentModel),
	}, nil
}

// Read handles the Read event from the Cloudformation service.
func Read(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	// contract test: contract_read_without_create
	if currentModel.Arn == nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Read: Arn cannot be empty",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
		}, nil
	}

	client := internal.NewAMP(req.Session)
	if _, err := readWorkspace(client, currentModel); err != nil {
		return internal.NewFailedEvent(err)
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
			Message:          "Invalid Update: Arn cannot be empty",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
		}, nil
	}

	client := internal.NewAMP(req.Session)
	if _, ok := req.CallbackContext["Arn"]; ok {
		currentModel.Arn = aws.String(req.CallbackContext["Arn"].(string))
		return validateWorkspaceState(
			client,
			currentModel,
			prometheusservice.WorkspaceStatusCodeActive,
			"Update Complete")
	}

	_, workspaceID, err := internal.ParseARN(*currentModel.Arn)
	if err != nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Read: invalid ARN format",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
		}, nil
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
		Message:              "In Progress",
		ResourceModel:        currentModel,
		CallbackDelaySeconds: defaultCallbackSeconds,
		CallbackContext:      buildCallbackContext(currentModel),
	}, nil
}

// Delete handles the Delete event from the Cloudformation service.
func Delete(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	if currentModel.Arn == nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Delete: Arn cannot be empty",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
		}, nil
	}

	client := internal.NewAMP(req.Session)
	if _, ok := req.CallbackContext["Arn"]; ok {
		currentModel.Arn = aws.String(req.CallbackContext["Arn"].(string))
		return validateWorkspaceDeleted(
			client,
			currentModel,
			"Delete Complete")
	}

	_, workspaceID, err := internal.ParseARN(*currentModel.Arn)
	if err != nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Read: invalid ARN format",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
		}, nil
	}

	_, err = client.
		DeleteWorkspace(&prometheusservice.DeleteWorkspaceInput{
			WorkspaceId: aws.String(workspaceID),
		})
	if err != nil {
		return internal.NewFailedEvent(err)
	}

	return handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              "In Progress",
		ResourceModel:        currentModel,
		CallbackDelaySeconds: defaultCallbackSeconds,
		CallbackContext:      buildCallbackContext(currentModel),
	}, nil
}

func validateWorkspaceDeleted(client *prometheusservice.PrometheusService, currentModel *Model, successMessage string) (handler.ProgressEvent, error) {
	_, err := readWorkspace(client, currentModel)
	if err == nil {
		return handler.ProgressEvent{
			ResourceModel:        currentModel,
			OperationStatus:      handler.InProgress,
			Message:              "In Progress",
			CallbackDelaySeconds: defaultCallbackSeconds,
			CallbackContext:      buildCallbackContext(currentModel),
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

	resp, err := internal.NewAMP(req.Session).ListWorkspaces(&prometheusservice.ListWorkspacesInput{
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

func readWorkspace(client *prometheusservice.PrometheusService, currentModel *Model) (*prometheusservice.WorkspaceStatus, error) {
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

func validateWorkspaceState(client *prometheusservice.PrometheusService, currentModel *Model, targetState string, successMessage string) (handler.ProgressEvent, error) {
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
			Message:              "In Progress",
			CallbackDelaySeconds: defaultCallbackSeconds,
			CallbackContext:      buildCallbackContext(currentModel),
		}, nil
	}

	return handler.ProgressEvent{
		ResourceModel:   currentModel,
		OperationStatus: handler.Success,
		Message:         successMessage,
	}, nil
}

func buildCallbackContext(model *Model) map[string]interface{} {
	return map[string]interface{}{
		"Arn": aws.StringValue(model.Arn),
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
