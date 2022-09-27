package resource

import (
	"github.com/aws/aws-sdk-go/aws"
)

func stringMapToTags(m map[string]*string) []Tag {
	res := make([]Tag, 0)
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
