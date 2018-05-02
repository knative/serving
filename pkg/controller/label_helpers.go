package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/mattbaird/jsonpatch"
	"k8s.io/apimachinery/pkg/types"
)

type configurationClient interface {
	Patch(string, types.PatchType, []byte, ...string) (result *v1alpha1.Configuration, err error)
}

type revisionClient interface {
	Patch(string, types.PatchType, []byte, ...string) (result *v1alpha1.Revision, err error)
}

func SetConfigLabel(
	client configurationClient,
	configName,
	labelKey,
	labelValue string) error {

	patch, err := createAddLabelPatch(labelKey, labelValue)
	if err != nil {
		return err
	}

	_, err = client.Patch(configName, types.JSONPatchType, patch)
	return err
}

func RemoveConfigLabel(
	client configurationClient,
	configName,
	labelKey string) error {

	patch, err := createRemoveLabelPatch(labelKey)
	if err != nil {
		return err
	}

	_, err = client.Patch(configName, types.JSONPatchType, patch)
	return err
}

func SetRevisionLabel(
	client revisionClient,
	revisionName,
	labelKey,
	labelValue string) error {

	patch, err := createAddLabelPatch(labelKey, labelValue)
	if err != nil {
		return err
	}

	_, err = client.Patch(revisionName, types.JSONPatchType, patch)
	return err

}

func RemoveRevisionLabel(
	client revisionClient,
	revisionName,
	labelKey string) error {

	patch, err := createRemoveLabelPatch(labelKey)
	if err != nil {
		return err
	}

	_, err = client.Patch(revisionName, types.JSONPatchType, patch)
	return err
}

func createAddLabelPatch(key, value string) ([]byte, error) {
	patch := []jsonpatch.JsonPatchOperation{
		{
			Operation: "add",
			Path:      makePath("/metadata/labels", key),
			Value:     value,
		},
	}

	return json.Marshal(patch)
}

func createRemoveLabelPatch(key string) ([]byte, error) {
	patch := []jsonpatch.JsonPatchOperation{
		{
			Operation: "remove",
			Path:      makePath("/metadata/labels", key),
		},
	}

	return json.Marshal(patch)
}

// TODO(bsnchan) switch to jsonpatch.MakePath when merged
// https://github.com/mattbaird/jsonpatch/pull/15
func makePath(path, part string) string {
	pathPart := strings.Replace(part, "/", "~1", -1)
	return fmt.Sprintf("%s/%s", path, pathPart)
}
