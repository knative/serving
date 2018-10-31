package ko

import (
	"fmt"
	"os"
	"testing"
)

const _testContent = "test\n"

func TestReadFile(t *testing.T) {
	tests := []struct {
		name                      string
		koDataPath                string
		koDataPathEnvDoesNotExist bool
		filename                  string
		want                      string
		wantErr                   bool
		err                       error
	}{{
		name:       "file name without / as prefix",
		koDataPath: "testdata",
		filename:   "testfile",
		want:       _testContent,
	}, {
		name:       "file name with / as prefix",
		koDataPath: "testdata",
		filename:   "/testfile",
		want:       _testContent,
	}, {
		name:       "file name with subdirectory",
		koDataPath: "testdata",
		filename:   "test_dir/testfile",
		want:       _testContent,
	}, {
		name:       "file name with //",
		koDataPath: "testdata",
		filename:   "test_dir//testfile",
		want:       _testContent,
	}, {
		name:       "KO_DATA_PATH is empty",
		koDataPath: "",
		wantErr:    true,
		err:        fmt.Errorf("%q does not exist or is empty", _KoDataPathEnvName),
	}, {
		name:                      "KO_DATA_PATH does not exist",
		koDataPath:                "",
		wantErr:                   true,
		koDataPathEnvDoesNotExist: true,
		err: fmt.Errorf("%q does not exist or is empty", _KoDataPathEnvName),
	}, {
		name:       "file does not exist",
		koDataPath: "testdata",
		filename:   "non_existing_file",
		wantErr:    true,
		err:        fmt.Errorf("open testdata/non_existing_file: no such file or directory"),
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.koDataPathEnvDoesNotExist {
				os.Clearenv()
			} else {
				os.Setenv(_KoDataPathEnvName, test.koDataPath)
			}

			got, err := ReadFileFromKoData(test.filename)

			if (err != nil) != test.wantErr {
				t.Errorf("ReadFileFromKoData(%q) = %v", test.filename, err)
			}
			if !test.wantErr {
				if test.want != got {
					t.Errorf("wanted %q but got %q", test.want, got)
				}
			} else {
				if err == nil || err.Error() != test.err.Error() {
					t.Errorf("wanted error %v but got: %v", test.err, err)
				}
			}
		})
	}
}
