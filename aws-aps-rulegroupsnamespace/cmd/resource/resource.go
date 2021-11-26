package resource

import (
	"errors"
	"fmt"
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/prometheusservice"
	"strings"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
)

const defaultCallbackSeconds = 2

// Create handles the Create event from the Cloudformation service.
func Create(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	client := internal.NewAPS(req.Session)
	if _, ok := req.CallbackContext["Arn"]; ok {
		currentModel.Arn = aws.String(req.CallbackContext["Arn"].(string))
		return validateRuleGroupsNamespaceState(
			client,
			currentModel,
			prometheusservice.RuleGroupsNamespaceStatusCodeActive,
			"Create Completed")
	}

	if currentModel.Workspace == nil {
		return internal.NewFailedEvent(errors.New("Missing Workspace ARN"))
	}
	if currentModel.Data == nil {
		return internal.NewFailedEvent(errors.New("Missing RuleGroupsNamespace Data"))
	}
	if currentModel.Name == nil {
		return internal.NewFailedEvent(errors.New("Missing RuleGroupsNamespace Name"))
	}

	_, workspaceID, err := internal.ParseARN(*currentModel.Workspace)
	if err != nil {
		return internal.NewFailedEvent(err)
	}
	resp, err := client.CreateRuleGroupsNamespace(&prometheusservice.CreateRuleGroupsNamespaceInput{
		WorkspaceId: aws.String(workspaceID),
		Name:        currentModel.Name,
		Data:        []byte(*currentModel.Data),
		Tags:        tagsToStringMap(currentModel.Tags),
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
			ResourceModel:    currentModel,
		}, nil
	}

	client := internal.NewAPS(req.Session)
	if _, err := readRuleGroupsNamespaceDefinition(client, currentModel); err != nil {
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
			ResourceModel:    currentModel,
		}, nil
	}

	client := internal.NewAPS(req.Session)
	if _, ok := req.CallbackContext["Arn"]; ok {
		currentModel.Arn = aws.String(req.CallbackContext["Arn"].(string))
		return validateRuleGroupsNamespaceState(
			client,
			currentModel,
			prometheusservice.RuleGroupsNamespaceStatusCodeActive,
			"Update Complete")
	}

	_, workspaceID, err := internal.ParseARN(*currentModel.Arn)
	if err != nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Read: invalid ARN format",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
			ResourceModel:    currentModel,
		}, nil
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

	_, err = client.
		PutRuleGroupsNamespace(&prometheusservice.PutRuleGroupsNamespaceInput{
			WorkspaceId: aws.String(workspaceID),
			Name:        currentModel.Name,
			Data:        []byte(*currentModel.Data),
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

// Delete handles the Delete event from the Cloudformation service.
func Delete(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	if currentModel.Arn == nil {
		return handler.ProgressEvent{
			OperationStatus:  handler.Failed,
			Message:          "Invalid Delete: Arn cannot be empty",
			HandlerErrorCode: cloudformation.HandlerErrorCodeNotFound,
			ResourceModel:    currentModel,
		}, nil
	}

	client := internal.NewAPS(req.Session)
	if _, ok := req.CallbackContext["Arn"]; ok {
		currentModel.Arn = aws.String(req.CallbackContext["Arn"].(string))
		return validateRuleGroupsNamespaceDeleted(
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
			ResourceModel:    currentModel,
		}, nil
	}

	_, err = client.
		DeleteRuleGroupsNamespace(&prometheusservice.DeleteRuleGroupsNamespaceInput{
			WorkspaceId: aws.String(workspaceID),
			Name:        currentModel.Name,
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

// List handles the List event from the Cloudformation service.
func List(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	return handler.ProgressEvent{}, errors.New("Not implemented: List")
}

func readRuleGroupsNamespaceDefinition(
	client *prometheusservice.PrometheusService,
	currentModel *Model,
) (*prometheusservice.RuleGroupsNamespaceStatus, error) {
	arn, workspaceID, err := internal.ParseARN(*currentModel.Arn)
	if err != nil {
		return nil, err
	}

	resourceParts := strings.Split(arn.Resource, "/")
	currentModel.Name = aws.String(resourceParts[len(resourceParts)-1])

	data, err := client.DescribeRuleGroupsNamespace(&prometheusservice.DescribeRuleGroupsNamespaceInput{
		WorkspaceId: aws.String(workspaceID),
		Name:        currentModel.Name,
	})
	if err != nil {
		return nil, err
	}

	arn.Resource = fmt.Sprintf("workspace/%s", workspaceID)
	currentModel.Workspace = aws.String(arn.String())
	currentModel.Data = aws.String(string(data.RuleGroupsNamespace.Data))
	currentModel.Tags = stringMapToTags(data.RuleGroupsNamespace.Tags)
	return data.RuleGroupsNamespace.Status, nil
}

func validateRuleGroupsNamespaceDeleted(client *prometheusservice.PrometheusService, currentModel *Model, successMessage string) (handler.ProgressEvent, error) {
	_, err := readRuleGroupsNamespaceDefinition(client, currentModel)
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

func validateRuleGroupsNamespaceState(client *prometheusservice.PrometheusService, currentModel *Model, targetState string, successMessage string) (handler.ProgressEvent, error) {
	state, err := readRuleGroupsNamespaceDefinition(client, currentModel)
	if err != nil {
		return handler.ProgressEvent{}, err
	}

	if aws.StringValue(state.StatusCode) != targetState {
		return handler.ProgressEvent{
			ResourceModel:        currentModel,
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
