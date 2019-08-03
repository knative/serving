package accessor

import (
	"fmt"
	"testing"
)

func TestIsNotOwned(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{{
		name: "IsNotOwned error",
		err: Error{
			err:         fmt.Errorf("test error"),
			errorReason: NotOwnResource,
		},
		want: true,
	}, {
		name: "other error",
		err: Error{
			err: fmt.Errorf("test error"),
		},
		want: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNotOwned(tc.err)
			if tc.want != got {
				t.Errorf("IsNotOwned function fails. want: %t, got: %t", tc.want, got)
			}
		})
	}
}

func TestError(t *testing.T) {
	err := Error{
		err:         fmt.Errorf("test error"),
		errorReason: NotOwnResource,
	}
	got := err.Error()
	want := "notowned: test error"
	if got != want {
		t.Errorf("Error function fails. want: %q, got: %q", want, got)
	}

}
