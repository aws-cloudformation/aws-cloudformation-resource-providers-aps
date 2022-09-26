package resource

import (
	"errors"
	"fmt"
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/prometheusservice"
	"strings"
)

var (
	waitForLoggingConfigurationActiveKey = "waitForLoggingConfigurationActive"
	waitForLoggingConfigurationDeleteKey = "waitForLoggingConfigurationDeleted"
)

func CreateLoggingConfiguration(req handler.Request, client internal.APSService, prevModel, currentModel *Model) (bool, handler.ProgressEvent, error) {
	if arn, ok := req.CallbackContext[waitForLoggingConfigurationActiveKey]; ok {
		currentModel.Arn = aws.String(arn.(string))
		evt, err := validateLoggingConfigurationState(client, currentModel, prometheusservice.LoggingConfigurationStatusCodeActive, messageCreateComplete)
		return proceedOnSuccess(evt, err)
	}

	if currentModel.LoggingConfiguration != nil && currentModel.LoggingConfiguration.LogGroupArn != nil {
		evt, err := createLoggingConfiguration(client, currentModel)
		return proceedOnSuccess(evt, err)
	}
	return proceed(currentModel)
}

func createLoggingConfiguration(client internal.APSService, currentModel *Model) (handler.ProgressEvent, error) {
	loggingConfigurationOutput, err := client.CreateLoggingConfiguration(&prometheusservice.CreateLoggingConfigurationInput{
		WorkspaceId: currentModel.WorkspaceId,
		LogGroupArn: currentModel.LoggingConfiguration.LogGroupArn,
	})

	if err != nil {
		return internal.NewFailedEvent(err)
	}

	if *loggingConfigurationOutput.Status.StatusCode == prometheusservice.LoggingConfigurationStatusCodeCreationFailed {
		return internal.NewFailedEvent(errors.New(fmt.Sprintf("logging config creation failed due to %s", *loggingConfigurationOutput.Status.StatusReason)))
	}

	return handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              messageInProgress,
		ResourceModel:        currentModel,
		CallbackDelaySeconds: longCallbackSeconds,
		CallbackContext:      buildWaitForLoggingConfigurationStatusCallbackContext(currentModel, waitForLoggingConfigurationActiveKey),
	}, nil
}

func updateLoggingConfiguration(client internal.APSService, currentModel *Model) (handler.ProgressEvent, error) {
	loggingConfigurationOutput, err := client.UpdateLoggingConfiguration(&prometheusservice.UpdateLoggingConfigurationInput{
		LogGroupArn: currentModel.LoggingConfiguration.LogGroupArn,
		WorkspaceId: currentModel.WorkspaceId,
	})

	if err != nil {
		return internal.NewFailedEvent(err)
	}

	if *loggingConfigurationOutput.Status.StatusCode == prometheusservice.LoggingConfigurationStatusCodeUpdateFailed {
		return internal.NewFailedEvent(errors.New(fmt.Sprintf("logging config update failed due to %s", *loggingConfigurationOutput.Status.StatusReason)))
	}

	return handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              messageInProgress,
		ResourceModel:        currentModel,
		CallbackDelaySeconds: longCallbackSeconds,
		CallbackContext:      buildWaitForLoggingConfigurationStatusCallbackContext(currentModel, waitForLoggingConfigurationActiveKey),
	}, nil
}

func deleteLoggingConfiguration(client internal.APSService, currentModel *Model) (handler.ProgressEvent, error) {
	_, err := client.DeleteLoggingConfiguration(&prometheusservice.DeleteLoggingConfigurationInput{
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
		CallbackContext:      buildWaitForLoggingConfigurationStatusCallbackContext(currentModel, waitForLoggingConfigurationDeleteKey),
	}, nil
}

func UpdateLoggingConfiguration(req handler.Request, client internal.APSService, prevModel, currentModel *Model) (bool, handler.ProgressEvent, error) {
	if arn, ok := req.CallbackContext[waitForLoggingConfigurationActiveKey]; ok {
		currentModel.Arn = aws.String(arn.(string))
		evt, err := validateLoggingConfigurationState(client,
			currentModel,
			prometheusservice.LoggingConfigurationStatusCodeActive,
			messageUpdateComplete)
		return proceedOnSuccess(evt, err)
	}
	if arn, ok := req.CallbackContext[waitForLoggingConfigurationDeleteKey]; ok {
		currentModel.Arn = aws.String(arn.(string))
		evt, err := validateLoggingConfigurationState(client, currentModel, "", messageUpdateComplete)
		return validateLoggingConfigurationDeleted(evt, err, currentModel)
	}
	if loggingConfigurationChanged(prevModel, currentModel) {
		evt, err := manageLoggingConfiguration(currentModel, prevModel, client)
		return proceedOnSuccess(evt, err)
	}
	return proceed(currentModel)
}

func validateLoggingConfigurationDeleted(evt handler.ProgressEvent, err error, currentModel *Model) (bool, handler.ProgressEvent, error) {
	if awsErr, ok := err.(awserr.Error); ok {
		if awsErr.Code() == prometheusservice.ErrCodeResourceNotFoundException {
			return proceedOnSuccess(
				handler.ProgressEvent{
					OperationStatus: handler.Success,
					Message:         messageUpdateComplete,
					ResourceModel:   currentModel,
				}, nil)
		}
	}
	return proceedOnSuccess(evt, err)
}

func logGroupARN(model *Model) string {
	if model.LoggingConfiguration != nil {
		return strings.TrimSpace(aws.StringValue(model.LoggingConfiguration.LogGroupArn))
	}
	return ""
}

func manageLoggingConfiguration(currentModel *Model, prevModel *Model, client internal.APSService) (handler.ProgressEvent, error) {
	currentLogGroup := logGroupARN(currentModel)
	prevLogGroup := logGroupARN(prevModel)

	shouldCreateLoggingConfiguration := currentLogGroup != "" && prevLogGroup == ""

	shouldDeleteLoggingConfiguration := currentLogGroup == "" && prevLogGroup != ""

	if shouldCreateLoggingConfiguration {
		return createLoggingConfiguration(client, currentModel)
	}
	if shouldDeleteLoggingConfiguration {
		return deleteLoggingConfiguration(client, currentModel)
	}

	return updateLoggingConfiguration(client, currentModel)
}

func loggingConfigurationChanged(prevModel, currentModel *Model) bool {

	prevLogGroup := logGroupARN(prevModel)
	currentLogGroup := logGroupARN(currentModel)

	return prevLogGroup != currentLogGroup
}

func readLoggingConfiguration(client internal.APSService, currentModel *Model) (*prometheusservice.DescribeLoggingConfigurationOutput, error) {
	_, workspaceID, err := internal.ParseARN(*currentModel.Arn)
	if err != nil {
		return nil, err
	}
	loggingConfigurationOutput, err := client.DescribeLoggingConfiguration(&prometheusservice.DescribeLoggingConfigurationInput{
		WorkspaceId: aws.String(workspaceID),
	})

	if err != nil {
		return nil, err
	}
	currentModel.LoggingConfiguration = &LoggingConfiguration{
		LogGroupArn: loggingConfigurationOutput.LoggingConfiguration.LogGroupArn,
	}
	return loggingConfigurationOutput, nil
}

func validateLoggingConfigurationState(client internal.APSService, currentModel *Model, targetState string, successMessage string) (handler.ProgressEvent, error) {
	_, err := readWorkspace(client, currentModel)
	if err != nil {
		return handler.ProgressEvent{}, awserr.New(ErrCodeWorkspaceNotFoundException, "Workspace not found", err)
	}
	loggingConfigurationOutput, err := readLoggingConfiguration(client, currentModel)
	if err != nil {
		return handler.ProgressEvent{
			OperationStatus: handler.Failed,
			ResourceModel:   currentModel,
			Message:         err.Error(),
		}, err
	}

	status := aws.StringValue(loggingConfigurationOutput.LoggingConfiguration.Status.StatusCode)
	switch status {
	case targetState:
		return handler.ProgressEvent{
			OperationStatus: handler.Success,
			ResourceModel:   currentModel,
			Message:         successMessage,
		}, nil
	case prometheusservice.LoggingConfigurationStatusCodeCreating, prometheusservice.LoggingConfigurationStatusCodeUpdating, prometheusservice.LoggingConfigurationStatusCodeDeleting:
		return handler.ProgressEvent{
			OperationStatus:      handler.InProgress,
			ResourceModel:        currentModel,
			Message:              messageInProgress,
			CallbackDelaySeconds: longCallbackSeconds,
		}, nil
	case prometheusservice.LoggingConfigurationStatusCodeCreationFailed, prometheusservice.LoggingConfigurationStatusCodeUpdateFailed:
		return handler.ProgressEvent{
			OperationStatus: handler.Failed,
			ResourceModel:   currentModel,
			Message:         fmt.Sprintf("Logging configuration status %s", aws.StringValue(loggingConfigurationOutput.LoggingConfiguration.Status.StatusReason)),
		}, nil
	}

	return handler.ProgressEvent{}, nil
}

func buildWaitForLoggingConfigurationStatusCallbackContext(model *Model, key string) map[string]interface{} {
	return map[string]interface{}{
		key: aws.StringValue(model.Arn),
	}
}
