package resource

import (
	"fmt"
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/prometheusservice"
	"strings"
)

var (
	alertManagerFailedStates = map[string]struct{}{
		prometheusservice.AlertManagerDefinitionStatusCodeCreationFailed: {},
		prometheusservice.AlertManagerDefinitionStatusCodeUpdateFailed:   {},
	}

	waitForAlertManagerStatusActiveKey = "waitForAlertManagerActive"
	waitForAlertManagerStatusDeleteKey = "waitForAlertManagerDeleted"
)

func CreateAlertManager(req handler.Request, client internal.APSService, prevModel, currentModel *Model) (bool, handler.ProgressEvent, error) {
	if arn, ok := req.CallbackContext[waitForAlertManagerStatusActiveKey]; ok {
		currentModel.Arn = aws.String(arn.(string))
		evt, err := validateAlertManagerState(client, currentModel, prometheusservice.AlertManagerDefinitionStatusCodeActive, messageCreateComplete)
		return proceedOnSuccess(evt, err)
	}
	if currentModel.AlertManagerDefinition != nil {
		evt, err := createAlertManagerDefinition(client, currentModel)
		return proceedOnSuccess(evt, err)
	}
	return proceed(currentModel)
}

func createAlertManagerDefinition(client internal.APSService, currentModel *Model) (handler.ProgressEvent, error) {
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
		CallbackDelaySeconds: longCallbackSeconds,
		CallbackContext:      buildWaitForAlertManagerStatusCallbackContext(currentModel, waitForAlertManagerStatusActiveKey),
	}, nil
}

func alertManagerChanged(prevModel, currentModel *Model) bool {
	return internal.StringDiffers(currentModel.AlertManagerDefinition, prevModel.AlertManagerDefinition)
}

func UpdateAlertManager(req handler.Request, client internal.APSService, prevModel, currentModel *Model) (bool, handler.ProgressEvent, error) {
	if arn, ok := req.CallbackContext[waitForAlertManagerStatusActiveKey]; ok {
		currentModel.Arn = aws.String(arn.(string))
		evt, err := validateAlertManagerState(client,
			currentModel,
			prometheusservice.AlertManagerDefinitionStatusCodeActive,
			messageUpdateComplete)
		return proceedOnSuccess(evt, err)
	}

	if arn, ok := req.CallbackContext[waitForAlertManagerStatusDeleteKey]; ok {
		currentModel.Arn = aws.String(arn.(string))
		evt, err := validateAlertManagerDeleted(client,
			currentModel,
			messageUpdateComplete)
		return proceedOnSuccess(evt, err)
	}
	if alertManagerChanged(prevModel, currentModel) {
		evt, err := manageAlertManagerDefinition(currentModel, prevModel, client)
		return proceedOnSuccess(evt, err)
	}
	return proceed(currentModel)
}

func manageAlertManagerDefinition(currentModel *Model, prevModel *Model, client internal.APSService) (handler.ProgressEvent, error) {
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
		CallbackDelaySeconds: longCallbackSeconds,
		CallbackContext:      buildWaitForAlertManagerStatusCallbackContext(currentModel, key),
	}, nil
}

func readAlertManagerDefinition(client internal.APSService, currentModel *Model) (*prometheusservice.AlertManagerDefinitionStatus, error) {
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
		return handler.ProgressEvent{}, awserr.New(ErrCodeWorkspaceNotFoundException, "Workspace not found", err)
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
			Message:         fmt.Sprintf("AlertManagerDefinition status: %s. Reason: %s", aws.StringValue(state.StatusCode), aws.StringValue(state.StatusReason)),
		}, err
	}

	if aws.StringValue(state.StatusCode) != targetState {
		return handler.ProgressEvent{
			ResourceModel:        currentModel,
			OperationStatus:      handler.InProgress,
			Message:              messageInProgress,
			CallbackDelaySeconds: longCallbackSeconds,
			CallbackContext:      buildWaitForAlertManagerStatusCallbackContext(currentModel, waitForAlertManagerStatusActiveKey),
		}, nil
	}

	return handler.ProgressEvent{
		ResourceModel:   currentModel,
		OperationStatus: handler.Success,
		Message:         successMessage,
	}, nil
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
			CallbackDelaySeconds: longCallbackSeconds,
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

func buildWaitForAlertManagerStatusCallbackContext(model *Model, key string) map[string]interface{} {
	return map[string]interface{}{
		key: aws.StringValue(model.Arn),
	}
}
