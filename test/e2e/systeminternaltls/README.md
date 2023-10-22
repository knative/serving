# System Internal TLS E2E Tests

In order to test System Internal TLS, this test turns enables request logging and sets the request log template to `TLS: {{.Request.TLS}}`.

The test setup will enable System Internal TLS, and then configure the logging settings.

The test then deploys and attempts to reach the HelloWorld test image.

Assuming the request succeeds, the test combs the logs for the Activator and QueueProxy looking for the TLS lines.

It counts the lines where `TLS: <nil>` appears, which indicates that TLS was not used. If that count is greater than 0, the test will fail.
