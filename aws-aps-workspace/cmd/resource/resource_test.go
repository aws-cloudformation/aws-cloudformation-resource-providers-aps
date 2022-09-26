package resource

import (
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/prometheusservice"
	"testing"
	"time"
)

type FakePrometheusService struct {
	internal.APSService
	createWorkspaceFunc   func(input *prometheusservice.CreateWorkspaceInput) (*prometheusservice.CreateWorkspaceOutput, error)
	describeWorkspaceFunc func(input *prometheusservice.DescribeWorkspaceInput) (*prometheusservice.DescribeWorkspaceOutput, error)
}

func (f *FakePrometheusService) CreateWorkspace(input *prometheusservice.CreateWorkspaceInput) (*prometheusservice.CreateWorkspaceOutput, error) {
	return f.createWorkspaceFunc(input)
}

func (f *FakePrometheusService) DescribeWorkspace(input *prometheusservice.DescribeWorkspaceInput) (*prometheusservice.DescribeWorkspaceOutput, error) {
	return f.describeWorkspaceFunc(input)
}

func workspaceCreateInProgress(input *prometheusservice.CreateWorkspaceInput) (*prometheusservice.CreateWorkspaceOutput, error) {
	return &prometheusservice.CreateWorkspaceOutput{
		Arn: aws.String("arn:aws:aps:us-west-2:123456789:workspace/ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
		Status: &prometheusservice.WorkspaceStatus{
			StatusCode: aws.String(prometheusservice.WorkspaceStatusCodeCreating),
		},
		Tags:        input.Tags,
		WorkspaceId: aws.String("ws-55c7e22b-094a-4109-ab5e-7456421d30b1"),
	}, nil
}

func workspaceDescriptionActive(input *prometheusservice.DescribeWorkspaceInput) (*prometheusservice.DescribeWorkspaceOutput, error) {
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

func Test_SimpleWorkspaceCreate(t *testing.T) {
	testCases := map[string]struct {
		client          internal.APSService
		status          handler.Status
		req             handler.Request
		prevModel       *Model
		currentModel    *Model
		callbackContext map[string]interface{}
	}{
		"Should return in progress": {
			client: &FakePrometheusService{
				createWorkspaceFunc: workspaceCreateInProgress,
			},
			status: handler.InProgress,
			req: handler.Request{
				LogicalResourceID: "1234",
				CallbackContext:   nil,
				RequestContext: handler.RequestContext{
					SystemTags: map[string]string{
						"aws:cloudformation:stack-name": "Test",
						"aws:cloudformation:logical-id": "1234",
						"aws:cloudformation:stack-id":   "arn:aws:cloudformation:us-west-2:123456789:stack/Test/fa4bdfd0-0498-11ed-a87d-06c24a5b766d",
					},
				},
			},
			prevModel: &Model{},
			currentModel: &Model{
				Alias: aws.String("Test"),
			},
			callbackContext: map[string]interface{}{
				waitForWorkspaceStatusKey: "arn:aws:aps:us-west-2:123456789:workspace/ws-55c7e22b-094a-4109-ab5e-7456421d30b1",
			},
		},
		"Should return success": {
			client: &FakePrometheusService{
				describeWorkspaceFunc: workspaceDescriptionActive,
			},
			status: handler.Success,
			req: handler.Request{
				LogicalResourceID: "1234",
				CallbackContext: map[string]interface{}{
					waitForWorkspaceStatusKey: "arn:aws:aps:us-west-2:123456789012:workspace/ws-55c7e22b-094a-4109-ab5e-7456421d30b1",
				},
				RequestContext: handler.RequestContext{
					SystemTags: map[string]string{
						"aws:cloudformation:stack-name": "Test",
						"aws:cloudformation:logical-id": "1234",
						"aws:cloudformation:stack-id":   "arn:aws:cloudformation:us-west-2:123456789012:stack/Test/fa4bdfd0-0498-11ed-a87d-06c24a5b766d",
					},
				},
			},
			prevModel: &Model{},
			currentModel: &Model{
				Alias: aws.String("Test"),
			},
			callbackContext: map[string]interface{}{},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			_, evt, err := CreateWorkspace(tc.req, tc.client, tc.prevModel, tc.currentModel)
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
