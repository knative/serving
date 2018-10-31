package util

import (
	"fmt"
	"testing"
)

func TestGetCommitId_HappyCase(t *testing.T) {
	commitID, err := _ExtractShortCommitID("a2d1bdfe929516d7da141aef68631a7ee6941b2d")
	if err != nil {
		t.Errorf("_ExtractShortCommitID() = %v", err)
	}
	want := "a2d1bdf"
	if want != commitID {
		t.Errorf("wanted %v but got %v", want, commitID)
	}
}

func TestGetCommitId_NoCommitIdFomat(t *testing.T) {
	data := "ref: refs/heads/non_committed_branch"
	_, err := _ExtractShortCommitID(data)
	wantedErr := fmt.Errorf("%q is not a valid GitHub commit ID", data)
	if err == nil || err.Error() != wantedErr.Error() {
		t.Errorf("Expected invalid GitHub commit ID error but got: %v", err)
	}
}
