package resource

import (
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/prometheusservice"
	"testing"
	"time"
)

type FakePrometheusServiceLoggingConfig struct {
	internal.APSService
	createLoggingConfigFunc   func(input *prometheusservice.CreateLoggingConfigurationInput) (*prometheusservice.CreateLoggingConfigurationOutput, error)
	describeWorkspaceFunc     func(input *prometheusservice.DescribeWorkspaceInput) (*prometheusservice.DescribeWorkspaceOutput, error)
	describeLoggingConfigFunc func(input *prometheusservice.DescribeLoggingConfigurationInput) (*prometheusservice.DescribeLoggingConfigurationOutput, error)
}

func (f *FakePrometheusServiceLoggingConfig) CreateLoggingConfiguration(input *prometheusservice.CreateLoggingConfigurationInput) (*prometheusservice.CreateLoggingConfigurationOutput, error) {
	return f.createLoggingConfigFunc(input)
}

func (f *FakePrometheusServiceLoggingConfig) DescribeWorkspace(input *prometheusservice.DescribeWorkspaceInput) (*prometheusservice.DescribeWorkspaceOutput, error) {
	return f.describeWorkspaceFunc(input)
}

func (f *FakePrometheusServiceLoggingConfig) DescribeLoggingConfiguration(input *prometheusservice.DescribeLoggingConfigurationInput) (*prometheusservice.DescribeLoggingConfigurationOutput, error) {
	return f.describeLoggingConfigFunc(input)
}

func loggingConfigInProgress(input *prometheusservice.CreateLoggingConfigurationInput) (*prometheusservice.CreateLoggingConfigurationOutput, error) {
	return &prometheusservice.CreateLoggingConfigurationOutput{
		Status: &prometheusservice.LoggingConfigurationStatus{
			StatusCode: aws.String(prometheusservice.LoggingConfigurationStatusCodeCreating),
		},
	}, nil
}

func loggingConfigWorkspaceDescriptionActive(input *prometheusservice.DescribeWorkspaceInput) (*prometheusservice.DescribeWorkspaceOutput, error) {
	return &prometheusservice.DescribeWorkspaceOutput{
		Workspace: &prometheusservice.WorkspaceDescription{
			Alias:              aws.String("Test"),
			Arn:                aws.String("arn:aws:aps:us-west-2:123456789:workspace/ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
			CreatedAt:          aws.Time(time.Now()),
			PrometheusEndpoint: aws.String("https://testendpoint"),
			Status: &prometheusservice.WorkspaceStatus{
				StatusCode: aws.String(prometheusservice.WorkspaceStatusCodeActive),
			},
			Tags: map[string]*string{
				"aws:cloudformation:stack-name": aws.String("Test"),
				"aws:cloudformation:logical-id": aws.String("1234"),
				"aws:cloudformation:stack-id":   aws.String("arn:aws:cloudformation:us-west-2:123456789:stack/Test/fa4bdfd0-0498-11ed-a87d-06c24a5b766d"),
			},
			WorkspaceId: aws.String("ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
		},
	}, nil
}

func loggingConfigActive(input *prometheusservice.DescribeLoggingConfigurationInput) (*prometheusservice.DescribeLoggingConfigurationOutput, error) {
	return &prometheusservice.DescribeLoggingConfigurationOutput{
		LoggingConfiguration: &prometheusservice.LoggingConfigurationMetadata{
			LogGroupArn: aws.String("arn:aws:logs:us-west-2:123456789012:log-group:test-loggroup:*"),
			Status: &prometheusservice.LoggingConfigurationStatus{
				StatusCode: aws.String(prometheusservice.LoggingConfigurationStatusCodeActive),
			},
			Workspace: aws.String("ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
		},
	}, nil
}

func Test_CreateLoggingConfiguration(t *testing.T) {
	testCases := map[string]struct {
		client          internal.APSService
		status          handler.Status
		req             handler.Request
		prevModel       *Model
		currentModel    *Model
		callbackContext map[string]interface{}
	}{
		"Should return in progress": {
			client: &FakePrometheusServiceLoggingConfig{
				createLoggingConfigFunc: loggingConfigInProgress,
			},
			status: handler.InProgress,
			req: handler.Request{
				CallbackContext: map[string]interface{}{},
			},
			prevModel: &Model{},
			currentModel: &Model{
				Arn:         aws.String("arn:aws:aps:us-west-2:123456789012:workspace/ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
				WorkspaceId: aws.String("ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
				LoggingConfiguration: &LoggingConfiguration{
					LogGroupArn: aws.String("arn:aws:logs:us-west-2:123456789012:log-group:test-loggroup:*"),
				},
			},
			callbackContext: map[string]interface{}{
				waitForLoggingConfigurationActiveKey: "arn:aws:aps:us-west-2:123456789012:workspace/ws-55c7e22b-094a-4109-ab5e-7456421d30b1",
			},
		},
		"Should return success": {
			client: &FakePrometheusServiceLoggingConfig{
				describeWorkspaceFunc:     loggingConfigWorkspaceDescriptionActive,
				describeLoggingConfigFunc: loggingConfigActive,
			},
			status: handler.Success,
			req: handler.Request{
				CallbackContext: map[string]interface{}{
					waitForLoggingConfigurationActiveKey: "arn:aws:aps:us-west-2:123456789:workspace/ws-55c7e22b-094a-4109-ab5e-7456421d30b1",
				},
			},
			prevModel: &Model{
				Arn:         aws.String("arn:aws:aps:us-west-2:123456789012:workspace/ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
				WorkspaceId: aws.String("ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
				LoggingConfiguration: &LoggingConfiguration{
					LogGroupArn: aws.String("arn:aws:logs:us-west-2:123456789012:log-group:test-loggroup:*"),
				},
			},
			currentModel: &Model{
				Arn:         aws.String("arn:aws:aps:us-west-2:123456789012:workspace/ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
				WorkspaceId: aws.String("ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
				LoggingConfiguration: &LoggingConfiguration{
					LogGroupArn: aws.String("arn:aws:logs:us-west-2:123456789012:log-group:test-loggroup:*"),
				},
			},
			callbackContext: map[string]interface{}{},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			_, evt, err := CreateLoggingConfiguration(tc.req, tc.client, tc.prevModel, tc.currentModel)
			if err != nil {
				t.Fatalf("Failed %v", err)
			}
			if evt.OperationStatus != tc.status {
				t.Fatalf("Unexpected status %s and should be %s", evt.OperationStatus, tc.status)
			}
			if (evt.CallbackContext == nil || len(evt.CallbackContext) == 0) && (tc.callbackContext != nil && len(tc.callbackContext) > 0) {
				t.Fatalf("Missing callback context. Expected %+v", tc.callbackContext)
			}
			for k, v := range tc.callbackContext {
				if evt.CallbackContext[k] != v {
					t.Fatalf("Expected key, value = [%s,%s]. Got [%s]", k, v, evt.CallbackContext[k])
				}
			}
		})
	}
}
