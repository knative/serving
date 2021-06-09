module dqp

go 1.16

replace (
	XDTprototype/utils => ../utils
	XDTprototype/proto/crossXDT => ../proto/crossXDT
	XDTprototype/proto/downXDT => ../proto/downXDT
	XDTprototype/proto/fnInvocation => ../proto/fnInvocation
	XDTprototype/transport => ../transport
)

require (
	XDTprototype/utils v0.0.0-00010101000000-000000000000
	XDTprototype/proto/crossXDT v0.0.0-00010101000000-000000000000
	XDTprototype/proto/downXDT v0.0.0-00010101000000-000000000000
	XDTprototype/proto/fnInvocation v0.0.0-00010101000000-000000000000
	XDTprototype/transport v0.0.0-00010101000000-000000000000
	github.com/sirupsen/logrus v1.8.1
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.20.0
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	google.golang.org/grpc v1.37.0
)
