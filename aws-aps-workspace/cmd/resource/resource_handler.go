package resource

import (
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
)

type AMPResourceHandler func(req handler.Request, client internal.APSService, prevModel, currentModel *Model) (bool, handler.ProgressEvent, error)

func proceed(currentModel *Model) (bool, handler.ProgressEvent, error) {
	return proceedOnSuccess(handler.ProgressEvent{
		OperationStatus: handler.Success,
		ResourceModel:   currentModel,
	}, nil)
}

func proceedOnSuccess(evt handler.ProgressEvent, err error) (bool, handler.ProgressEvent, error) {
	if err != nil {
		return false, evt, err
	}
	proceedToNextStep := evt.OperationStatus == handler.Success
	return proceedToNextStep, evt, err
}
