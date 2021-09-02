package internal

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

func TestParseARN(t *testing.T) {
	testCases := []struct {
		arn          string
		resourceType string
		resourceId   string
	}{
		{"arn:aws:aps:us-west-2:933102010132:workspace/ws-5f41d54a-41fc-4783-984f-7facb35c928c", "workspace", "ws-5f41d54a-41fc-4783-984f-7facb35c928c"},
		{"arn:aws:aps:us-west-2:320989744364:rulegroupsnamespace/ws-5291a005-10a2-4b24-aabc-5ce35174430a/Test2", "rulegroupsnamespace", "ws-5291a005-10a2-4b24-aabc-5ce35174430a"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.arn, func(t *testing.T) {
			arn, workspaceID, err := ParseARN(testCase.arn)
			assert.NoError(t, err)
			assert.Contains(t, arn.Resource, testCase.resourceType)
			assert.Equal(t, testCase.resourceId, workspaceID)
		})
	}
}

func TestStringDiffers(t *testing.T) {
	testCases := []struct {
		name               string
		current            *string
		previous           *string
		shouldBeDifference bool
	}{
		{"nil -> not nil", nil, aws.String("foo"), true},
		{"nil -> nil", nil, nil, false},
		{"not nil -> nil", aws.String("foo"), nil, true},
		{"a -> b", aws.String("foo"), aws.String("bar"), true},
		{"a -> a", aws.String("foo"), aws.String("foo"), false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if actual := StringDiffers(testCase.current, testCase.previous); actual != testCase.shouldBeDifference {
				t.Fatalf("expected %t, but got %t", testCase.shouldBeDifference, actual)
			}
		})
	}
}

func TestStringMapDifference(t *testing.T) {
	testCases := []struct {
		name             string
		current          map[string]*string
		previous         map[string]*string
		expectedToChange map[string]*string
		expectedToRemove []*string
	}{
		{"identical", map[string]*string{"a": aws.String("b")}, map[string]*string{"a": aws.String("b")}, map[string]*string{}, []*string{}},
		{"keys to remove", map[string]*string{}, map[string]*string{"a": aws.String("b")}, map[string]*string{}, []*string{aws.String("a")}},
		{"keys to add", map[string]*string{"a": aws.String("b")}, map[string]*string{}, map[string]*string{"a": aws.String("b")}, []*string{}},
		{"change, remove", map[string]*string{"a": aws.String("b")}, map[string]*string{"a": aws.String("a"), "b": aws.String("a")}, map[string]*string{"a": aws.String("b")}, []*string{aws.String("b")}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actualChange, actualRemove := StringMapDifference(testCase.current, testCase.previous)
			if changeDiff := cmp.Diff(actualChange, testCase.expectedToChange); changeDiff != "" {
				t.Fatalf("expected no diff in change, got %s", changeDiff)
			}
			if removeDiff := cmp.Diff(actualRemove, testCase.expectedToRemove); removeDiff != "" {
				t.Fatalf("expected no diff in remove, got %s", removeDiff)
			}
		})
	}
}
