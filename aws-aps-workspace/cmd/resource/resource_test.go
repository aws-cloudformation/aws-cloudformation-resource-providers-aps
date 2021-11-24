package resource

import (
	"github.com/aws-cloudformation/aws-cloudformation-resource-providers-aps/internal"
	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/prometheusservice"
	"testing"
)

type MockPrometheusService struct {
	internal.APSService
	status *prometheusservice.AlertManagerDefinitionStatus
}

func (c *MockPrometheusService) DescribeWorkspace(*prometheusservice.DescribeWorkspaceInput) (*prometheusservice.DescribeWorkspaceOutput, error) {
	return &prometheusservice.DescribeWorkspaceOutput{
		Workspace: &prometheusservice.WorkspaceDescription{
			Arn: aws.String("arn:aws:aps:us-west-2:933102010132:workspace/ws-5f41d54a-41fc-4783-984f-7facb35c928c"),
		},
	}, nil
}

func (c *MockPrometheusService) DescribeAlertManagerDefinition(*prometheusservice.DescribeAlertManagerDefinitionInput) (*prometheusservice.DescribeAlertManagerDefinitionOutput, error) {
	return &prometheusservice.DescribeAlertManagerDefinitionOutput{
		AlertManagerDefinition: &prometheusservice.AlertManagerDefinitionDescription{
			Status: c.status,
		},
	}, nil
}

func Test_validateAlertManagerState(t *testing.T) {
	testCases := map[string]struct {
		client        internal.APSService
		status        handler.Status
		targetState   string
		targetMessage string
	}{
		"Should return Fail when status is failed": {
			client: &MockPrometheusService{
				status: &prometheusservice.AlertManagerDefinitionStatus{
					StatusCode: aws.String(prometheusservice.AlertManagerDefinitionStatusCodeCreationFailed),
				},
			},
			status:        handler.Failed,
			targetMessage: "AlertManagerDefinition status: CREATION_FAILED",
		},
		"Should return in progress when state is not target state": {
			client: &MockPrometheusService{
				status: &prometheusservice.AlertManagerDefinitionStatus{
					StatusCode: aws.String(prometheusservice.AlertManagerDefinitionStatusCodeCreating),
				},
			},
			status:        handler.InProgress,
			targetState:   prometheusservice.AlertManagerDefinitionStatusCodeActive,
			targetMessage: "In Progress",
		},
		"Should return Success when state is  target state": {
			client: &MockPrometheusService{
				status: &prometheusservice.AlertManagerDefinitionStatus{
					StatusCode: aws.String(prometheusservice.AlertManagerDefinitionStatusCodeActive),
				},
			},
			status:      handler.Success,
			targetState: prometheusservice.AlertManagerDefinitionStatusCodeActive,
		},
	}

	m := &Model{
		Arn: aws.String("arn:aws:aps:us-west-2:933102010132:workspace/ws-5f41d54a-41fc-4783-984f-7facb35c928c"),
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			evt, err := validateAlertManagerState(tc.client, m, tc.targetState, "")
			if err != nil {
				t.Fatalf("Failed: %v", err)
			}
			if evt.OperationStatus != tc.status {
				t.Fatalf("Unexpected Status: %s and should be %s", evt.OperationStatus, tc.status)
			}

			if evt.Message != tc.targetMessage {
				t.Fatalf("Unexpected message: %s and should be %s", evt.Message, tc.targetMessage)
			}
		})
	}
}
