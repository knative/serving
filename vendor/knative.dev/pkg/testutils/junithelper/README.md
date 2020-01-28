## junithelper

junithelper is a tool for creating a simple junit test result, which is used for
creating a single junit result file with a single test

## Usage

This tool can be invoked from command line with following parameters:

- `--suite`: name of suite
- `--name`: name of test
- `--err-msg`: (optional) error message, by default it's empty, means test
  passed
- `--dest`: (optional) file path for result to be written to, default
  `junit_result.xml`

### Example

```
go run junithelper --suite foo --name TestBar --err-msg "Failed Randomly" --dest
"/tmp/junit_important_suite.xml"
```
