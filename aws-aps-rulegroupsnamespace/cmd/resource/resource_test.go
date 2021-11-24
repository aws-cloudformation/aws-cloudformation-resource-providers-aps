package resource

import (
	"testing"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/stretchr/testify/assert"
)

func TestCreate_withInvalidModel(t *testing.T) {
	testCases := map[string]struct {
		currentModel  Model
	}{
		"Should return Failed when Workspace is missing": {
			Model{
				Data:      aws.String("ruleGroupData"),
				Name:      aws.String("name"),
			},
		},
		"Should return Failed when Data is missing": {
			Model{
				Workspace: aws.String("workspaceArn"),
				Name:      aws.String("name"),
			},
		},
		"Should return Failed when Name is missing": {
			Model{
				Workspace: aws.String("workspaceArn"),
				Data:      aws.String("ruleGroupData"),
			},
		},
	}

	req := handler.Request{
		LogicalResourceID: "foo",
		Session: &session.Session{
			Config: defaults.Config(),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			failedEvent, err := Create(req, nil, &tc.currentModel)

			assert.NoError(t, err)
			assert.Equal(t, handler.Failed, failedEvent.OperationStatus)
			assert.Equal(t, "Internal Failure", failedEvent.Message)
		})
	}
}
